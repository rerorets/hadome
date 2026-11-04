package engine_test

import (
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/adapter"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/rules/tests"
)

// treeSource is two in-memory trees. It is real data, not a stand-in for behaviour.
type treeSource struct {
	base, head fs.FS
	renames    map[string]string
}

func (s treeSource) Base() (engine.Version, error) {
	return engine.Version{Label: "base", FS: s.base}, nil
}
func (s treeSource) Head() (engine.Version, error) {
	return engine.Version{Label: "head", FS: s.head}, nil
}
func (s treeSource) Renames() (map[string]string, error) { return s.renames, nil }

func module(test string) fstest.MapFS {
	return fstest.MapFS{
		"go.mod":         {Data: []byte("module example.com/app\n")},
		"store/store.go": {Data: []byte("package store\n\n// Append adds a row.\nfunc Append(row string) error { return nil }\n")},
		"store/store_test.go": {Data: []byte("package store_test\n\nimport (\n\t\"testing\"\n\t\"time\"\n\n\t\"example.com/app/store\"\n)\n\n" +
			"func TestAppend(t *testing.T) {\n\tif err := store.Append(\"a\"); err != nil {\n\t\tt.Fatal(err)\n\t}\n" + test + "}\n")},
	}
}

func check(t *testing.T, src engine.Source) engine.Report {
	t.Helper()
	e := &engine.Engine{Analyzer: adapter.New(), Rules: tests.All()}
	r, err := e.Check(src)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// viewpoint: state integrity
// target: engine.Engine.Check
// Unit tests on in-memory trees: the engine is pure apart from the source, which is real data here.
// guards: base-never-blocks
func TestExistingFindingNeverFailsWhereverItMoves(t *testing.T) {
	base := module("\t_ = time.Now()\n")
	head := module("\n\n\t_ = time.Now()\n")
	head["store/rows_test.go"], head["store/store_test.go"] = head["store/store_test.go"], nil
	delete(head, "store/store_test.go")
	r := check(t, treeSource{base: base, head: head, renames: map[string]string{"store/store_test.go": "store/rows_test.go"}})
	if r.ExitCode() != engine.ExitOK || r.Untouched != 1 {
		t.Fatalf("a moved, renamed existing finding must pass and count as untouched; got exit %d, untouched %d, %+v",
			r.ExitCode(), r.Untouched, r.Regressions)
	}
}

// viewpoint: state integrity
// target: engine.Engine.Check
func TestRenameWithoutVersionControlIsRecognised(t *testing.T) {
	base := module("\t_ = time.Now()\n")
	head := module("\t_ = time.Now()\n")
	head["store/rows_test.go"] = head["store/store_test.go"]
	delete(head, "store/store_test.go")
	r := check(t, treeSource{base: base, head: head}) // no renames reported by the source
	if r.ExitCode() != engine.ExitOK {
		t.Fatalf("a file moved with unchanged content must keep its existing findings, got %+v", r.Regressions)
	}
}

// viewpoint: boundaries and errors
// target: engine.Engine.Check
func TestDuplicateFindingsAreMatchedByCount(t *testing.T) {
	once := module("\t_ = time.Now()\n")
	twice := module("\t_ = time.Now()\n\t_ = time.Now()\n")
	if r := check(t, treeSource{base: twice, head: once}); r.ExitCode() != engine.ExitOK {
		t.Fatalf("fewer duplicates than base must pass, got %+v", r.Regressions)
	}
	if r := check(t, treeSource{base: once, head: twice}); r.ExitCode() != engine.ExitRegression {
		t.Fatalf("more duplicates than base must fail, got exit %d", r.ExitCode())
	}
}

// viewpoint: boundaries and errors
// target: engine.Engine.Check
func TestMarkers(t *testing.T) {
	plain := module("")
	cases := []struct {
		name string
		head string
		exit int
		rule string
	}{
		{"a reasoned exception suppresses the finding and needs review", "\t// gate:allow tests/no-clock reason: measures wall time on purpose\n\t_ = time.Now()\n", engine.ExitReview, ""},
		{"an exception without a reason has no effect", "\t// gate:allow tests/no-clock\n\t_ = time.Now()\n", engine.ExitRegression, "engine/marker-without-reason"},
		{"an exception for an unknown rule is a regression", "\t// gate:allow tests/no-such-rule reason: x\n", engine.ExitRegression, "engine/marker-unknown-rule"},
		{"an exception that suppresses nothing is a regression", "\t// gate:allow tests/no-clock reason: x\n", engine.ExitRegression, "engine/marker-unused"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := check(t, treeSource{base: plain, head: module(c.head)})
			if r.ExitCode() != c.exit {
				t.Fatalf("want exit %d, got %d: %+v", c.exit, r.ExitCode(), r)
			}
			if c.rule != "" && !hasRule(r.Regressions, c.rule) {
				t.Fatalf("want a %s regression, got %+v", c.rule, r.Regressions)
			}
		})
	}
}

// viewpoint: state integrity
// target: engine.Engine.Check
func TestExceptionNeedsReviewOnlyInTheChangeThatAddsIt(t *testing.T) {
	excepted := module("\t// gate:allow tests/no-clock reason: measures wall time on purpose\n\t_ = time.Now()\n")
	if r := check(t, treeSource{base: excepted, head: excepted}); r.ExitCode() != engine.ExitOK {
		t.Fatalf("an exception already in base must not be reported again, got exit %d", r.ExitCode())
	}
	reworded := module("\t// gate:allow tests/no-clock reason: something else\n\t_ = time.Now()\n")
	if r := check(t, treeSource{base: excepted, head: reworded}); r.ExitCode() != engine.ExitReview {
		t.Fatalf("a changed reason must be reviewed again, got exit %d", r.ExitCode())
	}
}

func hasRule(fs []engine.Finding, rule string) bool {
	for _, f := range fs {
		if f.Rule == rule {
			return true
		}
	}
	return false
}

// viewpoint: regression
// target: engine.Engine.SelfTest
// guards: every loaded rule has self-test cases, and all of them pass
func TestEveryRuleSelfTestPasses(t *testing.T) {
	e := &engine.Engine{Analyzer: adapter.New(), Rules: tests.All()}
	for _, f := range e.SelfTest() {
		t.Errorf("%s: %s: %s", f.Rule, f.Name, f.Err)
	}
}
