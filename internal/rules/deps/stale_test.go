package deps_test

import (
	"strings"
	"testing"

	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
	"github.com/rerorets/hadome/internal/rules/deps"
)

// viewpoint: external contract
// target: deps/diagram-stale
// A node naming a directory whose units all lie in its subdirectories is told it can take them in
// with "/..."; a node naming a directory with no units below it is not.
func TestStaleNodeOverUnitsSuggestsSlashDots(t *testing.T) {
	var rule engine.FindingRule
	for _, r := range deps.All() {
		if r.ID() == "deps/diagram-stale" {
			rule = r.(engine.FindingRule)
		}
	}
	in := engine.Input{
		Config: config.Config{OwnDiagram: &config.Diagram{Path: "docs/deps.md", Nodes: []config.Node{
			{ID: "tools", Label: "tools", Line: 3}, {ID: "util", Label: "utils", Line: 4}}}},
		Facts: &facts.Analysis{Files: []facts.File{
			{Path: "tools/lint/main.go", Unit: "example.com/app/tools/lint", UnitPath: "tools/lint", Role: facts.RoleSource}}},
	}
	got, err := rule.Find(in)
	if err != nil || len(got) != 2 {
		t.Fatalf("findings %+v, error %v", got, err)
	}
	if fix := got[0].Fix.In("en"); !strings.Contains(fix, "tools/...") {
		t.Errorf("tools: fix %q does not show tools/...", fix)
	}
	if fix := got[1].Fix.In("en"); strings.Contains(fix, "/...") {
		t.Errorf("utils: fix %q shows /... with no units below", fix)
	}
	if fix := got[0].Fix.In("ja"); !strings.Contains(fix, "tools/...") {
		t.Errorf("tools: Japanese fix %q does not show tools/...", fix)
	}
}
