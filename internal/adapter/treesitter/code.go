// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

//go:build cgo

package treesitter

import (
	"strconv"
	"strings"

	"github.com/rerorets/hadome/internal/facts"
)

// codeLang is what the code/ rules need to know of one language's grammar
// (docs/design/rules-code.md): how a function's parameters and local variables are declared, which
// catch drops an error, and how a value that belongs in configuration is written.
type codeLang struct {
	text    *textGrammar    // the language's string literals
	numbers map[string]bool // number literals
	idents  map[string]bool // identifiers that may name a local variable
	// declares returns the identifiers a node declares as a parameter or a local variable.
	declares func(f *file, n *node) []*node
	// member reports an identifier in its parent p in a place where it names no variable: the member
	// of a.b, a keyword argument's name, a method's name.
	member func(n, p *node) bool
	// swallows reports a node that drops an error.
	swallows func(f *file, n *node) bool
	// pair returns the name and the value of an argument, keyword argument or field given by name.
	pair func(f *file, n *node) (string, *node, bool)
	// duration reports a call that builds a duration from numbers; port returns the number a call
	// takes as a port, or nil.
	duration func(f *file, n *node) bool
	port     func(f *file, n *node) *node
	// constants are declarations whose values are named, so nothing in them counts.
	constants map[string]bool
	// blocks are the blocks that indentation alone closes (Python's), whose ends are no token.
	blocks map[string]bool
}

// codeLangs are the languages' code grammars, by language name.
var codeLangs = map[string]*codeLang{
	"typescript": tsCodeLang, "python": pyCodeLang, "java": javaCodeLang, "kotlin": ktCodeLang, "rust": rustCodeLang,
}

// withCode completes a function's symbol with what the code/ rules read: its body's fingerprint, the
// values in it that belong in configuration and the places that drop an error. n is the
// declaration funcSymbol was given.
func withCode(f *file, n *node, s facts.Symbol) facts.Symbol {
	cl := codeLangs[f.lang]
	if cl == nil {
		return s
	}
	fn := functionNode(n)
	if fn == nil {
		return s
	}
	body := fn.ChildByFieldName("body")
	if body == nil {
		for _, c := range named(fn) {
			if c.Kind() == "function_body" { // Kotlin
				body = c
			}
		}
	}
	if body == nil {
		return s
	}
	from := body
	for _, field := range []string{"parameters", "parameter"} {
		if p := fn.ChildByFieldName(field); p != nil {
			from = p
			break
		}
	}
	for _, c := range named(fn) {
		if c.Kind() == "function_value_parameters" { // Kotlin
			from = c
		}
	}
	s.Body, s.BodyTokens = cl.fingerprint(f, fn, from.StartByte(), body.EndByte())
	s.Literals = cl.literals(f, body)
	walk(body, func(x *node) bool {
		if cl.swallows(f, x) {
			at, text := swallowPlace(f, x)
			s.Swallows = append(s.Swallows, facts.Swallow{Line: at, Evidence: facts.Evidence(firstLine(text))})
		}
		return true
	})
	return s
}

// swallowPlace is the line and the source of a place that drops an error: of a call to .catch that
// drops it, from .catch on, since the receiver before it may span many lines.
func swallowPlace(f *file, x *node) (int, string) {
	if fn := x.ChildByFieldName("function"); x.Kind() == "call_expression" && fn != nil && fn.Kind() == "member_expression" {
		if prop := fn.ChildByFieldName("property"); prop != nil {
			return line(prop), "." + string(f.src[prop.StartByte():x.EndByte()])
		}
	}
	return line(x), f.text(x)
}

// functionNode returns the node that holds a function's parameters and body: n itself, or for a
// function assigned to a name (const f = () => …), the function inside it.
func functionNode(n *node) *node {
	queue := []*node{n}
	for depth := 0; depth < 4 && len(queue) > 0; depth++ {
		var next []*node
		for _, x := range queue {
			if x.ChildByFieldName("body") != nil {
				return x
			}
			for _, c := range named(x) {
				if c.Kind() == "function_body" {
					return x
				}
			}
			next = append(next, named(x)...)
		}
		queue = next
	}
	return nil
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return s
}

// fingerprint is facts.Fingerprint of the tokens of fn between the bytes from and to, without
// comments and whitespace, with each parameter and local variable replaced by its number in the order
// the names first appear.
func (cl *codeLang) fingerprint(f *file, fn *node, from, to uint) (string, int) {
	declared := map[string]bool{}
	walk(fn, func(x *node) bool {
		for _, d := range cl.declares(f, x) {
			declared[f.text(d)] = true
		}
		return true
	})
	numbers := map[string]string{}
	var toks []string
	var add func(x, parent *node, from, to uint)
	add = func(x, parent *node, from, to uint) {
		if x.EndByte() <= from || x.StartByte() >= to || strings.Contains(x.Kind(), "comment") {
			return
		}
		if x.ChildCount() == 0 {
			text := f.text(x)
			if cl.idents[x.Kind()] && declared[text] && (parent == nil || !cl.member(x, parent)) {
				if numbers[text] == "" {
					numbers[text] = "$" + strconv.Itoa(len(numbers))
				}
				text = numbers[text]
			}
			toks = append(toks, text)
			return
		}
		if cl.blocks[x.Kind()] { // where indentation alone closes a block, its ends are tokens
			toks = append(toks, "\x01{")
			defer func() { toks = append(toks, "\x01}") }()
		}
		at := max(x.StartByte(), from)
		for i := uint(0); i < x.ChildCount(); i++ {
			c := x.Child(i)
			if c.StartByte() >= at && c.StartByte() <= to {
				if gap := string(f.src[at:c.StartByte()]); strings.TrimSpace(gap) != "" {
					toks = append(toks, gap)
				}
			}
			add(c, x, from, to)
			at = max(at, c.EndByte())
		}
	}
	// What a declaration writes before its parameters and still means: its type parameters, and in
	// Java its result type (Object a() and String b() are different functions).
	for _, field := range []string{"type_parameters", "type"} {
		if c := fn.ChildByFieldName(field); c != nil && c.EndByte() <= from {
			add(c, fn, c.StartByte(), c.EndByte())
		}
	}
	add(fn, nil, from, to)
	return facts.Fingerprint(toks), len(toks)
}

