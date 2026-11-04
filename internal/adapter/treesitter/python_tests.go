//go:build cgo

package treesitter

import (
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/rerorets/hadome/internal/facts"
)

// pyFile is what a Python test file binds.
type pyFile struct {
	f       *file
	r       *python
	t       *tables
	names   map[string]string // local name -> target ("app/store.append")
	spaces  map[string]string // local name of an imported module -> module path
	helpers map[string]*sitter.Node
	mocks   []*sitter.Node
}

var (
	pySkips = set("pytest.mark.skip", "pytest.mark.skipif", "unittest.skip", "unittest.skipIf", "unittest.skipUnless",
		"skip", "skipIf", "skipUnless", "pytest.skip")
	pyLoops      = set("for_statement", "while_statement")
	pyUses       = useGrammar{members: map[string]string{"attribute": "object"}, names: map[string]string{"attribute": "attribute"}, callees: map[string][]string{"call": {"function"}}, types: set("type")}
	pyConditions = set("if_statement", "elif_clause", "else_clause", "try_statement", "except_clause", "match_statement")
)

func (r *python) tests(f *file, t *tables) []facts.Test {
	if !f.test {
		return nil
	}
	pf := &pyFile{f: f, r: r, t: t, names: map[string]string{}, spaces: map[string]string{}, helpers: map[string]*sitter.Node{}}
	pf.bind()
	var out []facts.Test
	for _, n := range named(f.root) {
		def := definition(n)
		switch {
		case def.Kind() == "function_definition" && strings.HasPrefix(f.field(def, "name"), "test"):
			out = append(out, pf.test(f.field(def, "name"), n, nil))
		case def.Kind() == "class_definition" && strings.HasPrefix(f.field(def, "name"), "Test"):
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
		if !im.internal {
			continue
		}
		for local, name := range im.names {
			sub := im.mod + "/" + name
			switch {
			case name == "":
				pf.spaces[local] = im.mod
			case pf.r.exists[sub+".py"] || pf.r.dirs[sub]:
				pf.spaces[local] = sub
			default:
				pf.names[local] = im.mod + "." + name
			}
		}
	}
	for _, n := range named(f.root) {
		if def := definition(n); def.Kind() == "function_definition" && !strings.HasPrefix(f.field(def, "name"), "test") {
			pf.helpers[f.field(def, "name")] = def.ChildByFieldName("body")
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
	t.fingerprint([]*sitter.Node{n}, def.ChildByFieldName("name"))
	for _, d := range []*sitter.Node{class, n} {
		if d != nil && d.Kind() == "decorated_definition" {
			pf.decorators(t, d)
		}
	}
	for _, m := range pf.mocks {
		t.hazard(facts.HazardMock, m)
	}
	for _, p := range pyParams(f, def) {
		if p == "mocker" {
			t.hazard(facts.HazardMock, def.ChildByFieldName("parameters"))
		}
	}
	w := &pyWalk{body: def.ChildByFieldName("body"), pf: pf, t: t, vars: map[string]string{}, locals: map[string]bool{}, walking: map[string]bool{}}
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
			if right != nil && right.Kind() == "call" {
				if target, ok := w.pf.names[f.field(right, "function")]; ok {
					w.vars[f.text(left)] = target
				}
			}
		}
	case "assert_statement":
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
	return e.Kind() == "comparison_operator" && len(ops) == 2 && strings.Contains(f.text(e), "==") && f.text(ops[0]) == f.text(ops[1])
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
	if cat, ok := w.pf.t.calls[callee]; ok {
		w.t.hazard(cat, n)
		return
	}
	target := ""
	switch fn.Kind() {
	case "identifier":
		if tg, ok := w.pf.names[callee]; ok {
			target = tg
		} else if body, ok := w.pf.helpers[callee]; ok && !w.walking[callee] {
			w.walking[callee] = true
			w.walk(body)
			delete(w.walking, callee)
		}
	case "attribute":
		obj, attr := f.field(fn, "object"), f.field(fn, "attribute")
		if mod, ok := w.pf.spaces[obj]; ok {
			target = mod + "." + attr
		} else if class, ok := w.vars[obj]; ok {
			target = class + "." + attr
		} else if class, ok := w.pf.names[obj]; ok {
			target = class + "." + attr
		} else if w.locals[obj] {
			w.t.test.Unresolved++
		}
	}
	if target != "" {
		w.t.call(target, scopeArg(w.pf.r.scopes, target, positional(args)))
	}
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
