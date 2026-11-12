package mutation_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/rerorets/hadome/internal/adapter"
	"github.com/rerorets/hadome/internal/mutation"
	"github.com/rerorets/hadome/internal/source"
)

const above = `package calc

// Above tells whether n is above lo.
func Above(n, lo int) bool {
	return n > lo
}
`

// aboveTest guards Above from another package, so that its package is not Above's.
const aboveTest = `package limits

import (
	"testing"

	"example.com/calc/calc"
)

func TestAbove(t *testing.T) {
	if !calc.Above(2, 1) || calc.Above(1, 2) {
		t.Fatal("wrong order")
	}
}
`

// viewpoint: behaviour
// target: mutation.Mutate.Run
// {packages} is filled by Go tests only: in a version without a Go file it stops the run as a
// configuration error, before any test runs, instead of silently narrowing nothing.
func TestPackagesWithoutGoIsAConfigurationError(t *testing.T) {
	t.Parallel()
	v := map[string]string{
		".hadome.yml":   "schema: 1\nmutation:\n  test: [pytest, \"{packages}\"]\n",
		"calc/calc.py":  "def sign(n):\n    return 1 if n > 0 else 0\n",
		"tests/test.py": "from calc.calc import sign\n\ndef test_sign():\n    assert sign(2) == 1\n",
	}
	an := adapter.New()
	m := &mutation.Mutate{Analyzer: an, Mutator: an, Workdir: t.TempDir(), Trees: t.TempDir(), Progress: &strings.Builder{}}
	_, err := m.Run(source.Dirs{BaseDir: dir(t, v), HeadDir: dir(t, v)})
	if err == nil || !strings.Contains(err.Error(), "{packages}") || !strings.Contains(err.Error(), "no Go file") {
		t.Fatalf("want a configuration error naming {packages}, got %v", err)
	}
}

// logGoTest makes the test binary a test command that writes down its arguments and runs go test
// with them: `<binary> log-go-test LOG ARG...`.
const logGoTest = "log-go-test"

func standInLogGoTest(log string, args []string) int {
	f, err := os.OpenFile(log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return 3
	}
	fmt.Fprintln(f, strings.Join(args, " "))
	f.Close()
	cmd := exec.Command("go", append([]string{"test"}, args...)...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		return 3
	}
	return 0
}

// counter keeps a count in a Counter. Inc is the function the changes break: it calls one
// function of its own, so it has one mutant, and with note added it has two.
const counter = `package store

// Counter counts.
type Counter struct{ n int }

func (c *Counter) bump() { c.n++ }

// N is the count.
func (c *Counter) N() int { return c.n }

// Inc counts one more.
func (c *Counter) Inc() {
	c.bump()
}
`

// total counts through store, so that a test of api reaches store only through api's import.
const total = `package api

import "example.com/calc/store"

// Total counts n times.
func Total(n int) int {
	var c store.Counter
	for i := 0; i < n; i++ {
		c.Inc()
	}
	return c.N()
}
`

const totalTest = `package api

import "testing"

func TestTotal(t *testing.T) {
	if Total(3) != 3 {
		t.Fatal("total")
	}
}
`

// needGo stops a case that runs go test where go is missing, instead of letting it pass.
func needGo(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Fatal("this case runs go test, and go is not on PATH")
	}
}

// narrowedRun is one hadome mutate over a module where two changes are guarded: calc.Above, which
// a test of limits calls, and store.Counter.Inc, which a test of api reaches through api's import.
// Its test command writes down every go test it runs. The cases that read it share it, so that
// go test builds and runs the module once for all of them; nothing they do changes it.
var narrowedRun struct {
	once           sync.Once
	progress, runs string
	err            error
}

func narrowed(t *testing.T) (progress, runs string) {
	t.Helper()
	needGo(t)
	narrowedRun.once.Do(func() {
		self, err := os.Executable()
		if err != nil {
			narrowedRun.err = err
			return
		}
		tmp := t.TempDir()
		log := filepath.Join(tmp, "log")
		v := func(above, counter string) map[string]string {
			return map[string]string{"go.mod": mod, "calc/above.go": above, "limits/limits_test.go": aboveTest,
				"store/store.go": counter, "api/api.go": total, "api/api_test.go": totalTest,
				".hadome.yml": fmt.Sprintf("schema: 1\nmutation:\n  test: [%q, %s, %q, \"{packages}\", -run, \"^({tests})$\"]\n  timeout: 60s\n",
					self, logGoTest, log)}
		}
		touchedAbove := strings.Replace(above, "return n > lo\n", "return n > lo // strictly\n", 1)
		touchedCounter := strings.Replace(counter, "\tc.bump()\n", "\tc.bump() // one\n", 1)
		var progress strings.Builder
		an := adapter.New()
		m := &mutation.Mutate{Analyzer: an, Mutator: an, Workdir: tmp, Trees: filepath.Join(tmp, "trees"), Progress: &progress}
		base, head := dir(t, v(above, counter)), dir(t, v(touchedAbove, touchedCounter))
		if _, narrowedRun.err = m.Run(source.Dirs{BaseDir: base, HeadDir: head}); narrowedRun.err != nil {
			return
		}
		runs, err := os.ReadFile(log)
		narrowedRun.progress, narrowedRun.runs, narrowedRun.err = progress.String(), string(runs), err
	})
	if narrowedRun.err != nil {
		t.Fatal(narrowedRun.err)
	}
	return narrowedRun.progress, narrowedRun.runs
}

