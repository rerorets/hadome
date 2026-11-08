//go:build cgo

package treesitter

import (
	"maps"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/rerorets/hadome/internal/facts"
)

// How a TypeScript test file declares its suites and tests, and what each test gets from the
// describe blocks around it.

// The heads are the functions that declare suites and tests; the parts after the head (it.only,
// test.describe.serial, it.each(table)) say how. A part outside tsParts makes the call something
// else (test.step, test.use).
var (
	tsSuiteHeads = set("describe", "context", "suite", "xdescribe")
	tsTestHeads  = set("it", "test", "xit", "xtest")
	tsParts      = set("only", "skip", "todo", "fixme", "concurrent", "sequential", "serial", "parallel", "fails", "fail",
		"describe", "each", "for", "skipIf", "runIf")
	// tsCurried are the parts that take an argument first and return the function that declares:
	// it.each(table)(name, fn), it.skipIf(cond)(name, fn).
	tsCurried = set("each", "for", "skipIf", "runIf")
)

// tsDecl is a call that declares a suite or a test.
type tsDecl struct {
	suite       bool
	skipped     bool // never runs: .skip, .todo, .fixme, xit
	conditional bool // runs only in some cases: .skipIf(cond), .runIf(cond)
	table       *sitter.Node
	name, fn    *sitter.Node
}

// declaration reads x as a declaration of a suite or a test: a head and its parts, called with a
// name first and a function after it. The function is the last argument that is one, since vitest
// takes options between the name and the function and jest takes a timeout after it.
func (tf *tsFile) declaration(x *sitter.Node) (tsDecl, bool) {
	callee := x.ChildByFieldName("function")
	var first *sitter.Node // the call that takes the table or the condition
	if callee != nil && callee.Kind() == "call_expression" {
		first, callee = callee, callee.ChildByFieldName("function")
	}
	if callee == nil || (callee.Kind() != "identifier" && callee.Kind() != "member_expression") {
		return tsDecl{}, false
	}
	d, curried, tabled, ok := tsReadParts(strings.Split(tf.f.text(callee), "."))
	if !ok || curried != (first != nil) {
		return tsDecl{}, false
	}
	if tabled {
		d.table = first.ChildByFieldName("arguments")
	}
	d.name, d.fn = tsBody(arguments(x.ChildByFieldName("arguments")))
	return d, d.fn != nil
}

// tsBody finds the name and the function among the arguments of a declaration.
func tsBody(args []*sitter.Node) (name, fn *sitter.Node) {
	for i := len(args) - 1; i >= 1; i-- {
		if k := args[i].Kind(); k == "arrow_function" || k == "function_expression" {
			return args[0], args[i]
		}
	}
	// it('adds', checkAdd): a function passed by name, after a name that is a string, since
	// Playwright's test.skip(isMobile, reason) passes a condition there.
	if len(args) >= 2 && (args[0].Kind() == "string" || args[0].Kind() == "template_string") && args[1].Kind() == "identifier" {
		return args[0], args[1]
	}
	return nil, nil
}

// tsReadParts reads what the parts of a callee declare. curried is true when a part takes a table
// or a condition first, and tabled when that is a table.
func tsReadParts(parts []string) (d tsDecl, curried, tabled, ok bool) {
	head := parts[0]
	if !tsSuiteHeads[head] && !tsTestHeads[head] {
		return d, false, false, false
	}
	d.suite = tsSuiteHeads[head]
	d.skipped = strings.HasPrefix(head, "x")
	for _, p := range parts[1:] {
		if !tsParts[p] {
			return d, false, false, false
		}
		curried = curried || tsCurried[p]
		switch p {
		case "describe":
			d.suite = true
		case "skip", "todo", "fixme":
			d.skipped = true
		case "skipIf", "runIf":
			d.conditional = true
		case "each", "for":
			tabled = true
		}
	}
	return d, curried, tabled, true
}

// tsScope is what the enclosing describe blocks give each test in them.
type tsScope struct {
	prefix      string
	skipped     bool
	conditional bool
	skipAt      *sitter.Node // the declaration that skips the suite, whose comment gives the reason
	// preludes run before each test of the scope: the bodies of beforeEach and beforeAll, the
	// tables of .each, and describe-level test.skip(cond) and fake timers.
	preludes []*sitter.Node
	// rows bind the parameters of the describe.each callbacks around the test (see tsWalk.rows).
	rows map[string][]string
}

func (sc tsScope) with(preludes ...*sitter.Node) tsScope {
	sc.preludes = append(append([]*sitter.Node{}, sc.preludes...), preludes...)
	return sc
}

