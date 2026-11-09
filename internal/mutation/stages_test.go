package mutation_test

import (
	"os/exec"
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