// literals finds the values in a body that belong in configuration.
func (cl *codeLang) literals(f *file, body *node) []facts.Literal {
	var out []facts.Literal
	addAt := func(kind facts.LiteralKind, x *node) {
		out = append(out, facts.Literal{Kind: kind, Line: line(x), Evidence: facts.Evidence(f.text(x))})
	}
	walk(body, func(x *node) bool {
		k := x.Kind()
		if cl.constants[k] {
			return false
		}
		if kind := cl.named(f, x); kind != "" {
			addAt(kind, x)
			return false
		}
		switch {
		case cl.duration != nil && cl.duration(f, x):
			if !cl.allZero(f, x) && !used(f, x, func(o *node) bool { return cl.duration(f, o) }) {
				addAt(facts.LiteralDuration, x)
			}
			return false
		case cl.port != nil && cl.port(f, x) != nil:
			if !facts.Zero(f.text(cl.port(f, x))) {
				addAt(facts.LiteralPort, x)
			}
			return true // the other arguments, a host among them, are read as usual
		case cl.text.literals[k]:
			if s, holes, _ := cl.text.flatten(f, x); len(holes) == 0 {
				if kind := facts.ConfigString(s); kind != "" && !used(f, x, nil) {
					addAt(kind, x)
				}
			}
			return false
		}
		return true
	})
	return out
}

// named is the shape of x when it gives a literal to a name for a port, a time limit, or a name
// rules.code.config-literal.names lists, or "".
func (cl *codeLang) named(f *file, x *node) facts.LiteralKind {
	if cl.pair == nil {
		return ""
	}
	name, value, ok := cl.pair(f, x)
	if !ok || value == nil || !cl.literal(f, value) {
		return ""
	}
	number := cl.numbers[value.Kind()]
	return facts.NamedLiteral(name, number, number && facts.Zero(f.text(value)), f.names)
}

// allZero reports a call whose numbers written out are all 0: no time, any port.
func (cl *codeLang) allZero(f *file, x *node) bool {
	zero := true
	walk(x, func(n *node) bool {
		if cl.numbers[n.Kind()] && !facts.Zero(f.text(n)) {
			zero = false
		}
		return zero
	})
	return zero
}

// ignoring reports a caught error's name that says it is dropped on purpose: _, a name starting
// with _, ignored, expected. Like Rust's let _ =, the name is the author's statement of intent.
func ignoring(name string) bool {
	switch strings.ToLower(name) {
	case "ignored", "ignore", "expected", "unused":
		return true
	}
	return strings.HasPrefix(name, "_")
}

// literal reports a number or a string with nothing interpolated, written out.
func (cl *codeLang) literal(f *file, x *node) bool {
	if cl.numbers[x.Kind()] {
		return true
	}
	if !cl.text.literals[x.Kind()] {
		return false
	}
	_, holes, _ := cl.text.flatten(f, x)
	return len(holes) == 0
}

// numbersOnly reports an argument list of one or more numbers written out.
func numbersOnly(numbers map[string]bool, args *node) bool {
	as := arguments(args)
	for _, a := range as {
		if a.Kind() == "value_argument" && a.NamedChildCount() == 1 { // Kotlin
			a = a.NamedChild(0)
		}
		if !numbers[a.Kind()] {
			return false
		}
	}
	return len(as) > 0
}

// portArgument returns the number written out where class's constructor takes its port, or nil:
// the last argument of InetSocketAddress (host, port), the first of the sockets (port, backlog).
func portArgument(numbers map[string]bool, class string, args *node) *node {
	as := arguments(args)
	if len(as) == 0 {
		return nil
	}
	a := as[0]
	if class == "InetSocketAddress" {
		a = as[len(as)-1]
	}
	if a.Kind() == "value_argument" && a.NamedChildCount() == 1 { // Kotlin
		a = a.NamedChild(0)
	}
	if numbers[a.Kind()] {
		return a
	}
	return nil
}

// className is the last name of a possibly qualified, possibly spaced class name: Duration of
// java.time. Duration.
func className(s string) string { return lastName(strings.Join(strings.Fields(s), "")) }

// emptyBlock reports a block that holds nothing but comments.
func emptyBlock(b *node) bool {
	if b == nil {
		return false
	}
	for _, c := range named(b) {
		if !strings.Contains(c.Kind(), "comment") {
			return false
		}
	}
	return true
}

// declaredIn returns the identifiers of the given kinds under n, leaving out the subtrees skip
// names: a pattern's type annotation or default value, which declare nothing.
func declaredIn(n *node, kinds map[string]bool, skip func(x *node) bool) []*node {
	var out []*node
	walk(n, func(x *node) bool {
		if x != n && skip != nil && skip(x) {
			return false
		}
		if kinds[x.Kind()] {
			out = append(out, x)
		}
		return true
	})
	return out
}

// fieldIs reports whether c is the child of p in the field.
func fieldIs(p *node, field string, c *node) bool {
	got := p.ChildByFieldName(field)
	return got != nil && got.Id() == c.Id()
}
