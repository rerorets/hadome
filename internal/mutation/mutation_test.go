package mutation_test

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rerorets/hadome/internal/adapter"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/mutation"
	"github.com/rerorets/hadome/internal/source"
)

const mod = "module example.com/calc\n\ngo 1.21\n"

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
	m := &mutation.Mutate{Analyzer: an, Mutator: an, Workdir: t.TempDir(), Progress: io.Discard}
	return m.Run(source.Dirs{BaseDir: dir(t, base), HeadDir: dir(t, head)})
}

func version(code, test string) map[string]string {
	return map[string]string{"go.mod": mod, ".hadome.yml": cfg, "calc/calc.go": code, "calc/calc_test.go": test}
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
	t.Run("a weakened test", func(t *testing.T) {
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
		failing := strings.Replace(strong, `Sign(2) != 1`, `Sign(2) != 2`, 1)
		if _, err := run(t, version(calc, strong), version(calc, failing)); err == nil || !strings.Contains(err.Error(), "fail with nothing broken") {
			t.Fatalf("want the clean run to stop it, got %v", err)
		}
	})
}
