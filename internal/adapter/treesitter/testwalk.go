//go:build cgo

package treesitter

import (
	"path"
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
	// setups are the setups of the variables declared outside the test that the walk used, which
	// are part of the test's Body.
	setups []codeIn
	texts  textCollector
}

type callCount struct {
	times  int
	scopes map[string]bool
}

func newTest(f *file, name string, at *sitter.Node) *testFacts {
	return &testFacts{f: f, calls: map[string]*callCount{},
		test: facts.Test{Name: name, Path: f.path, Line: line(at), Declared: declaredBefore(f, at)}}
}

// declaredBefore reads the "key: value" lines of the test's doc comment: the run of comments that
// ends on the line right before the test, or before the attributes on it. The test is the node
// itself, or the statement or decorated definition that holds it. extra adds lines the language
// keeps elsewhere, such as a Python docstring.
func declaredBefore(f *file, at *sitter.Node, extra ...string) map[string][]string {
	n := at
	for p := n.Parent(); p != nil && (p.Kind() == "expression_statement" || p.Kind() == "decorated_definition"); p = p.Parent() {
		n = p
	}
	var lines []string
	next := line(n)
	for prev := n.PrevSibling(); prev != nil; prev = prev.PrevSibling() {
		if prev.Kind() == "attribute_item" { // Rust's #[test] sits between the comment and the function
			next = line(prev)
			continue
		}
		if !strings.Contains(prev.Kind(), "comment") || endLine(prev) < next-1 {
			break
		}
		lines = append(commentLines(f.text(prev)), lines...)
		next = line(prev)
	}
	return facts.Declarations(append(lines, extra...))
}

// commentLines splits a comment into its lines without delimiters, including the leading * of
// the lines of a block comment.
func commentLines(text string) []string {
	var out []string
	for _, l := range strings.Split(text, "\n") {
		l = stripDelimiters(l)
		out = append(out, strings.TrimSpace(strings.TrimPrefix(l, "*")))
	}
	return out
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
	t.hazardIn(t.f, cat, n)
}

// hazardIn records a hazard at n in f, a file other than the test's when the test inherits it.
func (t *testFacts) hazardIn(f *file, cat facts.HazardCategory, n *sitter.Node) {
	t.addHazard(facts.Hazard{Category: cat, Path: f.path, Line: line(n), Evidence: f.text(n)})
}

// fileHazard records a hazard at n in the test's file that is outside the tests and applies to each
// of them, such as a module mocked at the top of the file.
func (t *testFacts) fileHazard(cat facts.HazardCategory, n *sitter.Node) {
	t.addHazard(facts.Hazard{Category: cat, Path: t.f.path, Line: line(n), Evidence: t.f.text(n), FileWide: true})
}

func (t *testFacts) addHazard(hz facts.Hazard) {
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
	t.test.Texts = t.texts.done()
	return t.test
}

// bodyIn walks a test body, or a helper's, written in f; tb are the tables of f's language. The
// strings it reaches belong to the test (see textCollector).
func (t *testFacts) bodyIn(f *file, tb *tables, n *sitter.Node, isLoop func(*sitter.Node) bool, visit func(*sitter.Node) bool) {
	in, inTables := t.texts.in, t.texts.tables
	t.texts.in, t.texts.tables = f, tb
	t.body(n, isLoop, visit)
	t.texts.in, t.texts.tables = in, inTables
}

