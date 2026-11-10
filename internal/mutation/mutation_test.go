package mutation_test

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rerorets/hadome/internal/adapter"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
	"github.com/rerorets/hadome/internal/mutation"
	"github.com/rerorets/hadome/internal/source"
)

const mod = "module example.com/calc\n\ngo 1.21\n"

// catchChanges, as the first argument, makes the test binary stand in for a project's tests (see
// TestMain).
const catchChanges = "stand-in-tests-catch-changes"

// TestMain lets the test binary stand in for a project's test command. Run as
// `<binary> stand-in-tests-catch-changes FILE...` in a copy of a version, it fails when any FILE
// differs from FILE.unbroken, the copy of it written with the version: tests that catch every break
// of those files and of nothing else. Cases about what mutate does with the outcomes use it, so they
// do not wait for go test to build the module again for every mutant.
//
// The comment a run before breaking adds at the end of the file is not a break: tests do not see
// comments.
//
// It also stands in for what the package's own tests need (see stand_in_test.go): the stages a
// mutant runs in (mutation.StandInStages) and a second hadome mutate holding the lock
// (mutation.StandInHoldLock), and one that cannot write the lock
// (mutation.StandInUnwritableLock), and a test command that starts a process of its own
// (mutation.StandInTree, mutation.StandInLeaf) run by a hadome mutate (mutation.StandInRunTree) or
// by a test binary holding a lifeline of its own (mutation.StandInHolder).
// Every stand-in ends once the binary running the tests has ended (mutation.WatchLifeline).
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-") { // a stand-in, not the tests
		mutation.WatchLifeline()
		switch os.Args[1] {
		case catchChanges:
			os.Exit(standInCatch(os.Args[2:]))
		case mutation.StandInStages:
			os.Exit(mutation.StandInStage(os.Args[2:]))
		case mutation.StandInHoldLock:
			os.Exit(mutation.StandInHold(os.Args[2]))
		case mutation.StandInUnwritableLock:
			os.Exit(mutation.StandInUnwritable(os.Args[2]))
		case mutation.StandInTree:
			os.Exit(mutation.StandInTreeRoot(os.Args[2], os.Args[3], os.Args[4]))
		case mutation.StandInLeaf:
			os.Exit(mutation.StandInLeafRun(os.Args[2], os.Args[3]))
		case mutation.StandInRunTree:
			os.Exit(mutation.StandInRunTreeRun(os.Args[2]))
		case mutation.StandInHolder:
			os.Exit(mutation.StandInHolderRun(os.Args[2]))
		}
	}
	release := mutation.HoldLifeline()
	code := m.Run()
	release()
	os.Exit(code)
}

func standInCatch(files []string) int {
	for _, f := range files {
		now, err := os.ReadFile(f)
		if err != nil {
			fmt.Println(err)
			return 2
		}
		was, err := os.ReadFile(f + ".unbroken")
		if err != nil {
			fmt.Println(err)
			return 2
		}
		if mutation.WithoutBaselineMark(string(now)) != string(was) {
			fmt.Println("FAIL:", f, "is broken")
			return 1
		}
	}
	return 0
}

// standIn is a version whose test command is the test binary standing in for tests that catch
// every break of calc/calc.go and nothing else, whatever test is the Go test file.
func standIn(t *testing.T, code, test string) map[string]string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	v := version(code, test)
	v[".hadome.yml"] = fmt.Sprintf("schema: 1\nmutation:\n  test: [%q, %s, calc/calc.go]\n  timeout: 60s\n", exe, catchChanges)
	v["calc/calc.go.unbroken"] = code
	return v
}

const cfg = "schema: 1\nmutation:\n  test: [go, test, ./...]\n  timeout: 60s\n"

const calc = `package calc

// Sign tells whether n is above zero.
func Sign(n int) int {
	if n > 0 {
		return 1
	}
	return 0
}
`

const strong = `package calc

import "testing"

func TestSign(t *testing.T) {
	if Sign(2) != 1 || Sign(-2) != 0 || Sign(0) != 0 {
		t.Fatal("wrong sign")
	}
}
`

const weak = `package calc

import "testing"

func TestSign(t *testing.T) {
	_ = Sign(2)
}
`

