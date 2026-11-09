//go:build cgo

package treesitter

import (
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/rerorets/hadome/internal/facts"
)

// pyFile is what a Python test file binds.
type pyFile struct {
	f      *file
	r      *python
	t      *tables
	names  map[string]string // local name -> target ("app/store.append")
	spaces map[string]string // local name of an imported module -> module path
	// via are the names imported from a test support module: local name -> the module's path and
	// the name there ("" for the module itself). A test that calls one is followed into it.
	via map[string][2]string
	// unresolved are the names imported from what looks like the repository's own module but
	// resolves to none. Calling one is a call hadome cannot identify.
	unresolved map[string]bool
	helpers    map[string]*sitter.Node
	mocks      []*sitter.Node
	// fixtures types the pytest fixtures of the file: name -> the class it returns or yields.
	fixtures map[string]string
	// fixtureDefs are the pytest fixtures of the file: name -> the function.
	fixtureDefs map[string]*sitter.Node
	// autouse are the names of the file's pytest fixtures marked autouse=True, in file order.
	autouse []string
}

var (
	pySkips = set("pytest.mark.skip", "pytest.mark.skipif", "unittest.skip", "unittest.skipIf", "unittest.skipUnless",
		"skip", "skipIf", "skipUnless", "pytest.skip")
	pyLoops      = set("for_statement", "while_statement")
	pyUses       = useGrammar{members: map[string]string{"attribute": "object"}, names: map[string]string{"attribute": "attribute"}, callees: map[string][]string{"call": {"function"}}, types: set("type")}
	pyConditions = set("if_statement", "elif_clause", "else_clause", "try_statement", "except_clause", "match_statement")
)

func (r *python) tests(f *file, t *tables) []facts.Test {
	// pytest collects tests only from files named like test files, not from conftest.py or helpers.
	if !f.test || (f.support && !f.testGlob && !pyTestFile.MatchString(f.path)) {
		return nil
	}
	pf := r.testFile(f, t)
	var out []facts.Test
	for _, n := range named(f.root) {
		def := definition(n)
		switch {
		case def.Kind() == "function_definition" && strings.HasPrefix(f.field(def, "name"), "test"):
			out = append(out, pf.test(f.field(def, "name"), n, nil))
		case def.Kind() == "class_definition" && pf.testClass(n):
			for _, m := range named(def.ChildByFieldName("body")) {
				if md := definition(m); md.Kind() == "function_definition" && strings.HasPrefix(f.field(md, "name"), "test") {
					out = append(out, pf.test(f.field(def, "name")+"."+f.field(md, "name"), m, n))
				}
			}
		}
	}
	return out
}

func (pf *pyFile) bind() {
	f := pf.f
	for _, im := range pf.r.parseImports(f) {
		if pf.t.isMock(im.dotted) || pf.importsMock(im) {
			pf.mocks = append(pf.mocks, im.at)
		}
		if im.missing {
			for local := range im.names {
				pf.unresolved[local] = true
			}
		}
		if im.internal {
			pf.bindImport(im)
		}
	}
	for _, n := range named(f.root) {
		if def := definition(n); def.Kind() == "function_definition" && !strings.HasPrefix(f.field(def, "name"), "test") {
			pf.helpers[f.field(def, "name")] = def.ChildByFieldName("body")
		}
	}
}

// bindImport binds the names an import of the repository takes: a module (from a import b, when
// b is one), or a name in a module. What comes from a test support module is followed instead.
func (pf *pyFile) bindImport(im pyImport) {
	for local, name := range im.names {
		mod, inner := im.mod, name
		if sub := im.mod + "/" + name; name != "" && (pf.r.exists[sub+".py"] || pf.r.dirs[sub]) {
			mod, inner = sub, ""
		}
		switch {
		case pf.r.support(mod):
			pf.via[local] = [2]string{mod, inner}
		case inner == "":
			pf.spaces[local] = mod
		default:
			pf.names[local] = mod + "." + inner
		}
	}
}

// importsMock reports "from unittest import mock" and the like: a name imported from a module that
// is itself a mocking module.
func (pf *pyFile) importsMock(im pyImport) bool {
	for _, name := range im.names {
		if name != "" && pf.t.isMock(im.dotted+"."+name) {
			return true
		}
	}
	return false
}