// linesOf are the progress lines about the function.
func linesOf(progress, fn string) []string {
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(progress), "\n") {
		if strings.Contains(l, " "+fn+", ") {
			out = append(out, l)
		}
	}
	return out
}

// viewpoint: behaviour
// target: mutation.Mutate.Run
// Integration: only go test shows how it reads the packages hadome puts into {packages}. The
// package of the test that calls the broken function, written as go test names it from the root,
// runs with nothing broken, so the narrowest stage judges the mutants and catches them.
func TestPackagesNameTheCallersPackagesAsGoTestReadsThem(t *testing.T) {
	t.Parallel()
	progress, runs := narrowed(t)
	if strings.Contains(progress, "fails with nothing broken") || !strings.Contains(progress, "calc.Above, flip a comparison: caught\n") {
		t.Errorf("want the narrowest stage to run and catch the flipped comparison, got\n%s", progress)
	}
	lines := linesOf(progress, "calc.Above")
	if len(lines) == 0 {
		t.Fatalf("no mutant of Above was tried, got\n%s", progress)
	}
	for _, l := range lines {
		if !strings.HasSuffix(l, ": caught") {
			t.Errorf("the test catches every break of Above, got %q", l)
		}
	}
	if !strings.Contains(runs, "./limits -run ^(TestAbove)$\n") {
		t.Errorf("the caller's package never ran with its test's name; the runs:\n%s", runs)
	}
}

// viewpoint: behaviour
// target: mutation.Mutate.Run
// Integration: a function no test calls, but a test reaches through the package that imports it,
// is guarded by that test. Its mutants are caught by running the reaching test's package, and every
// test runs only once, with nothing broken.
func TestAMutantReachedThroughAnImportIsCaughtByTheTestsThatReachIt(t *testing.T) {
	t.Parallel()
	progress, runs := narrowed(t)
	lines := linesOf(progress, "store.Counter.Inc")
	if len(lines) == 0 {
		t.Fatalf("no mutant of Inc was tried, got\n%s", progress)
	}
	for _, l := range lines {
		if !strings.HasSuffix(l, ": caught") {
			t.Errorf("the reaching test catches every break of Inc, got %q", l)
		}
	}
	if n := strings.Count(runs, "./...\n"); n != 1 {
		t.Errorf("every test ran %d times, want once, with nothing broken; the runs:\n%s", n, runs)
	}
	if !strings.Contains(runs, "./api\n") {
		t.Errorf("the reaching test's package never ran on its own; the runs:\n%s", runs)
	}
}

// viewpoint: invariant
// target: mutation.Mutate.Run
// Integration: narrowing the tests never changes the verdict. A change to a function reached
// through an import makes a mutant its reaching test catches and one no test notices; the second
// passes the narrowed stage and survives every test. The survivors are the same whether the command
// narrows by package and name or runs every test.
func TestNarrowingGivesTheVerdictOfEveryTest(t *testing.T) {
	needGo(t)
	t.Parallel()
	noted := strings.Replace(counter, "\tc.bump()\n", "\tc.bump()\n\tc.note()\n", 1) + "\nfunc (c *Counter) note() {}\n"
	other := "package other\n\nimport \"testing\"\n\nfunc TestOther(t *testing.T) {}\n"
	v := func(code, test string) map[string]string {
		return map[string]string{"go.mod": mod, "store/store.go": code, "api/api.go": total, "api/api_test.go": totalTest,
			"other/other_test.go": other, ".hadome.yml": "schema: 1\nmutation:\n  test: " + test + "\n  timeout: 60s\n"}
	}
	// The two runs have trees of their own, so they run at once.
	commands := []string{`[go, test, "{packages}", -run, "^({tests})$"]`, "[go, test, ./...]"}
	found, errs := make([][]string, len(commands)), make([]error, len(commands))
	var wg sync.WaitGroup
	for i, test := range commands {
		an := adapter.New()
		m := &mutation.Mutate{Analyzer: an, Mutator: an, Workdir: t.TempDir(), Trees: t.TempDir(), Progress: &strings.Builder{}}
		src := source.Dirs{BaseDir: dir(t, v(counter, test)), HeadDir: dir(t, v(noted, test))}
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := m.Run(src)
			errs[i] = err
			for _, f := range r.Regressions {
				found[i] = append(found[i], fmt.Sprintf("%s:%d %s", f.File, f.Line, f.Anchor))
			}
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	narrowed, every := found[0], found[1]
	if len(every) == 0 {
		t.Fatal("no survivor with every test, though nothing notices that note is not called")
	}
	if strings.Join(narrowed, "\n") != strings.Join(every, "\n") {
		t.Errorf("narrowed survivors\n%s\nwant those of every test\n%s", strings.Join(narrowed, "\n"), strings.Join(every, "\n"))
	}
}
