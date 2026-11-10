package engine_test

import (
	"bufio"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/adapter"
	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
)

// flagRule reports every line of head that says FLAG, and asks for review of every line that says
// REVIEW. Its cases are what each test gives it.
type flagRule struct{ cases []engine.Case }

func (flagRule) ID() string             { return "zz/flag" }
func (r flagRule) Cases() []engine.Case { return r.cases }
func (flagRule) Find(in engine.Input) ([]engine.Finding, error) {
	var out []engine.Finding
	err := fs.WalkDir(in.Version.FS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") {
			return err
		}
		f, err := in.Version.FS.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		for line := 1; sc.Scan(); line++ {
			text := sc.Text()
			switch {
			case strings.Contains(text, "REVIEW"):
				out = append(out, engine.Finding{Rule: "zz/flag", File: p, Line: line, Anchor: text, Review: true})
			case strings.HasSuffix(text, "// FLAG"):
				out = append(out, engine.Finding{Rule: "zz/flag", File: p, Line: line, Anchor: text})
			}
		}
		return sc.Err()
	})
	return out, err
}

// flagged is a head whose one line asks for review and whose flagged line carries a reasoned
// exception, so its review items are the rule's review and the new exception.
var flagged = fstest.MapFS{
	"p/p.go": {Data: []byte("package p\n\n// REVIEW this\nvar a = 1\n\n// gate:allow zz/flag reason: kept on purpose\nvar x = 1 // FLAG\n")},
}

func selfTest(an engine.Analyzer, c engine.Case) []engine.CaseResult {
	e := &engine.Engine{Analyzer: an, Rules: []engine.Rule{flagRule{cases: []engine.Case{c}}}}
	return e.SelfTest()
}

// viewpoint: behaviour
// target: engine.Engine.SelfTest
// A case passes when its head gives exactly the regressions and review items it expects, whatever
// order the rule's reviews and the new exceptions come in.
func TestACaseWithTheReviewsItExpectsPasses(t *testing.T) {
	c := engine.Case{Name: "flagged", Base: fstest.MapFS{}, Head: flagged, WantReview: []string{"zz/flag", "exception:zz/flag"}}
	for _, an := range []engine.Analyzer{adapter.New(), plainAnalyzer{}} {
		if failed := selfTest(an, c); len(failed) != 0 {
			t.Errorf("%T: %+v", an, failed)
		}
	}
}

// viewpoint: failure
// target: engine.Engine.SelfTest
// A case fails when its head gives other findings than it expects, when the check cannot run, and
// when the analyzer that reuses nothing gives another report or cannot run.
func TestACaseFailsWhenItsCheckDisagrees(t *testing.T) {
	broken := fstest.MapFS{".hadome.yml": {Data: []byte("rules: [\n")}, "p/p.go": flagged["p/p.go"]}
	cases := []struct {
		name string
		an   engine.Analyzer
		c    engine.Case
		want string
	}{
		{"other findings", adapter.New(), engine.Case{Name: "c", Base: fstest.MapFS{}, Head: flagged, WantFail: []string{"zz/flag"}}, "want fail"},
		{"check cannot run", adapter.New(), engine.Case{Name: "c", Base: fstest.MapFS{}, Head: broken}, "check failed"},
		{"reuse changes the report", reusing{without: forgetful{}}, engine.Case{Name: "c", Base: fstest.MapFS{}, Head: flagged, WantReview: []string{"zz/flag", "exception:zz/flag"}}, "reuse changed the report"},
		{"no reuse cannot run", reusing{without: plainAnalyzer{fail: "head"}}, engine.Case{Name: "c", Base: fstest.MapFS{}, Head: flagged, WantReview: []string{"zz/flag", "exception:zz/flag"}}, "check without reuse failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			failed := selfTest(tc.an, tc.c)
			if len(failed) != 1 || !strings.Contains(failed[0].Err, tc.want) {
				t.Errorf("failures %+v, want one saying %q", failed, tc.want)
			}
			// The failure shows both reports as JSON; the one with reuse names the rule's finding.
			if tc.want == "reuse changed the report" && len(failed) == 1 && !strings.Contains(failed[0].Err, `"rule": "zz/flag"`) {
				t.Errorf("the failure %q does not show the reports", failed[0].Err)
			}
		})
	}
}

// plainAnalyzer reads one version at a time and reuses nothing; it fails on the version labelled
// fail.
type plainAnalyzer struct{ fail string }

func (p plainAnalyzer) Analyze(v engine.Version, c config.Config) (*facts.Analysis, error) {
	if v.Label == p.fail {
		return nil, errors.New("cannot read " + v.Label)
	}
	return adapter.New().WithoutReuse().Analyze(v, c)
}

// forgetful reads a version without its comments, so no exception it holds is seen.
type forgetful struct{}

func (forgetful) Analyze(v engine.Version, c config.Config) (*facts.Analysis, error) {
	a, err := plainAnalyzer{}.Analyze(v, c)
	if a != nil {
		a.Comments = nil
	}
	return a, err
}

// reusing claims to reuse results, and gives without as the analyzer that does not.
type reusing struct{ without engine.Analyzer }

func (reusing) Analyze(v engine.Version, c config.Config) (*facts.Analysis, error) {
	return plainAnalyzer{}.Analyze(v, c)
}
func (r reusing) WithoutReuse() engine.Analyzer { return r.without }

// viewpoint: failure
// target: engine.Engine.Check
// With an analyzer that reads one version at a time, a version it cannot read fails the check,
// which names that version.
func TestAVersionTheAnalyzerCannotReadFailsTheCheck(t *testing.T) {
	for _, label := range []string{"base", "head"} {
		e := &engine.Engine{Analyzer: plainAnalyzer{fail: label}, Rules: []engine.Rule{flagRule{}}}
		_, err := e.Check(treeSource{base: fstest.MapFS{}, head: flagged})
		if err == nil || !strings.Contains(err.Error(), "analysing "+label) {
			t.Errorf("%s: got %v, want an error naming it", label, err)
		}
	}
}
