package mutation_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
// Integration: only go test shows how it reads the packages hadome puts into {packages}. The
// package of the test that calls the broken function, written as go test names it from the root,
// runs with nothing broken, so the narrowest stage judges the mutants and catches them.
func TestPackagesNameTheCallersPackagesAsGoTestReadsThem(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Fatal("this case runs go test, and go is not on PATH")
	}
	t.Parallel()
	v := func(code string) map[string]string {
		return map[string]string{"go.mod": mod, "calc/above.go": code, "limits/limits_test.go": aboveTest,
			".hadome.yml": "schema: 1\nmutation:\n  test: [go, test, \"{packages}\", -run, \"^({tests})$\"]\n  timeout: 60s\n"}
	}
	var progress strings.Builder
	an := adapter.New()
	m := &mutation.Mutate{Analyzer: an, Mutator: an, Workdir: t.TempDir(), Trees: t.TempDir(), Progress: &progress}
	touched := strings.Replace(above, "return n > lo\n", "return n > lo // strictly\n", 1)
	if _, err := m.Run(source.Dirs{BaseDir: dir(t, v(above)), HeadDir: dir(t, v(touched))}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(progress.String(), "fails with nothing broken") || !strings.Contains(progress.String(), "calc.Above, flip a comparison: caught\n") {
		t.Errorf("want the narrowest stage to run and catch the flipped comparison, got\n%s", progress.String())
	}
	for _, l := range strings.Split(strings.TrimSpace(progress.String()), "\n") {
		if !strings.HasSuffix(l, ": caught") {
			t.Errorf("the test catches every break of Above, got %q", l)
		}
	}
}

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

// viewpoint: behaviour
// target: mutation.Mutate.Run
// Integration: a function no test calls, but a test reaches through the package that imports it,
// is guarded by that test. Its mutants are caught by running the reaching test's package, and every
// test runs only once, with nothing broken.
func TestAMutantReachedThroughAnImportIsCaughtByTheTestsThatReachIt(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Fatal("this case runs go test, and go is not on PATH")
	}
	t.Parallel()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(t.TempDir(), "log")
	add := "package store\n\n// Add adds two numbers.\nfunc Add(a, b int) int {\n\treturn a + b\n}\n"
	v := func(code string) map[string]string {
		return map[string]string{
			"go.mod":         mod,
			"store/store.go": code,
			"api/api.go": "package api\n\nimport \"example.com/calc/store\"\n\n// Sum adds the numbers.\n" +
				"func Sum(xs []int) int {\n\tt := 0\n\tfor _, x := range xs {\n\t\tt = store.Add(t, x)\n\t}\n\treturn t\n}\n",
			"api/api_test.go":     "package api\n\nimport \"testing\"\n\nfunc TestSum(t *testing.T) {\n\tif Sum([]int{2, 3}) != 5 {\n\t\tt.Fatal(\"sum\")\n\t}\n}\n",
			"other/other.go":      "package other\n\n// One is one.\nfunc One() int { return 1 }\n",
			"other/other_test.go": "package other\n\nimport \"testing\"\n\nfunc TestOne(t *testing.T) {\n\tif One() != 1 {\n\t\tt.Fatal(\"one\")\n\t}\n}\n",
			".hadome.yml": fmt.Sprintf("schema: 1\nmutation:\n  test: [%q, %s, %q, \"{packages}\", -run, \"^({tests})$\"]\n  timeout: 60s\n",
				self, logGoTest, log),
		}
	}
	var progress strings.Builder
	an := adapter.New()
	m := &mutation.Mutate{Analyzer: an, Mutator: an, Workdir: t.TempDir(), Trees: t.TempDir(), Progress: &progress}
	touched := strings.Replace(add, "return a + b\n", "return a + b // both\n", 1)
	if _, err := m.Run(source.Dirs{BaseDir: dir(t, v(add)), HeadDir: dir(t, v(touched))}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(progress.String()), "\n")
	if len(lines) == 0 || lines[0] == "" {
		t.Fatal("no mutant was tried")
	}
	for _, l := range lines {
		if !strings.HasSuffix(l, ": caught") {
			t.Errorf("the reaching test catches every break of Add, got %q", l)
		}
	}
	runs, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(runs), "./...\n"); n != 1 {
		t.Errorf("every test ran %d times, want once, with nothing broken; the runs:\n%s", n, runs)
	}
	if !strings.Contains(string(runs), "./api\n") {
		t.Errorf("the reaching test's package never ran on its own; the runs:\n%s", runs)
	}
}

// viewpoint: invariant
// target: mutation.Mutate.Run
// Integration: narrowing the tests never changes the verdict. A change that touches a guarded
// function reached through an import and adds a function whose result no test checks reports the
// same survivors whether the command narrows by package and name or runs every test.
func TestNarrowingGivesTheVerdictOfEveryTest(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Fatal("this case runs go test, and go is not on PATH")
	}
	t.Parallel()
	base := "package store\n\n// Add adds two numbers.\nfunc Add(a, b int) int {\n\treturn a + b\n}\n"
	head := strings.Replace(base, "return a + b\n", "return a + b // both\n", 1) +
		"\n// Twice doubles a number.\nfunc Twice(n int) int {\n\treturn n * 2\n}\n"
	api := func(extra string) string {
		return "package api\n\nimport \"example.com/calc/store\"\n\n// Sum adds the numbers.\n" +
			"func Sum(xs []int) int {\n\tt := 0\n\tfor _, x := range xs {\n\t\tt = store.Add(t, x)\n\t}\n" + extra + "\treturn t\n}\n"
	}
	v := func(code, apiCode, test string) map[string]string {
		return map[string]string{
			"go.mod":          mod,
			"store/store.go":  code,
			"api/api.go":      apiCode,
			"api/api_test.go": "package api\n\nimport \"testing\"\n\nfunc TestSum(t *testing.T) {\n\tif Sum([]int{2, 3}) != 5 {\n\t\tt.Fatal(\"sum\")\n\t}\n}\n",
			".hadome.yml":     "schema: 1\nmutation:\n  test: " + test + "\n  timeout: 60s\n",
		}
	}
	findings := func(test string) []string {
		an := adapter.New()
		m := &mutation.Mutate{Analyzer: an, Mutator: an, Workdir: t.TempDir(), Trees: t.TempDir(), Progress: &strings.Builder{}}
		r, err := m.Run(source.Dirs{BaseDir: dir(t, v(base, api(""), test)), HeadDir: dir(t, v(head, api("\t_ = store.Twice(t)\n"), test))})
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, f := range r.Regressions {
			out = append(out, fmt.Sprintf("%s:%d %s", f.File, f.Line, f.Anchor))
		}
		return out
	}
	narrowed, every := findings(`[go, test, "{packages}", -run, "^({tests})$"]`), findings("[go, test, ./...]")
	if len(every) == 0 {
		t.Fatal("no survivor with every test, though Twice's result is never checked")
	}
	if strings.Join(narrowed, "\n") != strings.Join(every, "\n") {
		t.Errorf("narrowed survivors\n%s\nwant those of every test\n%s", strings.Join(narrowed, "\n"), strings.Join(every, "\n"))
	}
}
