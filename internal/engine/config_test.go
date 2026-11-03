package engine_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/adapter"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/rules/tests"
)

func withConfig(tree fstest.MapFS, yml string) fstest.MapFS {
	out := fstest.MapFS{}
	for k, v := range tree {
		out[k] = v
	}
	if yml != "" {
		out[".hadome.yml"] = &fstest.MapFile{Data: []byte(yml)}
	}
	return out
}

const twoInvariants = "schema: 1\ninvariants:\n  - id: rows-1\n    says: Rows are kept\n  - id: rows-2\n    says: Rows are ordered\n"

// viewpoint: state integrity
// target: engine.Engine.Check
// Configuration invariant 5: a change cannot loosen the rules that judge it without a person's review.
// guards: loosening-is-reviewed
func TestLooseningTheConfigurationNeedsReviewOnce(t *testing.T) {
	tree := module("")
	oneInvariant := strings.SplitAfter(twoInvariants, "Rows are kept\n")[0]

	r := check(t, treeSource{base: withConfig(tree, twoInvariants), head: withConfig(tree, oneInvariant)})
	if r.ExitCode() != engine.ExitReview || !hasRule(r.Reviews, engine.LoosenedID) {
		t.Fatalf("removing an invariant must need review; got exit %d, reviews %+v", r.ExitCode(), r.Reviews)
	}
	r = check(t, treeSource{base: withConfig(tree, oneInvariant), head: withConfig(tree, oneInvariant)})
	if hasRule(r.Reviews, engine.LoosenedID) {
		t.Fatal("once merged, the removal is part of base and is not raised again")
	}
	r = check(t, treeSource{base: withConfig(tree, oneInvariant), head: withConfig(tree, twoInvariants)})
	if hasRule(r.Reviews, engine.LoosenedID) {
		t.Fatal("adding an invariant tightens the configuration and needs no review")
	}
}

// viewpoint: failure handling
// target: engine.Engine.Check
// Configuration invariant 2: a configuration that does not match the schema never runs a check,
// in either version, because treating it as empty would hide what it removed.
// guards: no-guessing-configuration
func TestInvalidConfigurationInEitherVersionStopsTheCheck(t *testing.T) {
	tree := module("")
	for name, src := range map[string]treeSource{
		"head": {base: withConfig(tree, twoInvariants), head: withConfig(tree, "schema: 1\ninvariant: []\n")},
		"base": {base: withConfig(tree, "schema: one\n"), head: withConfig(tree, twoInvariants)},
	} {
		e := &engine.Engine{Analyzer: adapter.New(), Rules: tests.All()}
		if _, err := e.Check(src); err == nil || !strings.Contains(err.Error(), "configuration of "+name) {
			t.Errorf("an invalid %s configuration must stop the check and say which version: %v", name, err)
		}
	}
}

// viewpoint: state integrity
// target: engine.Engine.Check
// Rule settings come from head and apply to both versions: adding a hazard entry reports new uses,
// never the uses that were already there.
func TestAddedHazardEntriesJudgeNewCodeOnly(t *testing.T) {
	const cfg = "schema: 1\nrules:\n  tests:\n    hazards:\n      sleep: [example.com/app/store.Append]\n"
	base := module("")
	head := withConfig(module(""), cfg)
	r := check(t, treeSource{base: base, head: head})
	if hasRule(r.Regressions, "tests/no-sleep") {
		t.Fatalf("a use that base already had must not fail when the entry is added: %+v", r.Regressions)
	}
	head = withConfig(module("\tstore.Append(\"b\")\n"), cfg)
	r = check(t, treeSource{base: base, head: head})
	if !hasRule(r.Regressions, "tests/no-sleep") {
		t.Fatalf("a new use of an added entry must fail: %+v", r.Regressions)
	}
}