// dir writes one version of the module into a fresh directory.
func dir(t *testing.T, files map[string]string) string {
	t.Helper()
	d := t.TempDir()
	for name, content := range files {
		p := filepath.Join(d, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

func run(t *testing.T, base, head map[string]string) (engine.Report, error) {
	t.Helper()
	an := adapter.New()
	m := &mutation.Mutate{Analyzer: an, Mutator: an, Workdir: t.TempDir(), Trees: t.TempDir(), Progress: io.Discard}
	return m.Run(source.Dirs{BaseDir: dir(t, base), HeadDir: dir(t, head)})
}

func version(code, test string) map[string]string {
	return map[string]string{"go.mod": mod, ".hadome.yml": cfg, "calc/calc.go": code, "calc/calc_test.go": test}
}

// viewpoint: external contract
// target: mutation.Mutate.Run (its progress lines)
// Each progress line says where the break is, so a person watching can open the line, and it is
// written in the language the report uses: --lang, else messages in head's configuration.
func TestProgressNamesTheLineInTheChosenLanguage(t *testing.T) {
	t.Parallel()
	touched := strings.Replace(calc, "return 1", "return 1 // above zero", 1)
	for _, tc := range []struct {
		name, flag, messages, want string
	}{
		{"english by default", "", "", "calc/calc.go:5 calc.Sign, flip a comparison: caught"},
		{"messages in the configuration", "", "messages: ja\n", "calc/calc.go:5 calc.Sign、比較を逆にする: テストが気づいた"},
		{"--lang over the configuration", "en", "messages: ja\n", "calc/calc.go:5 calc.Sign, flip a comparison: caught"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			head := standIn(t, touched, strong)
			head[".hadome.yml"] += tc.messages
			var progress strings.Builder
			an := adapter.New()
			m := &mutation.Mutate{Analyzer: an, Mutator: an, Workdir: t.TempDir(), Trees: t.TempDir(), Progress: &progress, Lang: tc.flag}
			if _, err := m.Run(source.Dirs{BaseDir: dir(t, standIn(t, calc, strong)), HeadDir: dir(t, head)}); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(progress.String(), tc.want+"\n") {
				t.Errorf("want a line ending %q, got\n%s", tc.want, progress.String())
			}
		})
	}
}

// viewpoint: behaviour
// target: mutation.Mutate.Run
// Integration: only real test runs show what the tests notice. A test weakened so that breaks of
// the code it guarded survive is a regression; a gap that base had as well is not reported, even in
// a function the change touched.
func TestSurvivorsAreComparedLikeFindings(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Fatal("these cases run go test, and go is not on PATH")
	}
	t.Parallel()
	t.Run("a weakened test", func(t *testing.T) {
		t.Parallel()
		r, err := run(t, version(calc, strong), version(calc, weak))
		if err != nil {
			t.Fatal(err)
		}
		if r.ExitCode() != engine.ExitRegression || len(r.Regressions) == 0 {
			t.Fatalf("want regressions, got exit %d: %+v", r.ExitCode(), r)
		}
		for _, f := range r.Regressions {
			if f.Rule != mutation.RuleID || f.File != "calc/calc.go" {
				t.Errorf("unexpected regression %+v", f)
			}
		}
	})
	t.Run("an old gap", func(t *testing.T) {
		t.Parallel()
		touched := strings.Replace(calc, "return 1", "return 1 // above zero", 1)
		r, err := run(t, version(calc, weak), version(touched, weak))
		if err != nil {
			t.Fatal(err)
		}
		if r.ExitCode() != engine.ExitOK || r.Untouched == 0 {
			t.Fatalf("base's survivors are not this change's: exit %d, %+v", r.ExitCode(), r)
		}
	})
	t.Run("tests that fail with nothing broken", func(t *testing.T) {
		t.Parallel()
		failing := strings.Replace(strong, `Sign(2) != 1`, `Sign(2) != 2`, 1)
		if _, err := run(t, version(calc, strong), version(calc, failing)); err == nil || !strings.Contains(err.Error(), "fail with nothing broken") {
			t.Fatalf("want the clean run to stop it, got %v", err)
		}
	})
}

const double = `package calc

// Double returns twice n.
func Double(n int) int {
	return n * 2
}
`

const doubleTest = `package calc

import "testing"

func TestDouble(t *testing.T) {
	if Double(3) != 6 {
		t.Fatal("wrong double")
	}
}
`

// viewpoint: behaviour
// target: mutation.Mutate.Run
// Integration: a mutant in a file the build that runs the tests does not compile is never run, so
// it is neither caught nor a survivor. Even in a new function it is not a regression; the file goes to a person's review,
// with a progress line that says why. A file the build compiles because of -tags in the test
// command is tried as before.
func TestMutantsOutsideTheBuildAreNotSurvivors(t *testing.T) {
	t.Parallel()
	touched := strings.Replace(double, "return n * 2", "return n * 2 // twice", 1)
	t.Run("a file the build leaves out", func(t *testing.T) {
		t.Parallel()
		base := standIn(t, calc, strong)
		head := standIn(t, strings.Replace(calc, "return 1", "return 1 // above zero", 1), strong)
		head["calc/double.go"] = "//go:build ignore\n\n" + double // new, so a survivor would be a regression
		// One mutant only: removing the call.
		head["calc/log.go"] = "//go:build ignore\n\npackage calc\n\nimport \"fmt\"\n\n// Log says hello.\nfunc Log() {\n\tfmt.Println(\"hello\")\n}\n"
		var progress strings.Builder
		an := adapter.New()
		m := &mutation.Mutate{Analyzer: an, Mutator: an, Workdir: t.TempDir(), Trees: t.TempDir(), Progress: &progress}
		r, err := m.Run(source.Dirs{BaseDir: dir(t, base), HeadDir: dir(t, head)})
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Regressions) != 0 || r.ExitCode() != engine.ExitReview {
			t.Fatalf("want no regression and a review, got exit %d: %+v", r.ExitCode(), r)
		}
		var files []string
		for _, f := range r.Reviews {
			if f.Rule != mutation.RuleID {
				t.Errorf("unexpected review %+v", f)
			}
			files = append(files, f.File)
		}
		if strings.Join(files, " ") != "calc/double.go calc/log.go" {
			t.Fatalf("want one review per file left out of the build, got %+v", r.Reviews)
		}
		if !strings.Contains(progress.String(), "calc.Double, swap arithmetic: not built\n") {
			t.Errorf("want a progress line saying the mutant was not built, got\n%s", progress.String())
		}
		lines := strings.Split(strings.TrimSuffix(progress.String(), "\n"), "\n")
		for i, l := range lines {
			if want := fmt.Sprintf("mutate %d/%d ", i+1, len(lines)); !strings.HasPrefix(l, want) {
				t.Errorf("mutants not built and mutants tried are counted as one run: line %d is %q, want it to start %q", i+1, l, want)
			}
		}
	})
	t.Run("a file built by -tags in the test command", func(t *testing.T) {
		t.Parallel()
		if _, err := exec.LookPath("go"); err != nil {
			t.Fatal("this case runs go test, and go is not on PATH")
		}
		tagged := "schema: 1\nmutation:\n  test: [go, test, -tags, special, ./...]\n  timeout: 60s\n"
		base, head := version(calc, strong), version(calc, strong)
		for i, v := range []map[string]string{base, head} {
			v[".hadome.yml"] = tagged
			v["calc/double.go"] = "//go:build special\n\n" + []string{double, touched}[i]
			v["calc/double_test.go"] = "//go:build special\n\n" + doubleTest
		}
		r, err := run(t, base, head)
		if err != nil {
			t.Fatal(err)
		}
		if r.ExitCode() != engine.ExitOK {
			t.Fatalf("the tests catch every break of Double, so want exit 0, got %d: %+v", r.ExitCode(), r)
		}
	})
}

// untranslatable is a mutator that cannot break one language.
type untranslatable struct{ adapter.Analyzer }

func (untranslatable) Unmutated([]facts.Symbol) []string { return []string{"Cobol"} }

// viewpoint: behaviour
// target: mutation.Mutate.Run
// Functions touched in a language hadome cannot break are not passed silently: they go to a
// person's review, never to a regression.
func TestALanguageWithoutMutantsGoesToReview(t *testing.T) {
	t.Parallel()
	an := adapter.New()
	m := &mutation.Mutate{Analyzer: an, Mutator: untranslatable{an}, Workdir: t.TempDir(), Trees: t.TempDir(), Progress: io.Discard}
	touched := strings.Replace(calc, "return 1", "return 1 // above zero", 1)
	r, err := m.Run(source.Dirs{BaseDir: dir(t, standIn(t, calc, strong)), HeadDir: dir(t, standIn(t, touched, strong))})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Regressions) != 0 || len(r.Reviews) != 1 || r.Reviews[0].File != ".hadome.yml" {
		t.Fatalf("want one review of the untried language and no regression, got exit %d: %+v", r.ExitCode(), r)
	}
}