// bind gives the tests of the scope the bindings of a describe.each callback's parameters.
func (sc tsScope) bind(rows map[string][]string) tsScope {
	sc.rows = maps.Clone(sc.rows)
	if sc.rows == nil {
		sc.rows = map[string][]string{}
	}
	maps.Copy(sc.rows, rows)
	return sc
}

// tabled is the scope inside a declaration with a table: the table runs with each test, and the
// parameters of a describe.each callback hold its rows.
func (tf *tsFile) tabled(sc tsScope, d tsDecl) tsScope {
	if d.table == nil {
		return sc
	}
	sc = sc.with(d.table)
	if d.suite {
		sc = sc.bind(tf.rowBindings(d.table, tsParams(d.fn)))
	}
	return sc
}

// helperOfSupport reports whether x is a helper function of a test support file that is not also a
// test file.
func (tf *tsFile) helperOfSupport(x *sitter.Node) bool {
	return tf.f.support && !tf.f.testGlob && !tsTestFile.MatchString(tf.f.path) && tsHelperFunction(x)
}

// suite finds the tests under n, the program or the body of a describe block.
func (tf *tsFile) suite(n *sitter.Node, sc tsScope, out *[]facts.Test) {
	sc = sc.with(tf.preludes(n)...)
	walk(n, func(x *sitter.Node) bool {
		if tf.helperOfSupport(x) {
			return false // a support file's helper declares its tests in the file that calls it
		}
		if x.Kind() != "call_expression" {
			return true
		}
		d, ok := tf.declaration(x)
		if !ok {
			return true
		}
		inner := tf.tabled(sc, d)
		if (d.skipped && !inner.skipped) || (d.conditional && !inner.skipped && !inner.conditional) {
			inner.skipAt = x
		}
		inner.skipped = inner.skipped || d.skipped
		inner.conditional = inner.conditional || d.conditional
		name := sc.prefix + unquote(tf.f.text(d.name))
		if d.suite {
			inner.prefix = name + " "
			tf.suite(d.fn.ChildByFieldName("body"), inner, out)
			return false
		}
		*out = append(*out, tf.test(name, x, d, inner))
		return false
	})
}

func (tf *tsFile) test(name string, at *sitter.Node, d tsDecl, sc tsScope) facts.Test {
	t := newTest(tf.f, name, at)
	t.fingerprint([]*sitter.Node{at}, d.name)
	if sc.skipped || sc.conditional {
		reason := tf.reasonAt(sc.skipAt)
		if reason == "" {
			reason = tf.reasonAt(at)
		}
		if sc.skipped {
			t.skip(reason, at)
		} else {
			t.skipIf(reason, at)
		}
	}
	for _, m := range tf.mocks {
		t.fileHazard(facts.HazardMock, m)
	}
	w := &tsWalk{tf: tf, t: t, at: at, vars: tf.outerVars(), locals: map[string]bool{}, walking: map[string]bool{},
		loaded: maps.Clone(tf.outerLoaded), modules: maps.Clone(tf.outerModules), used: map[string]bool{}, done: map[uintptr]bool{},
		plain: map[string]bool{}, rows: maps.Clone(sc.rows)}
	if w.rows == nil {
		w.rows = map[string][]string{}
	}
	for _, unit := range tf.sideEffects {
		t.call(unit, nil)
	}
	for _, p := range sc.preludes {
		w.prelude(p)
	}
	if d.fn.Kind() == "identifier" {
		w.follow(tf.f.text(d.fn), nil)
	} else {
		t.test.Drives = t.test.Drives || tf.receivesBrowser(d.fn)
		w.params(tsParams(d.fn))
		if d.table != nil {
			maps.Copy(w.rows, tf.rowBindings(d.table, tsParams(d.fn)))
		}
		w.walk(d.fn.ChildByFieldName("body"))
	}
	return t.done()
}

// reasonAt is the comment on the line of n or the line before it.
func (tf *tsFile) reasonAt(n *sitter.Node) string {
	if n == nil {
		return ""
	}
	if r := tf.comments[line(n)]; r != "" {
		return r
	}
	return tf.comments[line(n)-1]
}

// tsHelperFunction reports whether n is a function that runs only when something calls it: a
// declaration, a method, or a function held by a name or returned. A function passed straight to a
// call ([1, 2].forEach((n) => it(…))) runs where it is written.
func tsHelperFunction(n *sitter.Node) bool {
	if !tsFunctions[n.Kind()] {
		return false
	}
	parent := n.Parent()
	return parent == nil || parent.Kind() != "arguments"
}
