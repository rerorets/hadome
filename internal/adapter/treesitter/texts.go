//go:build cgo

package treesitter

import (
	"fmt"
	"sort"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/rerorets/hadome/internal/facts"
)

// textGrammar says how a language writes strings, the names that read them and the calls that take
// SQL, so that a test can hand over the strings it holds (facts.Test.Texts) without knowing what
// they say.
type textGrammar struct {
	literals map[string]bool // a whole string literal
	adjacent map[string]bool // literals written next to each other, which make one string
	concat   map[string]bool // binary expressions, which join strings when their operator is +
	holes    map[string]bool // parts of a literal that interpolate a value: ${…}, {…}, $name
	escapes  map[string]bool // parts of a literal that are an escape sequence
	// dollarHoles is true for Kotlin, whose grammar reads $name in a one-line string as the
	// content "$" and the content "name" rather than as an interpolation.
	dollarHoles bool
	// raw reports whether a literal is written raw, so that its backslashes are content.
	raw    func(f *file, n *sitter.Node) bool
	idents map[string]bool // names that may read a constant
	// notRead are the fields of a parent in which a name is not read: the member side of a.b, a
	// declared name, a keyword argument's name.
	notRead map[string]bool
	// notReadIn reports, for grammars without fields there, whether a name is not read where it is,
	// in its parent p.
	notReadIn func(n, p *sitter.Node) bool
	// declare returns the name and the value of a local declaration of the given kind, or nils.
	declare func(f *file, n *sitter.Node, kind string) (name, value *sitter.Node)
	// call returns, for a call of the given kind, what it calls as written, the last name of it and
	// its first argument.
	call func(f *file, n *sitter.Node, kind string) (callee, last string, first *sitter.Node, ok bool)
	// consts returns the file's constants by name with their value.
	consts func(f *file) map[string]*sitter.Node
	sinks  []sqlSink
}

// sqlSink is a call whose first argument takes SQL. A string passed there gets facts.TextSinkSQL,
// which only widens which strings the SQL adapter tries as SQL: a string still has to read as SQL
// statements to name anything. dynamic is true when the call's name says SQL clearly enough that a
// value other than a string passed there is worth one unresolved name (facts.Text.Dynamic); calls
// with common names (execute, query, update) widen the strings only, since a value passed to them
// is often not SQL at all (session.query(Model), executor.execute(task)). member restricts the
// entry to calls on a receiver (pool.query(…)), which a free function of the same name is not.
type sqlSink struct {
	name    string // the last name of the callee, or the callee as written when it holds . or ::
	dynamic bool
	member  bool
}

// matches reports whether the entry covers a call of callee whose last name is last.
func (s sqlSink) matches(callee, last string) bool {
	if strings.Contains(s.name, ".") || strings.Contains(s.name, "::") {
		return callee == s.name
	}
	return last == s.name && (!s.member || callee != last)
}

// textGrammars are the languages' text grammars, by language name.
var textGrammars = map[string]*textGrammar{
	"typescript": tsText, "python": pyText, "rust": rustText, "java": javaText, "kotlin": ktText,
}

// textCollector keeps the strings of one test while its body and the helpers it follows are walked.
type textCollector struct {
	in     *file   // the file being walked
	tables *tables // the tables of its language
	// g is the text grammar of in's language, nil while no strings are collected.
	g     *textGrammar
	texts []facts.Text
	// sinks are the nodes passed where a call takes SQL, by ID.
	sinks map[uintptr]bool
	// pending are the values of local declarations being walked, by ID: node -> the local's name.
	pending map[uintptr]string
	// locals are the locals of the test that hold a string: name -> index in texts.
	locals map[string]int
	// shadow are the names the test declares, which hide a constant of the same name.
	shadow map[string]bool
	// read are the values of constants the test reads, by ID. A walker that also walks a constant's
	// declaration when the test uses it (TypeScript's setups) does not count its string again.
	read map[uintptr]bool
}

// use makes the collector walk f, whose language's tables are tb.
func (c *textCollector) use(f *file, tb *tables) {
	c.in, c.tables, c.g = f, tb, nil
	if f != nil && tb != nil && tb.texts {
		c.g = textGrammars[tb.lang]
	}
}

