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
				src("domain/d.go", "domain"), src("jobs/j.go", "jobs"), src("port/p.go", "port"), src("lib/l.go", "lib")},
			Symbols: []facts.Symbol{
				{ID: "api.Late", Path: "api/b.go", Line: 3, Calls: []string{"domain.Get"}},
				{ID: "api.Early", Path: "api/a.go", Line: 9, Calls: []string{"domain.Get", "store.Save", "domain.Fake"}},
				{ID: "api.Later", Path: "api/b.go", Line: 12, Calls: []string{"lib.Do"}},
				{ID: "lib.Do", Path: "lib/l.go", Line: 1},
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
	if err != nil || len(got) != 3 {
		t.Fatalf("want api -> domain, api -> lib and jobs -> store, got %+v, error %v", got, err)
	}
	byAnchor := map[string]engine.Finding{}
	for _, f := range got {
		byAnchor[f.Anchor] = f
	}
	first := byAnchor["api -> domain"]
	if first.File != "api/a.go" || first.Line != 9 {
		t.Errorf("want api -> domain at api/a.go:9, got %s:%d", first.File, first.Line)
	}
	if msg := first.Message.In("en"); !strings.Contains(msg, "api.Early calls domain.Get") || !strings.Contains(msg, "no arrow api --> domain") {
		t.Errorf("message %q does not name the caller, the callee and the missing arrow", msg)
	}
	if msg := byAnchor["api -> lib"].Message.In("en"); !strings.Contains(msg, "no node covers the called unit") {
		t.Errorf("api -> lib: message %q does not say no node covers lib", msg)
	}
	entry := byAnchor["jobs -> store"]
	msg := entry.Message.In("en")
	if entry.File != "jobs/j.go" || entry.Line != 5 || !strings.Contains(msg, "no node covers the calling unit") ||
		!strings.Contains(msg, "the entry point calls store.Save") {
		t.Errorf("want the entry-point code at jobs/j.go:5 with no node, got %s:%d %q", entry.File, entry.Line, msg)
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
	if got[0].Anchor != "utils" {
		t.Errorf("the dependency diagram's node keeps its label as the anchor, got %q", got[0].Anchor)
	}
	if got[1].File != "docs/calls.md" {
		t.Errorf("want the second in docs/calls.md, got %s", got[1].File)
	}
}

// viewpoint: external contract
// target: deps/call-not-in-diagram
// A call lands in every type below the one the code names that declares the method: through an
// interface, at any depth and never in the interface's own unit; on a concrete type, in its own unit
// and in a subclass overriding it. The message names the implementation and the method named.
func TestCallsLandInTheImplementations(t *testing.T) {
	src := func(path, unit string) facts.File {
		return facts.File{Path: path, Unit: unit, UnitPath: unit, Role: facts.RoleSource}
	}
	nodes := []config.Node{{ID: "api", Label: "api"}, {ID: "port", Label: "port"}, {ID: "pg", Label: "pg"}, {ID: "base", Label: "base"},
		{ID: "child", Label: "child"}}
	in := engine.Input{
		Config: config.Config{Calls: &config.Diagram{Path: "docs/calls.md", Nodes: nodes,
			Edges: []config.Edge{{From: "api", To: "base"}}}},
		Facts: &facts.Analysis{
			Files: []facts.File{src("api/a.go", "api"), src("port/p.go", "port"), src("pg/s.go", "pg"), src("base/b.go", "base"),
				src("child/c.go", "child")},
			Symbols: []facts.Symbol{
				{ID: "api.Show", Path: "api/a.go", Line: 3, Through: []string{"port.Finder.Find"}},
				{ID: "api.Save", Path: "api/a.go", Line: 8, Calls: []string{"base.Base.Save"}},
				{ID: "port.Finder", Kind: facts.SymbolInterface, Path: "port/p.go", Line: 1},
				{ID: "port.Finder.Find", Kind: facts.SymbolMethod, Path: "port/p.go", Line: 2},
				{ID: "port.Store", Kind: facts.SymbolInterface, Path: "port/p.go", Line: 4, Supertypes: []string{"port.Finder"}},
				{ID: "pg.Store", Kind: facts.SymbolType, Path: "pg/s.go", Line: 1, Supertypes: []string{"port.Store"}},
				{ID: "pg.Store.Find", Kind: facts.SymbolMethod, Path: "pg/s.go", Line: 2},
				{ID: "base.Base", Kind: facts.SymbolType, Path: "base/b.go", Line: 1},
				{ID: "base.Base.Save", Kind: facts.SymbolMethod, Path: "base/b.go", Line: 2},
				{ID: "child.Child", Kind: facts.SymbolType, Path: "child/c.go", Line: 1, Supertypes: []string{"base.Base"}},
				{ID: "child.Child.Save", Kind: facts.SymbolMethod, Path: "child/c.go", Line: 2},
			},
		},
	}
	got, err := findingRule(t, "deps/call-not-in-diagram").Find(in)
	if err != nil || len(got) != 2 {
		t.Fatalf("want api -> pg and api -> child, got %+v, error %v", got, err)
	}
	byAnchor := map[string]engine.Finding{}
	for _, f := range got {
		byAnchor[f.Anchor] = f
	}
	if msg := byAnchor["api -> pg"].Message.In("en"); !strings.Contains(msg, "api.Show calls pg.Store.Find through port.Finder.Find") {
		t.Errorf("api -> pg: message %q does not name the implementation and the method named", msg)
	}
	if msg := byAnchor["api -> child"].Message.In("en"); !strings.Contains(msg, "api.Save calls child.Child.Save through base.Base.Save") {
		t.Errorf("api -> child: message %q does not name the override", msg)
	}
}

// viewpoint: failure handling
// target: deps/call-not-in-diagram
// Supertypes that form loops, which code that does not compile can write, end the walk, through the
// interface the code names or not: the call lands in each type below once, and never back in that
// interface.
func TestALoopOfSupertypesEndsTheWalk(t *testing.T) {
	src := func(path, unit string) facts.File {
		return facts.File{Path: path, Unit: unit, UnitPath: unit, Role: facts.RoleSource}
	}
	in := engine.Input{
		Config: config.Config{Calls: &config.Diagram{Path: "docs/calls.md",
			Nodes: []config.Node{{ID: "api", Label: "api"}, {ID: "pa", Label: "pa"}, {ID: "pb", Label: "pb"}, {ID: "pc", Label: "pc"}},
			Edges: []config.Edge{{From: "api", To: "pb"}, {From: "api", To: "pc"}}}},
		Facts: &facts.Analysis{
			Files: []facts.File{src("api/a.go", "api"), src("pa/a.go", "pa"), src("pb/b.go", "pb"), src("pc/c.go", "pc")},
			Symbols: []facts.Symbol{
				{ID: "api.Show", Path: "api/a.go", Line: 3, Through: []string{"pa.A.M"}},
				{ID: "pa.A", Kind: facts.SymbolInterface, Path: "pa/a.go", Line: 1, Supertypes: []string{"pb.B"}},
				{ID: "pa.A.M", Kind: facts.SymbolMethod, Path: "pa/a.go", Line: 2},
				{ID: "pb.B", Kind: facts.SymbolInterface, Path: "pb/b.go", Line: 1, Supertypes: []string{"pa.A", "pc.C"}},
				{ID: "pc.C", Kind: facts.SymbolType, Path: "pc/c.go", Line: 1, Supertypes: []string{"pb.B"}},
				{ID: "pc.C.M", Kind: facts.SymbolMethod, Path: "pc/c.go", Line: 2},
				{ID: "pb.B.M", Kind: facts.SymbolMethod, Path: "pb/b.go", Line: 2},
			},
		},
	}
	got, err := findingRule(t, "deps/call-not-in-diagram").Find(in)
	if err != nil || len(got) != 0 {
		t.Fatalf("want no finding, got %+v, error %v", got, err)
	}
}
