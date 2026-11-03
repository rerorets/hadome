package engine_test

import (
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/adapter"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/rules/tests"
	"github.com/rerorets/hadome/internal/suggest"
)

// viewpoint: external contract
// target: engine.Engine.Check
// Suggest invariant 1: candidates never change an exit code, and a check lists only those the change adds.
func TestCheckListsOnlyNewCandidatesAndKeepsTheExitCode(t *testing.T) {
	withDelete := func(tree fstest.MapFS) fstest.MapFS {
		tree["store/delete.go"] = &fstest.MapFile{Data: []byte("package store\n\n// DeleteRow deletes a row.\nfunc DeleteRow(id string) {}\n")}
		return tree
	}
	e := &engine.Engine{Analyzer: adapter.New(), Rules: tests.All(), Candidates: suggest.Candidates}
	r, err := e.Check(treeSource{base: module(""), head: withDelete(module(""))})
	if err != nil {
		t.Fatal(err)
	}
	if r.ExitCode() != engine.ExitOK || len(r.Candidates) != 1 || r.Candidates[0].Key() != "erasable store.DeleteRow" {
		t.Fatalf("a new delete is listed and passes; got exit %d, candidates %+v", r.ExitCode(), r.Candidates)
	}
	r, err = e.Check(treeSource{base: withDelete(module("")), head: withDelete(module(""))})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Candidates) != 0 {
		t.Fatalf("a candidate already in base is not repeated: %+v", r.Candidates)
	}
}