// note records what node n, reached by the walk with loops loops around it, adds to the strings.
// at is where n is: its parent, and its index there when the walk knows it. The walk hands it
// down because asking tree-sitter for a node's parent descends from the root.
func (c *textCollector) note(n *sitter.Node, at place, loops int) {
	g := c.g
	if g == nil {
		return
	}
	if c.sinks == nil {
		c.sinks, c.pending, c.locals, c.shadow = map[uintptr]bool{}, map[uintptr]string{}, map[string]int{}, map[string]bool{}
		c.read = map[uintptr]bool{}
	}
	f := c.in
	times := 1
	if loops > 0 {
		times = 2 // it runs more than once
	}
	kind := n.Kind()
	if name, value := g.declare(f, n, kind); name != nil {
		c.shadow[f.text(name)] = true
		delete(c.locals, f.text(name))
		if value != nil {
			c.pending[value.Id()] = f.text(name)
		}
	}
	if callee, last, first, ok := g.call(f, n, kind); ok && first != nil {
		c.sink(g, callee, last, first, times)
	}
	switch {
	case g.top(f, n, kind, at.parent):
		if c.read[n.Id()] {
			return
		}
		value, holes, lits := g.flatten(f, n)
		if lits == 0 {
			return
		}
		c.add(facts.Text{Path: f.path, Line: line(n), Value: value, Holes: holes, Times: times}, n)
	case g.idents[kind] && !c.shadow[f.text(n)] && g.reads(n, at):
		v, ok := stringConsts(g, f)[f.text(n)]
		if !ok {
			return
		}
		c.read[v.Id()] = true
		value, holes, _ := g.flatten(f, v)
		c.add(facts.Text{Path: f.path, Line: line(v), Value: value, Holes: holes, Times: times}, n)
	}
}

// add records a text for node n, the literal or the name that reads it.
func (c *textCollector) add(t facts.Text, n *sitter.Node) {
	if c.sinks[n.Id()] {
		t.Sink = facts.TextSinkSQL
	}
	c.texts = append(c.texts, t)
	if name, ok := c.pending[n.Id()]; ok {
		c.locals[name] = len(c.texts) - 1
	}
}

// sink records the first argument of a call when the call takes SQL there.
func (c *textCollector) sink(g *textGrammar, callee, last string, arg *sitter.Node, times int) {
	entry, ok := c.sinkEntry(g, callee, last)
	if !ok {
		return
	}
	f := c.in
	switch {
	case g.literalish(f, arg):
		c.sinks[arg.Id()] = true
		return
	case g.idents[arg.Kind()]:
		name := f.text(arg)
		if i, ok := c.locals[name]; ok {
			c.texts[i].Sink = facts.TextSinkSQL
			return
		}
		if _, ok := stringConsts(g, f)[name]; ok && !c.shadow[name] {
			c.sinks[arg.Id()] = true
			return
		}
	}
	// A call that takes SQL itself, such as db.execute(sql`…`), hands its own string over.
	if inner, last, _, ok := g.call(f, arg, arg.Kind()); ok {
		if _, isSink := c.sinkEntry(g, inner, last); isSink {
			return
		}
	}
	if entry.dynamic {
		c.texts = append(c.texts, facts.Text{Path: f.path, Line: line(arg), Sink: facts.TextSinkSQL, Dynamic: true, Times: times})
	}
}

// sinkEntry returns the entry of the built-in table or the configuration that covers the call.
// Configuration's entries count as dynamic: they say the call takes SQL.
func (c *textCollector) sinkEntry(g *textGrammar, callee, last string) (sqlSink, bool) {
	for _, name := range c.tables.sqlSinks {
		if s := (sqlSink{name: name, dynamic: true}); s.matches(callee, last) {
			return s, true
		}
	}
	for _, s := range g.sinks {
		if s.matches(callee, last) {
			return s, true
		}
	}
	return sqlSink{}, false
}

// done merges the texts recorded at the same place with the same content, adding up their uses,
// and sorts them by path, line and value.
func (c *textCollector) done() []facts.Text {
	byKey := map[string]int{}
	var out []facts.Text
	for _, t := range c.texts {
		key := fmt.Sprintf("%s\x00%d\x00%s\x00%v\x00%s\x00%v", t.Path, t.Line, t.Value, t.Holes, t.Sink, t.Dynamic)
		if i, ok := byKey[key]; ok {
			out[i].Times += t.Times
			continue
		}
		byKey[key] = len(out)
		out = append(out, t)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Value != b.Value {
			return a.Value < b.Value
		}
		return a.Sink < b.Sink
	})
	return out
}

// stringConsts returns the file's constants that hold a string, read once per file.
func stringConsts(g *textGrammar, f *file) map[string]*sitter.Node {
	if f.stringConsts == nil {
		f.stringConsts = map[string]*sitter.Node{}
		for name, v := range g.consts(f) {
			for v.Kind() == "parenthesized_expression" && v.NamedChildCount() == 1 {
				v = v.NamedChild(0)
			}
			if g.literalish(f, v) {
				f.stringConsts[name] = v
			}
		}
	}
	return f.stringConsts
}

// joins reports whether n joins the strings below it into one.
func (g *textGrammar) joins(f *file, n *sitter.Node) bool {
	return n != nil && g.joinsAs(f, n, n.Kind())
}

// joinsAs is joins for a node whose kind is known.
func (g *textGrammar) joinsAs(f *file, n *sitter.Node, kind string) bool {
	return g.adjacent[kind] || (g.concat[kind] && f.field(n, "operator") == "+")
}

// top reports whether n, of the given kind and below parent, is a whole string: a literal, or
// literals joined, that is not part of a larger one.
func (g *textGrammar) top(f *file, n *sitter.Node, kind string, parent *sitter.Node) bool {
	return (g.literals[kind] || g.joinsAs(f, n, kind)) && !g.joins(f, parent)
}

