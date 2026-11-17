package engine_test

import (
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/adapter"
	"github.com/rerorets/hadome/internal/engine"
)

// newFiles reports every file of head that base does not have. It records the versions it was
// given, so a test can see that base alone is never looked at.
type newFiles struct{ heads *[]string }

func (newFiles) ID() string           { return "zz/new-file" }
func (newFiles) Cases() []engine.Case { return nil }
func (r newFiles) FindAgainst(base, head engine.Input) ([]engine.Finding, error) {
	*r.heads = append(*r.heads, head.Version.Label)
	var out []engine.Finding
	err := fs.WalkDir(head.Version.FS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if _, err := fs.Stat(base.Version.FS, p); err != nil {
			out = append(out, engine.Finding{Rule: "zz/new-file", File: p, Line: 1, Anchor: p})
		}
		return nil
	})
	return out, err
}

// viewpoint: behaviour
// target: engine.BaseFindingRule
// A rule that finds against base is called once, on head with base beside it; what it returns is a
// regression unless an exception marker suppresses it.
func TestARuleThatFindsAgainstBaseIsCalledOnHeadOnly(t *testing.T) {
	base := fstest.MapFS{"go.mod": {Data: []byte("module example.com/app\n")}, "a/a.go": {Data: []byte("package a\n")}}
	head := fstest.MapFS{"go.mod": base["go.mod"], "a/a.go": base["a/a.go"],
		"b/b.go": {Data: []byte("package b\n")},
		"c/c.go": {Data: []byte("// gate:allow zz/new-file reason: added on purpose\npackage c\n")}}
	var heads []string
	e := &engine.Engine{Analyzer: adapter.New(), Rules: []engine.Rule{newFiles{&heads}}}
	r, err := e.Check(treeSource{base: base, head: head})
	if err != nil {
		t.Fatal(err)
	}
	if len(heads) != 1 || heads[0] != "head" {
		t.Errorf("want one call, on head, got %v", heads)
	}
	if len(r.Regressions) != 1 || r.Regressions[0].File != "b/b.go" {
		t.Errorf("want b/b.go alone as a regression, got %+v", r.Regressions)
	}
}
