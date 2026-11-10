package mutation

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/facts"
)

// viewpoint: behaviour
// target: mutation.stages
// The stages run from the narrowest: the callers in their packages, the callers in every package
// ({packages} as ./...), then every test ({tests} and the flag before it taken out). A placeholder
// the command lacks makes a stage the same as the one before, and it runs once.
func TestStagesGoFromTheCallersPackagesToEveryTest(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		test        []string
		tests, pkgs []string
		want        [][]string
	}{
		{"both placeholders", []string{"go", "test", "{packages}", "-run", "^({tests})$"}, []string{"TestA", "TestB"}, []string{"./a", "./b"},
			[][]string{
				{"go", "test", "./a", "./b", "-run", "^(TestA|TestB)$"},
				{"go", "test", "./...", "-run", "^(TestA|TestB)$"},
				{"go", "test", "./..."},
			}},
		{"tests only", []string{"go", "test", "./...", "-run", "{tests}"}, []string{"TestA"}, []string{"./a"},
			[][]string{{"go", "test", "./...", "-run", "TestA"}, {"go", "test", "./..."}}},
		{"packages only", []string{"go", "test", "{packages}"}, []string{"TestA"}, []string{"./a"},
			[][]string{{"go", "test", "./a"}, {"go", "test", "./..."}}},
		{"no caller", []string{"go", "test", "{packages}", "-run", "{tests}"}, nil, nil,
			[][]string{{"go", "test", "./..."}}},
		{"callers not narrowed by package", []string{"go", "test", "{packages}", "-run", "{tests}"}, []string{"TestA"}, nil,
			[][]string{{"go", "test", "./...", "-run", "TestA"}, {"go", "test", "./..."}}},
		{"neither placeholder", []string{"go", "test", "./..."}, []string{"TestA"}, []string{"./a"},
			[][]string{{"go", "test", "./..."}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := stages(tc.test, "|", tc.tests, tc.pkgs)
			if !slices.EqualFunc(got, tc.want, slices.Equal[[]string]) {
				t.Errorf("want %q, got %q", tc.want, got)
			}
		})
	}
}

// viewpoint: behaviour
// target: mutation.goPackages
// The packages of Go tests are their directories as go test names them from where it runs: the
// root, or -C's directory. A test that is not a Go test, or is outside -C's directory, makes the
// tests not narrowable by package, since narrowing would leave it out.
func TestGoPackagesAreTheTestsDirectoriesSeenFromWhereGoRuns(t *testing.T) {
	t.Parallel()
	tests := func(paths ...string) []facts.Test {
		var out []facts.Test
		for _, p := range paths {
			out = append(out, facts.Test{Name: "TestX", Path: p})
		}
		return out
	}
	for _, tc := range []struct {
		name  string
		test  []string
		tests []facts.Test
		want  []string
	}{
		{"at the root", []string{"go", "test", "{packages}"},
			tests("worker/queue/q_test.go", "store/store_test.go", "store/more_test.go", "main_test.go"),
			[]string{".", "./store", "./worker/queue"}},
		{"under -C", []string{"go", "-C", "svc", "test", "{packages}"}, tests("svc/store/store_test.go", "svc/main_test.go"),
			[]string{".", "./store"}},
		{"under -C=", []string{"go", "-C=svc", "test", "{packages}"}, tests("svc/store/store_test.go"), []string{"./store"}},
		{"outside -C", []string{"go", "-C", "svc", "test", "{packages}"}, tests("svc/a_test.go", "tools/b_test.go"), nil},
		{"an absolute -C", []string{"go", "-C", "/srv/app", "test", "{packages}"}, tests("a/a_test.go"), nil},
		{"-C above the root", []string{"go", "-C", "../app", "test", "{packages}"}, tests("a/a_test.go"), nil},
		{"a test of another language", []string{"go", "test", "{packages}"}, tests("store/store_test.go", "web/store.test.ts"), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := goPackages(tc.test, tc.tests); !slices.Equal(got, tc.want) {
				t.Errorf("want %q, got %q", tc.want, got)
			}
		})
	}
}

const (
	xGo = "package x\n\nvar ok = true\n"
	yPy = "ok = True\n"
)

