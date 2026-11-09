package adapter_test

import (
	"sort"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/adapter"
	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
)

// viewpoint: external contract
// target: adapter.Analyzer.Analyze
// The facts of every language come out in one order, by path, whichever language read them first:
// rules and reports depend on it.
func TestFactsOfAllLanguagesAreSortedByPath(t *testing.T) {
	tree := fstest.MapFS{
		"go.mod":          {Data: []byte("module example.com/app\n")},
		"zstore/store.go": {Data: []byte("// Package zstore keeps rows.\npackage zstore\n\n// Seed adds a row.\nfunc Seed() {}\n")},
		"a/schema.sql":    {Data: []byte("-- the accounts\nCREATE TABLE accounts (id int);\n")},
	}
	a, err := adapter.New().Analyze(engine.Version{FS: tree}, config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, f := range a.Files {
		paths = append(paths, f.Path)
	}
	if !sort.StringsAreSorted(paths) || len(paths) != 3 {
		t.Errorf("files %v, want go.mod, a/schema.sql and zstore/store.go sorted by path", paths)
	}
	if !sort.SliceIsSorted(a.Comments, func(i, j int) bool { return a.Comments[i].Path < a.Comments[j].Path }) {
		t.Errorf("comments %+v are not sorted by path", a.Comments)
	}
}
