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

// viewpoint: external contract
// target: mutation.Mutate.Run (its progress lines)
// Each progress line says where the break is, so a person watching can open the line, and it is
// written in the language the report uses: --lang, else messages in head's configuration.
func TestProgressNamesTheLineInTheChosenLanguage(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Fatal("these cases run go test, and go is not on PATH")
	}
	touched := strings.Replace(calc, "return 1", "return 1 // above zero", 1)
	for _, tc := range []struct {
		name, flag, messages, want string
	}{
		{"english by default", "", "", "calc/calc.go:5 calc.Sign, flip a comparison: caught"},
		{"messages in the configuration", "", "messages: ja\n", "calc/calc.go:5 calc.Sign、比較を逆にする: テストが気づいた"},
		{"--lang over the configuration", "en", "messages: ja\n", "calc/calc.go:5 calc.Sign, flip a comparison: caught"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			head := version(touched, strong)
			head[".hadome.yml"] = cfg + tc.messages
			var progress strings.Builder
			an := adapter.New()
			m := &mutation.Mutate{Analyzer: an, Mutator: an, Workdir: t.TempDir(), Progress: &progress, Lang: tc.flag}
			if _, err := m.Run(source.Dirs{BaseDir: dir(t, version(calc, strong)), HeadDir: dir(t, head)}); err != nil {
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
	if _, err := exec.LookPath("go"); err != nil {
		t.Fatal("these cases run go test, and go is not on PATH")
	}
	touched := strings.Replace(double, "return n * 2", "return n * 2 // twice", 1)
	t.Run("a file the build leaves out", func(t *testing.T) {
		base := version(calc, strong)
		head := version(strings.Replace(calc, "return 1", "return 1 // above zero", 1), strong)
		head["calc/double.go"] = "//go:build ignore\n\n" + double // new, so a survivor would be a regression
		var progress strings.Builder
		an := adapter.New()
		m := &mutation.Mutate{Analyzer: an, Mutator: an, Workdir: t.TempDir(), Progress: &progress}
		r, err := m.Run(source.Dirs{BaseDir: dir(t, base), HeadDir: dir(t, head)})
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Regressions) != 0 || r.ExitCode() != engine.ExitReview {
			t.Fatalf("want no regression and a review, got exit %d: %+v", r.ExitCode(), r)
		}
		if len(r.Reviews) != 1 || r.Reviews[0].Rule != mutation.RuleID || r.Reviews[0].File != "calc/double.go" {
			t.Fatalf("want one review of calc/double.go, got %+v", r.Reviews)
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
	if _, err := exec.LookPath("go"); err != nil {
		t.Fatal("these cases run go test, and go is not on PATH")
	}
	an := adapter.New()
	m := &mutation.Mutate{Analyzer: an, Mutator: untranslatable{an}, Workdir: t.TempDir(), Progress: io.Discard}
	touched := strings.Replace(calc, "return 1", "return 1 // above zero", 1)
	r, err := m.Run(source.Dirs{BaseDir: dir(t, version(calc, strong)), HeadDir: dir(t, version(touched, strong))})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Regressions) != 0 || len(r.Reviews) != 1 || r.Reviews[0].File != ".hadome.yml" {
		t.Fatalf("want one review of the untried language and no regression, got exit %d: %+v", r.ExitCode(), r)
	}
}