// test reads one test; n is its (possibly decorated) definition, class the enclosing class's.
func (pf *pyFile) test(name string, n, class *sitter.Node) facts.Test {
	f := pf.f
	def := definition(n)
	t := newTest(f, name, def)
	t.test.Declared = declaredBefore(f, def, docstringLines(f, def)...)
	var context []codeIn
	if class != nil {
		for _, c := range append(pf.classAutouse(class), pf.classSetups(class)...) {
			context = append(context, codeIn{c.pf.f, c.n})
		}
	}
	// A class's decorators (@pytest.mark.parametrize) give every test in it its inputs.
	var inputs []*sitter.Node
	if class != nil && class.Kind() == "decorated_definition" {
		for _, c := range named(class) {
			if c.Kind() == "decorator" {
				inputs = append(inputs, c)
			}
		}
	}
	t.fingerprint(append(inputs, n), def.ChildByFieldName("name"), context...)
	for _, d := range []*sitter.Node{class, n} {
		if d != nil && d.Kind() == "decorated_definition" {
			pf.decorators(t, d)
		}
	}
	for _, m := range pf.mocks {
		t.fileHazard(facts.HazardMock, m)
	}
	for _, p := range pyParams(f, def) {
		if p == "mocker" {
			t.hazard(facts.HazardMock, def.ChildByFieldName("parameters"))
		}
	}
	pf.setupHazards(t, class)
	w := &pyWalk{body: def.ChildByFieldName("body"), pf: pf, t: t, vars: pf.given(def, class), locals: map[string]bool{}, walking: map[string]bool{}}
	for _, p := range pyParams(f, def) {
		w.fixture(p)
	}
	w.walk(w.body)
	return t.done()
}

func (pf *pyFile) decorators(t *testFacts, d *sitter.Node) {
	for _, dec := range named(d) {
		if dec.Kind() != "decorator" || dec.NamedChildCount() == 0 {
			continue
		}
		e := dec.NamedChild(0)
		callee, args := pf.f.text(e), (*sitter.Node)(nil)
		if e.Kind() == "call" {
			callee, args = pf.f.field(e, "function"), e.ChildByFieldName("arguments")
		}
		if pySkips[callee] {
			t.skip(pf.reason(args), dec)
		}
		if strings.HasSuffix(callee, "patch") || strings.Contains(callee, "patch.") {
			t.hazard(facts.HazardMock, dec)
		}
	}
}

// reason is the reason= argument of a skip, or else its first string argument.
func (pf *pyFile) reason(args *sitter.Node) string {
	if args == nil {
		return ""
	}
	first := ""
	for _, a := range named(args) {
		if a.Kind() == "keyword_argument" && pf.f.field(a, "name") == "reason" {
			return unquote(pf.f.field(a, "value"))
		}
		if a.Kind() == "string" && first == "" {
			first = unquote(pf.f.text(a))
		}
	}
	return first
}

type pyWalk struct {
	body    *sitter.Node // the test's own body; branches above it do not count
	pf      *pyFile
	t       *testFacts
	vars    map[string]string
	locals  map[string]bool
	walking map[string]bool
}

func (w *pyWalk) walk(body *sitter.Node) {
	w.t.body(body, func(n *sitter.Node) bool { return pyLoops[n.Kind()] }, w.visit)
}

func (w *pyWalk) visit(n *sitter.Node) bool {
	f := w.pf.f
	switch n.Kind() {
	case "assignment":
		left, right := n.ChildByFieldName("left"), n.ChildByFieldName("right")
		if left != nil && left.Kind() == "identifier" {
			w.locals[f.text(left)] = true
			delete(w.vars, f.text(left)) // reassigning a fixture's name drops what the fixture gave
			if right != nil && right.Kind() == "call" {
				if target, ok := w.pf.names[f.field(right, "function")]; ok {
					w.vars[f.text(left)] = target
				}
			}
		}
	case "assert_statement":
		w.t.test.Asserts++
		if alwaysTruePy(f, n) {
			w.t.hazard(facts.HazardAlwaysTrue, n)
		}
	case "call":
		w.call(n)
	case "identifier":
		if _, ok := w.pf.names[f.text(n)]; ok {
			w.t.readName(n, pyUses)
		} else if _, ok := w.pf.spaces[f.text(n)]; ok {
			w.t.readName(n, pyUses)
		}
	}
	return true
}