// literalish reports whether n is a string: a literal, or a join holding at least one.
func (g *textGrammar) literalish(f *file, n *sitter.Node) bool {
	if !g.literals[n.Kind()] && !g.joins(f, n) {
		return false
	}
	_, _, lits := g.flatten(f, n)
	return lits > 0
}

// reads reports whether the name n, at at, is read where it is, rather than declared or used as a
// member.
func (g *textGrammar) reads(n *sitter.Node, at place) bool {
	p := at.parent
	if p == nil {
		return false
	}
	if i := at.index(n); i >= 0 && g.notRead[p.FieldNameForChild(uint32(i))] {
		return false
	}
	return g.notReadIn == nil || !g.notReadIn(n, p)
}

// place is where a node is in its tree: its parent, nil at the root, and its index among the
// parent's children, or -1 when the walk does not know it.
type place struct {
	parent *sitter.Node
	i      int
}

// placeOf is the place of n, asking tree-sitter for its parent.
func placeOf(n *sitter.Node) place { return place{parent: n.Parent(), i: -1} }

// index returns n's index among its parent's children, or -1 when n has no parent.
func (at place) index(n *sitter.Node) int {
	if at.i >= 0 || at.parent == nil {
		return at.i
	}
	for i := uint(0); i < at.parent.ChildCount(); i++ {
		if at.parent.Child(i).Id() == n.Id() {
			return int(i)
		}
	}
	return -1
}

// flatten returns the content of the string n: literals joined into one, with a hole wherever a
// value that is not a literal is joined or interpolated, and the number of literals in it.
func (g *textGrammar) flatten(f *file, n *sitter.Node) (string, []int, int) {
	var b textBuf
	lits := g.flattenInto(f, n, &b)
	return b.String(), b.holes, lits
}

// textBuf is a string being built, with the offsets of its holes.
type textBuf struct {
	strings.Builder
	holes []int
}

func (b *textBuf) hole() { b.holes = append(b.holes, b.Len()) }

func (g *textGrammar) flattenInto(f *file, n *sitter.Node, b *textBuf) int {
	switch {
	case g.literals[n.Kind()]:
		raw := g.raw != nil && g.raw(f, n)
		g.content(f, n, b, raw, false)
		return 1
	case g.adjacent[n.Kind()]:
		lits := 0
		for _, c := range named(n) {
			lits += g.flattenInto(f, c, b)
		}
		return lits
	case g.joins(f, n):
		return g.flattenInto(f, n.ChildByFieldName("left"), b) + g.flattenInto(f, n.ChildByFieldName("right"), b)
	case n.Kind() == "parenthesized_expression" && n.NamedChildCount() == 1:
		return g.flattenInto(f, n.NamedChild(0), b)
	default:
		b.hole()
		return 0
	}
}

// content writes the content of a literal: its text without delimiters, with interpolations as
// holes. The common escapes (\n, \t, \r, \", \', \\, \`, \$) are decoded, so that a string reads
// as the program sees it; other escapes are kept as written, and a raw literal keeps them all.
// inner is true inside a part of the literal, where text between child nodes is content too.
func (g *textGrammar) content(f *file, n *sitter.Node, b *textBuf, raw, inner bool) {
	at := n.StartByte()
	for i := uint(0); i < n.ChildCount(); i++ {
		c := n.Child(i)
		if inner {
			b.Write(f.src[at:c.StartByte()])
		}
		at = c.EndByte()
		k := c.Kind()
		switch {
		case g.holes[k]:
			b.hole()
		case g.escapes[k]:
			if raw {
				b.WriteString(f.text(c))
			} else {
				b.WriteString(unescape(f.text(c)))
			}
		case !c.IsNamed():
			// a quote, a prefix or the brackets of an interpolation
		case g.dollarHoles && f.text(c) == "$" && i+1 < n.ChildCount() && startsName(f.text(n.Child(i+1))):
			b.hole()
			next := n.Child(i + 1)
			rest := f.text(next)
			b.WriteString(strings.TrimLeftFunc(rest, isNameRune))
			at = next.EndByte()
			i++
		case c.ChildCount() > 0:
			g.content(f, c, b, raw, true)
		default:
			if k == "string_start" || k == "string_end" {
				continue
			}
			b.WriteString(f.text(c))
		}
	}
	if inner {
		b.Write(f.src[at:n.EndByte()])
	}
}

func startsName(s string) bool {
	return s != "" && isNameRune(rune(s[0])) && (s[0] < '0' || s[0] > '9')
}

func isNameRune(r rune) bool {
	return r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

var escapes = map[string]string{`\n`: "\n", `\t`: "\t", `\r`: "\r", `\"`: `"`, `\'`: "'", `\\`: `\`, "\\`": "`", `\$`: "$"}

func unescape(s string) string {
	if v, ok := escapes[s]; ok {
		return v
	}
	return s
}