// fakeRunner is a runner whose test command is the test binary standing in for the stages
// (StandInStages) with spec, in a copy holding x.go and y.py, with a mutant that turns ok into BROKEN
// in x.go.
func fakeRunner(t *testing.T, spec string, skipped func([]string)) (*Runner, facts.Mutant, string) {
	t.Helper()
	dir, log := t.TempDir(), filepath.Join(t.TempDir(), "log")
	for name, data := range map[string]string{"x.go": xGo, "y.py": yPy} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	r, err := newRunner(dir, config.Mutation{Test: []string{self, StandInStages, log, spec, "{packages}", "-run", "{tests}"}, TestsSeparator: "|"},
		map[string]string{"x.go": lineComment("go"), "y.py": lineComment("python")}, skipped)
	if err != nil {
		t.Fatal(err)
	}
	r.clock = scriptedClock(nil)
	at := strings.Index(xGo, "ok")
	return r, facts.Mutant{Path: "x.go", Start: at, End: at + len("ok"), Replacement: "BROKEN"}, log
}

// scriptedClock is a clock that takes no account of the machine's load. The runs before breaking
// take the durations of took in turn, and no time once they are used up. A run times out exactly
// when the stand-in says it hangs (StandInStage), and never otherwise.
func scriptedClock(took []time.Duration) clock {
	return clock{
		start: func() func() time.Duration {
			var d time.Duration
			if len(took) > 0 {
				d, took = took[0], took[1:]
			}
			return func() time.Duration { return d }
		},
		deadline: func([]string, time.Duration) (context.Context, context.CancelFunc, io.Writer) {
			ctx, cancel := context.WithCancel(context.Background())
			return ctx, cancel, &lines{line: func(l string) {
				if l == hanging {
					cancel()
				}
			}}
		},
	}
}

// brokenRuns are the logged runs that saw the mutant, by their arguments.
func brokenRuns(lines []string) []string {
	var out []string
	for _, l := range lines {
		if args, seen, _ := strings.Cut(l, " | "); strings.HasPrefix(seen, "var BROKEN") {
			out = append(out, args)
		}
	}
	return out
}

// viewpoint: behaviour
// target: mutation.Runner.try
// A mutant runs the stages from the narrowest and is caught by the first that fails, timeouts
// included; only one that passes every stage survived. A stage whose command fails with nothing
// broken never catches it: the stage is passed over, and said once. The file is restored.
func TestAMutantIsCaughtByTheFirstStageThatFailsBecauseOfIt(t *testing.T) {
	t.Parallel()
	everyStage := []string{"./a -run T1", "./... -run T1", "./..."}
	for _, tc := range []struct {
		name    string
		spec    string
		want    Outcome
		runs    []string // the stages the mutant ran in
		skipped []string // the stages passed over
	}{
		{"no stage notices", "", Survived, everyStage, nil},
		{"the narrowest notices", "catch=./a -run T1", Caught, everyStage[:1], nil},
		{"only every test notices", "catch=./...", Caught, everyStage, nil},
		{"a hang in the narrowest", "hang=./a -run T1", Caught, everyStage[:1], nil},
		{"the narrowest fails anyway", "fail=./a -run T1,catch=./a -run T1", Survived, everyStage[1:], []string{"./a -run T1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var skipped []string
			r, mu, log := fakeRunner(t, tc.spec, func(cmd []string) { skipped = append(skipped, strings.Join(cmd[4:], " ")) })
			if err := r.clean(); err != nil {
				t.Fatal(err)
			}
			for range 2 { // the second try reuses what the runs before breaking found
				got, err := r.try(mu, []string{"T1"}, []string{"./a"})
				if err != nil {
					t.Fatal(err)
				}
				if got != tc.want {
					t.Fatalf("want outcome %d, got %d", tc.want, got)
				}
			}
			runs := brokenRuns(standInLines(t, log))
			if want := append(append([]string{}, tc.runs...), tc.runs...); !slices.Equal(runs, want) {
				t.Errorf("want the mutant run in %q, got %q", want, runs)
			}
			if !slices.Equal(skipped, tc.skipped) {
				t.Errorf("want the stages %q passed over and said once, got %q", tc.skipped, skipped)
			}
			if data, _ := os.ReadFile(filepath.Join(r.dir, "x.go")); string(data) != xGo {
				t.Errorf("the broken file was not restored:\n%s", data)
			}
		})
	}
}

