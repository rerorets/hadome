// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

package deps_test

import (
	"fmt"
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
// target: deps/call-not-in-diagram
// A call is reported at its own line: of a pair's calls, the one whose path and line come first,
// whichever function declares it, also through an interface and from entry-point code. A call
// whose line the facts do not hold counts at the calling function's declaration.
func TestUndrawnCallsAreReportedAtTheLineOfTheCall(t *testing.T) {
	src := func(path, unit string) facts.File {
		return facts.File{Path: path, Unit: unit, UnitPath: unit, Role: facts.RoleSource}
	}
	in := engine.Input{
		Config: config.Config{Calls: &config.Diagram{Path: "docs/calls.md", Nodes: []config.Node{
			{ID: "api", Label: "api"}, {ID: "store", Label: "store"}, {ID: "domain", Label: "domain"},
			{ID: "jobs", Label: "jobs"}, {ID: "port", Label: "port"}, {ID: "pg", Label: "pg"}, {ID: "lib", Label: "lib"}}}},
		Facts: &facts.Analysis{
			Files: []facts.File{src("api/a.go", "api"), src("store/s.go", "store"), src("domain/d.go", "domain"),
				src("jobs/j.go", "jobs"), src("port/p.go", "port"), src("pg/pg.go", "pg"), src("lib/l.go", "lib")},
			Symbols: []facts.Symbol{
				{ID: "api.Long", Path: "api/a.go", Line: 3, Calls: []string{"domain.Get"}, CallAt: map[string]int{"domain.Get": 40}},
				{ID: "api.Short", Path: "api/a.go", Line: 10, Calls: []string{"domain.Get"}, CallAt: map[string]int{"domain.Get": 12}},
				{ID: "api.Find", Path: "api/a.go", Line: 50, Through: []string{"port.Finder.Find"}, CallAt: map[string]int{"port.Finder.Find": 57}},
				{ID: "api.Old", Path: "api/a.go", Line: 70, Calls: []string{"lib.Do"}},
				{ID: "domain.Get", Path: "domain/d.go", Line: 1},
				{ID: "store.Save", Path: "store/s.go", Line: 1},
				{ID: "lib.Do", Path: "lib/l.go", Line: 1},
				{ID: "port.Finder", Kind: facts.SymbolInterface, Path: "port/p.go", Line: 3},
				{ID: "port.Finder.Find", Kind: facts.SymbolMethod, Path: "port/p.go", Line: 4},
				{ID: "pg.Repo", Kind: facts.SymbolType, Path: "pg/pg.go", Line: 3, Supertypes: []string{"port.Finder"}},
				{ID: "pg.Repo.Find", Kind: facts.SymbolMethod, Path: "pg/pg.go", Line: 5},
			},
			Entries: []facts.Entry{{Path: "jobs/j.go", Line: 5, Calls: []string{"store.Save"}, CallAt: map[string]int{"store.Save": 8}}},
		},
	}
	got, err := findingRule(t, "deps/call-not-in-diagram").Find(in)
	if err != nil {
		t.Fatal(err)
	}
	at := map[string]string{}
	for _, f := range got {
		at[f.Anchor] = fmt.Sprintf("%s:%d %s", f.File, f.Line, f.Message.In("en"))
	}
	for anchor, want := range map[string][]string{
		"api -> domain": {"api/a.go:12 ", "api.Short calls domain.Get"},
		"api -> pg":     {"api/a.go:57 ", "api.Find"},
		"jobs -> store": {"jobs/j.go:8 "},
		"api -> lib":    {"api/a.go:70 "},
	} {
		for _, w := range want {
			if !strings.Contains(at[anchor], w) {
				t.Errorf("%s: want %q in %q", anchor, w, at[anchor])
			}
		}
	}
}

// viewpoint: external contract
// target: deps/call-not-in-diagram
// A file's calls outside functions count from the file's unit, at their own line, through an
// interface too, and the message names the code outside functions of that file as the caller; a
// call whose line the facts do not hold counts at the file's first line.
func TestCallsOutsideFunctionsCountFromTheFilesUnit(t *testing.T) {
	src := func(path, unit string, calls, through []string, at map[string]int) facts.File {
		return facts.File{Path: path, Unit: unit, UnitPath: unit, Role: facts.RoleSource, Calls: calls, Through: through, CallAt: at}
	}
	in := engine.Input{
		Config: config.Config{Calls: &config.Diagram{Path: "docs/calls.md", Nodes: []config.Node{
			{ID: "app", Label: "app"}, {ID: "store", Label: "store"}, {ID: "pg", Label: "pg"}, {ID: "port", Label: "port"},
			{ID: "jobs", Label: "jobs"}, {ID: "lib", Label: "lib"}}}},
		Facts: &facts.Analysis{
			Files: []facts.File{
				src("app/state.py", "app", []string{"store.Queue"}, []string{"port.Finder.Find"},
					map[string]int{"store.Queue": 4, "port.Finder.Find": 9}),
				src("jobs/main.py", "jobs", []string{"lib.run"}, nil, nil),
				src("store/queue.py", "store", nil, nil, nil), src("port/p.py", "port", nil, nil, nil),
				src("pg/pg.py", "pg", nil, nil, nil), src("lib/l.py", "lib", nil, nil, nil),
			},
			Symbols: []facts.Symbol{
				{ID: "store.Queue", Kind: facts.SymbolType, Path: "store/queue.py", Line: 1},
				{ID: "lib.run", Path: "lib/l.py", Line: 1},
				{ID: "port.Finder", Kind: facts.SymbolInterface, Path: "port/p.py", Line: 3},
				{ID: "port.Finder.Find", Kind: facts.SymbolMethod, Path: "port/p.py", Line: 4},
				{ID: "pg.Repo", Kind: facts.SymbolType, Path: "pg/pg.py", Line: 3, Supertypes: []string{"port.Finder"}},
				{ID: "pg.Repo.Find", Kind: facts.SymbolMethod, Path: "pg/pg.py", Line: 5},
			},
		},
	}
	got, err := findingRule(t, "deps/call-not-in-diagram").Find(in)
	if err != nil {
		t.Fatal(err)
	}
	at := map[string]string{}
	for _, f := range got {
		at[f.Anchor] = fmt.Sprintf("%s:%d %s", f.File, f.Line, f.Message.In("en"))
	}
	for anchor, want := range map[string][]string{
		"app -> store": {"app/state.py:4 ", "the code outside functions in app/state.py calls store.Queue"},
		"app -> pg":    {"app/state.py:9 ", "the code outside functions in app/state.py calls pg.Repo.Find through port.Finder.Find"},
		"jobs -> lib":  {"jobs/main.py:1 "},
	} {
		for _, w := range want {
			if !strings.Contains(at[anchor], w) {
				t.Errorf("%s: want %q in %q", anchor, w, at[anchor])
			}
		}
	}
	if len(got) != 3 {
		t.Errorf("want 3 findings, got %v", at)
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

// viewpoint: external contract
// target: deps/call-not-in-diagram
// A method the type the code names does not declare lands where a value of it gets the method: the
// nearest class above that declares it, else an interface's default; a method no type of the
// repository declares (a library's, through an alias of its type) lands nowhere.
func TestACallLandsWhereTheMethodIsDeclared(t *testing.T) {
	src := func(path, unit string) facts.File {
		return facts.File{Path: path, Unit: unit, UnitPath: unit, Role: facts.RoleSource}
	}
	var nodes []config.Node
	for _, n := range []string{"api", "db", "leaf", "mid", "base", "port", "dflt"} {
		nodes = append(nodes, config.Node{ID: n, Label: n})
	}
	in := engine.Input{
		Config: config.Config{Calls: &config.Diagram{Path: "docs/calls.md", Nodes: nodes}},
		Facts: &facts.Analysis{
			Files: []facts.File{src("api/a.go", "api"), src("db/d.go", "db"), src("leaf/l.go", "leaf"), src("mid/m.go", "mid"),
				src("base/b.go", "base"), src("port/p.go", "port"), src("dflt/d.go", "dflt")},
			Symbols: []facts.Symbol{
				{ID: "api.Save", Path: "api/a.go", Line: 3, Calls: []string{"db.Db.Update", "leaf.Leaf.Save"}},
				{ID: "api.Show", Path: "api/a.go", Line: 8, Calls: []string{"leaf.Leaf.Show"}},
				{ID: "db.Db", Kind: facts.SymbolType, Path: "db/d.go", Line: 1},
				{ID: "leaf.Leaf", Kind: facts.SymbolType, Path: "leaf/l.go", Line: 1, Supertypes: []string{"port.Port", "mid.Mid"}},
				{ID: "mid.Mid", Kind: facts.SymbolType, Path: "mid/m.go", Line: 1, Supertypes: []string{"base.Base", "dflt.Shower"}},
				{ID: "base.Base", Kind: facts.SymbolType, Path: "base/b.go", Line: 1, Supertypes: []string{"mid.Mid"}},
				{ID: "base.Base.Save", Kind: facts.SymbolMethod, Path: "base/b.go", Line: 2},
				{ID: "port.Port", Kind: facts.SymbolInterface, Path: "port/p.go", Line: 1},
				{ID: "port.Port.Save", Kind: facts.SymbolMethod, Path: "port/p.go", Line: 2},
				{ID: "dflt.Shower", Kind: facts.SymbolInterface, Path: "dflt/d.go", Line: 1},
				{ID: "dflt.Shower.Show", Kind: facts.SymbolMethod, Path: "dflt/d.go", Line: 2},
			},
		},
	}
	got, err := findingRule(t, "deps/call-not-in-diagram").Find(in)
	var anchors []string
	for _, f := range got {
		anchors = append(anchors, f.Anchor)
	}
	if err != nil || strings.Join(anchors, ",") != "api -> base,api -> dflt" {
		t.Fatalf("want api -> base and api -> dflt, got %v, error %v", anchors, err)
	}
	if msg := got[0].Message.In("en"); !strings.Contains(msg, "api.Save calls base.Base.Save through leaf.Leaf.Save") {
		t.Errorf("api -> base: message %q does not name the inherited method", msg)
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

// viewpoint: external contract
// target: deps.Unfollowed
// Calls hadome cannot follow are summed by the directory of each unit the call diagram draws; a unit
// in no node, and every unit without a call diagram, counts nothing.
func TestUnfollowedCallsAreSummedOverTheCallDiagramsUnits(t *testing.T) {
	src := func(path, unit string) facts.File {
		return facts.File{Path: path, Unit: unit, UnitPath: unit, Role: facts.RoleSource}
	}
	a := &facts.Analysis{
		Files: []facts.File{src("api/a.py", "api"), src("api/b.py", "api"), src("jobs/j.py", "jobs")},
		Symbols: []facts.Symbol{{ID: "api/a.show", Path: "api/a.py", Unfollowed: 2}, {ID: "api/b.list", Path: "api/b.py", Unfollowed: 3},
			{ID: "jobs/j.run", Path: "jobs/j.py", Unfollowed: 4}},
	}
	diagram := &config.Diagram{Path: "docs/calls.md", Nodes: []config.Node{{ID: "api", Label: "api"}}}
	got := deps.Unfollowed(engine.Input{Facts: a, Config: config.Config{Calls: diagram}})
	if len(got) != 1 || got["api"] != 5 {
		t.Errorf("want api 5 alone, got %v", got)
	}
	if got := deps.Unfollowed(engine.Input{Facts: a}); len(got) != 0 {
		t.Errorf("without a call diagram, want nothing, got %v", got)
	}
}