// body walks a test body. visit handles one node and returns false to skip its children; loops
// are tracked here for every language, and so are the strings the walk reaches.
func (t *testFacts) body(n *sitter.Node, isLoop func(*sitter.Node) bool, visit func(*sitter.Node) bool) {
	walk(n, func(c *sitter.Node) bool {
		t.texts.note(c, t.loops)
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

// assertPrefixes start the names of the functions that check something in the common test
// libraries: assertEquals, assert_called_once, expect, verify. A helper of another file named so
// counts too, since the adapter cannot read it and its name says it checks.
var assertPrefixes = []string{"assert", "expect", "verify"}

// assertion counts a call to callee, as written, when it checks something: a configured entry, a
// name in extra, or a name whose first or last part starts with one of the prefixes.
func (t *testFacts) assertion(callee string, configured map[string]bool, extra ...string) bool {
	parts := strings.FieldsFunc(callee, func(r rune) bool { return r == '.' || r == ':' })
	if len(parts) == 0 {
		return false
	}
	// The first part counts only as a plain name (assert.equal), so that expect(x).toBe(3), whose
	// expect(x) is a call of its own, counts once.
	names := []string{parts[len(parts)-1]}
	if !strings.ContainsAny(parts[0], "()") {
		names = append(names, parts[0])
	}
	ok := configured[callee] || callee == "fail"
	for _, name := range names {
		name = strings.ToLower(strings.TrimSuffix(name, "!"))
		for _, p := range append(assertPrefixes, extra...) {
			ok = ok || strings.HasPrefix(name, p)
		}
	}
	if ok {
		t.test.Asserts++
	}
	return ok
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

// fingerprint sets the test's Body from the tokens of nodes, leaving out the nodes that only name
// the test (names: its name, and annotations or arguments that only give it a display name),
// comments and whitespace. Text between children that is not whitespace, such as the content of a
// string some grammars keep outside any node, is a token too. context is the setup that runs for the
// test, possibly in another file; tests with the same body under different setup check different
// things, so its tokens are part of the Body, after a separator no source holds.
func (t *testFacts) fingerprint(nodes []*sitter.Node, names []*sitter.Node, context ...codeIn) {
	leaveOut := map[uintptr]bool{}
	for _, n := range names {
		if n != nil {
			leaveOut[n.Id()] = true
		}
	}
	var toks []string
	var add func(f *file, n *sitter.Node)
	add = func(f *file, n *sitter.Node) {
		if leaveOut[n.Id()] || strings.Contains(n.Kind(), "comment") {
			return
		}
		if n.ChildCount() == 0 {
			toks = append(toks, f.text(n))
			return
		}
		at := n.StartByte()
		for i := uint(0); i < n.ChildCount(); i++ {
			c := n.Child(i)
			if gap := string(f.src[at:c.StartByte()]); strings.TrimSpace(gap) != "" {
				toks = append(toks, gap)
			}
			add(f, c)
			at = c.EndByte()
		}
		if gap := string(f.src[at:n.EndByte()]); strings.TrimSpace(gap) != "" {
			toks = append(toks, gap)
		}
	}
	for _, n := range nodes {
		if n != nil {
			add(t.f, n)
		}
	}
	for _, c := range context {
		if c.n != nil {
			toks = append(toks, "\x00")
			add(c.f, c.n)
		}
	}
	t.test.Body = facts.Fingerprint(toks)
}

// callsSupport records that the test calls a function of the support file at support, and whether
// the test's file is that support file's own test: in the same directory, with the same name once
// the test marker is left out (clock.test.ts and test_clock.py for clock.ts and clock.py).
func (t *testFacts) callsSupport(support string) {
	t.test.CallsSupport = true
	if path.Dir(t.test.Path) == path.Dir(support) && testedName(path.Base(t.test.Path)) == stemOf(path.Base(support)) {
		t.test.TestsSupport = true
	}
}

// stemOf is a file name without its extension.
func stemOf(name string) string {
	return strings.TrimSuffix(name, path.Ext(name))
}

// testedName is the name of the file a test file is named after: its stem without the test
// marker (.test, .spec, _test, or a test_ prefix).
func testedName(name string) string {
	stem := stemOf(name)
	for _, suffix := range []string{".test", ".spec", "_test"} {
		if s, ok := strings.CutSuffix(stem, suffix); ok {
			return s
		}
	}
	return strings.TrimPrefix(stem, "test_")
}

// codeIn is a node with the file that holds it.
type codeIn struct {
	f *file
	n *sitter.Node
}
