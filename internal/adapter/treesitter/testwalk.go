//go:build cgo

package treesitter

import (
	"sort"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/rerorets/hadome/internal/facts"
)

// testFacts collects the facts of one test while its body is walked: calls to targets, hazards,
// and whether it is skipped. The languages decide what a call or a hazard is; this keeps the counts.
type testFacts struct {
	f     *file
	test  facts.Test
	calls map[string]*callCount
	loops int // how many loops enclose the node being walked
}

type callCount struct {
	times  int
	scopes map[string]bool
}

func newTest(f *file, name string, at *sitter.Node) *testFacts {
	return &testFacts{f: f, calls: map[string]*callCount{},
		test: facts.Test{Name: name, Path: f.path, Line: line(at)}}
}

// call records one call site of a target; scope is the argument where the user or tenant goes, or
// nil. A call inside a loop counts twice, because it runs more than once.
func (t *testFacts) call(target string, scope *sitter.Node) {
	cc := t.calls[target]
	if cc == nil {
		cc = &callCount{scopes: map[string]bool{}}
		t.calls[target] = cc
	}
	cc.times++
	if t.loops > 0 {
		cc.times++
	}
	if scope != nil {
		cc.scopes[t.f.text(scope)] = true
	}
}

func (t *testFacts) hazard(cat facts.HazardCategory, n *sitter.Node) {
	hz := facts.Hazard{Category: cat, Path: t.f.path, Line: line(n), Evidence: t.f.text(n)}
	for _, h := range t.test.Hazards {
		if h == hz {
			return
		}
	}
	t.test.Hazards = append(t.test.Hazards, hz)
}

func (t *testFacts) skip(reason string, at *sitter.Node) {
	t.test.Skip = facts.Skip{Skipped: true, Reason: reason, Line: line(at)}
}

// skipIf records a skip that happens only in some cases; an unconditional skip wins over it.
func (t *testFacts) skipIf(reason string, at *sitter.Node) {
	if !t.test.Skip.Skipped {
		t.test.Skip = facts.Skip{Conditional: true, Reason: reason, Line: line(at)}
	}
}

// conditional reports whether n runs only in some cases: it is inside a branch below stop.
func conditional(n, stop *sitter.Node, branchKinds map[string]bool) bool {
	for p := n.Parent(); p != nil && p.Id() != stop.Id(); p = p.Parent() {
		if branchKinds[p.Kind()] {
			return true
		}
	}
	return false
}

func (t *testFacts) done() facts.Test {
	for id, cc := range t.calls {
		t.test.Calls = append(t.test.Calls, facts.Call{Target: id, Times: cc.times, Scopes: len(cc.scopes)})
	}
	sort.Slice(t.test.Calls, func(i, j int) bool { return t.test.Calls[i].Target < t.test.Calls[j].Target })
	return t.test
}

// body walks a test body. visit handles one node and returns false to skip its children; loops
// are tracked here for every language.
func (t *testFacts) body(n *sitter.Node, isLoop func(*sitter.Node) bool, visit func(*sitter.Node) bool) {
	walk(n, func(c *sitter.Node) bool {
		if c != n && isLoop(c) {
			t.loops++
			for i := uint(0); i < c.ChildCount(); i++ {
				t.body(c.Child(i), isLoop, visit)
			}
			t.loops--
			return false
		}
		return visit(c)
	})
}

// useGrammar says how a language writes the use of a name, so that a read can be told from a call
// and from a type.
type useGrammar struct {
	members map[string]string   // kind of a member access -> field holding its object ("" = first named child)
	names   map[string]string   // kind of a member access -> field holding the member's name ("-" = last named child)
	callees map[string][]string // kind of a call -> fields holding what it calls ("" = first named child)
	types   map[string]bool     // kinds that hold a type; names inside them are never read
}

// readName counts n, an identifier naming product code, as a read unless it is being called or it
// sits in a type. A.b.c is one use: it is a call only when the whole of it is called.
func (t *testFacts) readName(n *sitter.Node, g useGrammar) {
	if p := n.Parent(); p != nil {
		if field, ok := g.names[p.Kind()]; ok && isField(p, field, n) {
			return // the member side of a.b: a decides what b is
		}
	}
	use := n
	for p := use.Parent(); p != nil; p = p.Parent() {
		field, ok := g.members[p.Kind()]
		if !ok || !isField(p, field, use) {
			break
		}
		use = p
	}
	if p := use.Parent(); p != nil {
		for _, field := range g.callees[p.Kind()] {
			if isField(p, field, use) {
				return
			}
		}
	}
	for p := n.Parent(); p != nil; p = p.Parent() {
		if g.types[p.Kind()] {
			return
		}
	}
	t.test.Reads++
}

func isField(p *sitter.Node, field string, c *sitter.Node) bool {
	var got *sitter.Node
	switch {
	case field == "" && p.NamedChildCount() > 0:
		got = p.NamedChild(0)
	case field == "-" && p.NamedChildCount() > 0:
		got = p.NamedChild(p.NamedChildCount() - 1)
	case field != "" && field != "-":
		got = p.ChildByFieldName(field)
	}
	return got != nil && got.Id() == c.Id()
}

// fingerprint sets the test's Body from the tokens of nodes, leaving out the node naming the test,
// comments and whitespace. Text between children that is not whitespace, such as the content of a
// string some grammars keep outside any node, is a token too.
func (t *testFacts) fingerprint(nodes []*sitter.Node, name *sitter.Node) {
	var toks []string
	var add func(n *sitter.Node)
	add = func(n *sitter.Node) {
		if (name != nil && n.Id() == name.Id()) || strings.Contains(n.Kind(), "comment") {
			return
		}
		if n.ChildCount() == 0 {
			toks = append(toks, t.f.text(n))
			return
		}
		at := n.StartByte()
		for i := uint(0); i < n.ChildCount(); i++ {
			c := n.Child(i)
			if gap := string(t.f.src[at:c.StartByte()]); strings.TrimSpace(gap) != "" {
				toks = append(toks, gap)
			}
			add(c)
			at = c.EndByte()
		}
		if gap := string(t.f.src[at:n.EndByte()]); strings.TrimSpace(gap) != "" {
			toks = append(toks, gap)
		}
	}
	for _, n := range nodes {
		if n != nil {
			add(n)
		}
	}
	t.test.Body = facts.Fingerprint(toks)
}
