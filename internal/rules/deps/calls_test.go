package deps_test

import (
	"strings"
	"testing"

	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
	"github.com/rerorets/hadome/internal/rules/deps"
)

func findingRule(t *testing.T, id string) engine.FindingRule {
	t.Helper()
	for _, r := range deps.All() {
		if r.ID() == id {
			return r.(engine.FindingRule)
		}
	}
	t.Fatalf("no rule %s", id)
	return nil
}

// viewpoint: external contract
// target: deps/call-not-in-diagram
// Undrawn calls are reported once per pair of units, at the earliest caller, naming the caller and
// the callee; entry-point code that is no symbol counts for its own unit, and neither a callee
// outside the version's symbols (test or generated code) nor an interface's method is counted.
func TestUndrawnCallsAreReportedOncePerPairAtTheEarliestCaller(t *testing.T) {
	src := func(path, unit string) facts.File {
		return facts.File{Path: path, Unit: unit, UnitPath: unit, Role: facts.RoleSource}
	}
	in := engine.Input{
		Config: config.Config{Calls: &config.Diagram{Path: "docs/calls.md", Nodes: []config.Node{
			{ID: "api", Label: "api"}, {ID: "store", Label: "store"}, {ID: "domain", Label: "domain"}},
			Edges: []config.Edge{{From: "api", To: "store"}}}},
		Facts: &facts.Analysis{
			Files: []facts.File{src("api/a.go", "api"), src("api/b.go", "api"), src("store/s.go", "store"),
				src("domain/d.go", "domain"), src("jobs/j.go", "jobs"), src("port/p.go", "port")},
			Symbols: []facts.Symbol{
				{ID: "api.Late", Path: "api/b.go", Line: 3, Calls: []string{"domain.Get"}},
				{ID: "api.Early", Path: "api/a.go", Line: 9, Calls: []string{"domain.Get", "store.Save", "domain.Fake"}},
				{ID: "store.Save", Path: "store/s.go", Line: 1},
				{ID: "domain.Get", Path: "domain/d.go", Line: 1},
				{ID: "api.Late2", Path: "api/b.go", Line: 7, Calls: []string{"port.Finder.Find"}},
				{ID: "port.Finder", Kind: facts.SymbolInterface, Path: "port/p.go", Line: 3},
				{ID: "port.Finder.Find", Kind: facts.SymbolMethod, Path: "port/p.go", Line: 4},
			},
			Entries: []facts.Entry{{Path: "jobs/j.go", Line: 5, Calls: []string{"store.Save"}},
				{Path: "api/a.go", Line: 9, Symbol: "api.Early", Calls: []string{"domain.Get"}}},
		},
	}
	got, err := findingRule(t, "deps/call-not-in-diagram").Find(in)
	if err != nil || len(got) != 2 {
		t.Fatalf("want api -> domain and jobs -> store, got %+v, error %v", got, err)
	}
	first := got[0]
	if first.File != "api/a.go" || first.Line != 9 || first.Anchor != "api -> domain" {
		t.Errorf("want api -> domain at api/a.go:9, got %s:%d %s", first.File, first.Line, first.Anchor)
	}
	if msg := first.Message.In("en"); !strings.Contains(msg, "api.Early calls domain.Get") || !strings.Contains(msg, "no arrow api --> domain") {
		t.Errorf("message %q does not name the caller, the callee and the missing arrow", msg)
	}
	entry := got[1]
	if entry.File != "jobs/j.go" || entry.Line != 5 || !strings.Contains(entry.Message.In("en"), "no node covers the calling unit") {
		t.Errorf("want the entry-point code at jobs/j.go:5 with no node, got %s:%d %q", entry.File, entry.Line, entry.Message.In("en"))
	}
}

// viewpoint: external contract
// target: deps/diagram-stale
// A node of the call diagram is anchored apart from the same label in the dependency diagram, so
// fixing one diagram never hides the other's finding.
func TestTheSameStaleLabelInBothDiagramsMakesTwoFindings(t *testing.T) {
	node := []config.Node{{ID: "util", Label: "utils", Line: 3}}
	in := engine.Input{
		Config: config.Config{
			OwnDiagram: &config.Diagram{Path: "docs/deps.md", Key: config.DiagramKey, Nodes: node},
			OwnCalls:   &config.Diagram{Path: "docs/calls.md", Key: config.CallsKey, Nodes: node}},
		Facts: &facts.Analysis{},
	}
	got, err := findingRule(t, "deps/diagram-stale").Find(in)
	if err != nil || len(got) != 2 || got[0].Anchor == got[1].Anchor {
		t.Fatalf("want two findings with different anchors, got %+v, error %v", got, err)
	}
	if got[1].File != "docs/calls.md" {
		t.Errorf("want the second in docs/calls.md, got %s", got[1].File)
	}
}