// viewpoint: behaviour
// target: mutation.Runner.clean, mutation.Runner.try
// Every run before breaking sees a comment of the file's language at the end of each file that
// holds a mutant, different in every run, so that caches keyed by contents miss as they do for a
// mutant. The comment is gone afterwards.
func TestRunsBeforeBreakingSeeAFreshCommentInEveryMarkedFile(t *testing.T) {
	t.Parallel()
	r, mu, log := fakeRunner(t, "", nil)
	if err := r.clean(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.try(mu, []string{"T1"}, []string{"./a"}); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	var before int
	for _, l := range standInLines(t, log) {
		parts := strings.Split(l, " | ")
		if strings.HasPrefix(parts[1], "var BROKEN") {
			continue
		}
		before++
		goMark, pyMark := parts[1], parts[2]
		if !strings.HasPrefix(goMark, "// hadome mutate baseline ") || !strings.HasPrefix(pyMark, "# hadome mutate baseline ") {
			t.Errorf("want a comment of each file's language, got %q and %q", goMark, pyMark)
		}
		if seen[goMark] {
			t.Errorf("two runs before breaking saw the same comment %q", goMark)
		}
		seen[goMark] = true
	}
	if before != 3 {
		t.Errorf("want one run before breaking per stage, got %d", before)
	}
	for name, want := range map[string]string{"x.go": xGo, "y.py": yPy} {
		if data, _ := os.ReadFile(filepath.Join(r.dir, name)); string(data) != want {
			t.Errorf("%s keeps the comment:\n%s", name, data)
		}
	}
}

// viewpoint: behaviour
// target: mutation.Runner.clean
// Invariant 1: when every test fails with nothing broken, no mutant can be judged.
func TestFailingTestsWithNothingBrokenStopTheRun(t *testing.T) {
	t.Parallel()
	r, _, _ := fakeRunner(t, "fail=./...", nil)
	if err := r.clean(); err == nil || !strings.Contains(err.Error(), "fail with nothing broken") {
		t.Fatalf("want the run stopped, got %v", err)
	}
}

// viewpoint: behaviour
// target: mutation.Runner.try
// Design "Confirming the limit": a run with a mutant that times out counts as caught only once the
// command's run before breaking, run again, does not call for a longer limit. A machine that got
// slower raises the limit and runs the mutant again, at most twice; a run before breaking that fails
// again confirms nothing; a configured timeout is the limit as written.
func TestATimeoutCatchesOnlyAgainstAConfirmedLimit(t *testing.T) {
	t.Parallel()
	const narrowest = "./a -run T1"
	for _, tc := range []struct {
		name       string
		spec       string
		took       []time.Duration // of the runs before breaking: every test, the narrowest, then its runs again
		timeout    time.Duration
		want       Outcome
		broken     int // runs of the narrowest stage with the mutant
		unbroken   int // runs of the narrowest stage before breaking
		everyStage bool
	}{
		{"a run only the slower machine stopped runs again and survives", "hangonce=" + narrowest,
			[]time.Duration{0, 0, time.Second}, 0, Survived, 2, 2, true},
		{"a hang against a confirmed limit is caught", "hang=" + narrowest,
			[]time.Duration{0, 0, 0}, 0, Caught, 1, 2, false},
		{"a hang on a machine that keeps slowing is caught after two more runs", "hang=" + narrowest,
			[]time.Duration{0, 0, time.Second, 2 * time.Second, 3 * time.Second}, 0, Caught, 3, 3, false},
		{"a run before breaking that fails again confirms nothing", "hang=" + narrowest + ",failagain=" + narrowest,
			[]time.Duration{0, 0, time.Second}, 0, Caught, 1, 2, false},
		{"a configured timeout is not confirmed", "hang=" + narrowest,
			[]time.Duration{0, 0, time.Second}, time.Hour, Caught, 1, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r, mu, log := fakeRunner(t, tc.spec, nil)
			r.clock = scriptedClock(tc.took)
			r.Timeout = tc.timeout
			if err := r.clean(); err != nil {
				t.Fatal(err)
			}
			got, err := r.try(mu, []string{"T1"}, []string{"./a"})
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("want outcome %d, got %d", tc.want, got)
			}
			lines := standInLines(t, log)
			broken := brokenRuns(lines)
			var unbroken int
			for _, l := range lines {
				if args, seen, _ := strings.Cut(l, " | "); args == narrowest && !strings.HasPrefix(seen, "var BROKEN") {
					unbroken++
				}
			}
			if n := countOf(broken, narrowest); n != tc.broken {
				t.Errorf("want the narrowest stage run %d times with the mutant, got the runs %q", tc.broken, broken)
			}
			if tc.everyStage != slices.Contains(broken, "./...") {
				t.Errorf("want every test run with the mutant: %v, got the runs %q", tc.everyStage, broken)
			}
			if unbroken != tc.unbroken {
				t.Errorf("want the narrowest stage run %d times before breaking, got %d", tc.unbroken, unbroken)
			}
		})
	}
}

func countOf(list []string, s string) int {
	n := 0
	for _, x := range list {
		if x == s {
			n++
		}
	}
	return n
}