func alwaysTruePy(f *file, n *sitter.Node) bool {
	if n.NamedChildCount() == 0 {
		return false
	}
	e := n.NamedChild(0)
	if e.Kind() == "true" {
		return true
	}
	ops := named(e)
	return e.Kind() == "comparison_operator" && len(ops) == 2 && strings.Contains(f.text(e), "==") && sameValue(f, ops[0], ops[1])
}

func (w *pyWalk) call(n *sitter.Node) {
	f := w.pf.f
	fn := n.ChildByFieldName("function")
	callee := f.text(fn)
	args := n.ChildByFieldName("arguments")
	if callee == "pytest.skip" {
		if conditional(n, w.body, pyConditions) {
			w.t.skipIf(w.pf.reason(args), n)
		} else {
			w.t.skip(w.pf.reason(args), n)
		}
		return
	}
	w.checks(callee, args)
	if cat, ok := w.pf.t.calls[callee]; ok {
		w.t.hazard(cat, n)
		return
	}
	if target := w.target(fn); target != "" {
		w.t.call(target, scopeArg(w.pf.r.scopes, target, positional(args)))
	}
}

// target names what a call's function is when it is the product's. Else it follows a helper, a
// function of a test support module, or counts the call as unresolved when it cannot tell.
func (w *pyWalk) target(fn *sitter.Node) string {
	f := w.pf.f
	if fn.Kind() == "identifier" {
		name := f.text(fn)
		if tg, ok := w.pf.names[name]; ok {
			return tg
		}
		if v, ok := w.pf.via[name]; ok && v[1] != "" {
			w.followSupport(v[0], v[1])
		} else if w.pf.unresolved[name] {
			w.t.test.Unresolved++
		} else {
			w.follow(name)
		}
		return ""
	}
	if fn.Kind() != "attribute" {
		return ""
	}
	obj, attr := f.field(fn, "object"), f.field(fn, "attribute")
	// A method called on what a call returns at once (Store().append()) is typed as a local that
	// holds the result would be.
	if o := fn.ChildByFieldName("object"); o.Kind() == "call" {
		if owner, ok := w.pf.names[f.field(o, "function")]; ok {
			return owner + "." + attr
		}
	}
	for _, owners := range []map[string]string{w.pf.spaces, w.vars, w.pf.names} {
		if owner, ok := owners[obj]; ok {
			return owner + "." + attr
		}
	}
	if v, ok := w.pf.via[obj]; ok && v[1] == "" {
		w.followSupport(v[0], attr)
	} else if w.pf.unresolved[obj] || w.locals[obj] {
		w.t.test.Unresolved++
	}
	return ""
}

func positional(args *sitter.Node) []*sitter.Node {
	if args == nil {
		return nil
	}
	var out []*sitter.Node
	for _, a := range named(args) {
		if a.Kind() != "keyword_argument" {
			out = append(out, a)
		}
	}
	return out
}

// scopeArg returns the argument at the target's scope position, or the first when unknown.
func scopeArg(scopes map[string]int, target string, args []*sitter.Node) *sitter.Node {
	pos, ok := scopes[target]
	if !ok {
		pos = 0
	}
	if pos >= 0 && pos < len(args) {
		return args[pos]
	}
	return nil
}

// checks counts a call that checks something. A method of the test's own class, or self handed on,
// may check something the adapter does not read.
func (w *pyWalk) checks(callee string, args *sitter.Node) {
	switch {
	case pytestChecks[callee]:
		w.t.test.Asserts++
	case w.t.assertion(callee, w.pf.t.asserts):
	case strings.HasPrefix(callee, "self.") && strings.Count(callee, ".") == 1, passes(w.pf.f, args, "self"):
		w.t.test.AssertsUnknown = true
	}
}

// pytestChecks fail the test unless something happens.
var pytestChecks = set("pytest.raises", "pytest.fail", "pytest.warns")

// passes reports whether one of the arguments is the name itself.
func passes(f *file, args *sitter.Node, name string) bool {
	if args == nil {
		return false
	}
	for _, a := range named(args) {
		if f.text(a) == name {
			return true
		}
	}
	return false
}

// docstringLines returns the lines of a function's docstring. Python's doc comment lives there
// rather than before the function.
func docstringLines(f *file, def *sitter.Node) []string {
	if !docstring(def) {
		return nil
	}
	return strings.Split(strings.Trim(f.text(def.ChildByFieldName("body").NamedChild(0)), `"'`), "\n")
}
