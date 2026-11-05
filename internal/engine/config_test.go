package engine_test

import (
	"bytes"
	"encoding/json"
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

// viewpoint: behaviour
// target: engine.Engine.Check
// Configuration invariant 5's exception: a change that adds .hadome.yml where base has none
// introduces hadome. Nothing ran on base, so nothing is loosened; the settings that differ from the
// built-in ones are listed instead, without changing the exit code.
func TestAddingTheFirstConfigurationIntroducesHadome(t *testing.T) {
	const raised = "schema: 1\nrules:\n  size:\n    file-lines: 500\n"
	tree := module("")
	r := check(t, treeSource{base: tree, head: withConfig(tree, raised)})
	if r.ExitCode() != engine.ExitOK || hasRule(r.Reviews, engine.LoosenedID) {
		t.Fatalf("the first configuration must not need review; got exit %d, reviews %+v", r.ExitCode(), r.Reviews)
	}
	if len(r.Introduced) != 1 || r.Introduced[0].Key != "rules.size.file-lines" || r.Introduced[0].Added != "500" ||
		r.Introduced[0].File != ".hadome.yml" || r.Introduced[0].Line != 4 {
		t.Fatalf("want rules.size.file-lines 500 at .hadome.yml:4 listed as introduced, got %+v", r.Introduced)
	}
	r = check(t, treeSource{base: withConfig(tree, "schema: 1\n"), head: withConfig(tree, raised)})
	if !hasRule(r.Reviews, engine.LoosenedID) || len(r.Introduced) != 0 {
		t.Fatalf("once base has a configuration, raising a limit needs review; got reviews %+v, introduced %+v", r.Reviews, r.Introduced)
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

// viewpoint: behaviour
// target: engine.WriteJSON, engine.WriteText
// The introduced settings reach agents under "introduced", an empty list when there are none so the
// key is always there, and reach people as one line each with where the setting is.
func TestIntroducedSettingsAreWrittenOut(t *testing.T) {
	tree := module("")
	for name, tc := range map[string]struct {
		head fstest.MapFS
		want int
	}{
		"introducing": {withConfig(tree, "schema: 1\nrules:\n  size:\n    file-lines: 500\n"), 1},
		"no change":   {tree, 0},
	} {
		r := check(t, treeSource{base: tree, head: tc.head})
		var js bytes.Buffer
		if err := engine.WriteJSON(&js, r, "en"); err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(js.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		got, ok := m["introduced"].([]any)
		if !ok || len(got) != tc.want {
			t.Errorf("%s: want %d introduced setting(s) in JSON, got %v", name, tc.want, m["introduced"])
		}
		var text bytes.Buffer
		engine.WriteText(&text, r, "en")
		if listed := strings.Contains(text.String(), "rules.size.file-lines  .hadome.yml:4  400 → 500"); listed != (tc.want == 1) {
			t.Errorf("%s: the text lists the setting: %v\n%s", name, listed, text.String())
		}
		if said := strings.Contains(text.String(), "information only"); said != (tc.want == 1) {
			t.Errorf("%s: the text says the list is information only: %v\n%s", name, said, text.String())
		}
	}
}
