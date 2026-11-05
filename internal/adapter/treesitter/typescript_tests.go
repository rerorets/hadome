//go:build cgo

package treesitter

import (
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/rerorets/hadome/internal/facts"
)

// tsFile is what a TypeScript test file binds: names imported from the repository, helper
// functions, file-level mocks and comments by line.
type tsFile struct {
	f        *file
	r        *typescript
	t        *tables
	names    map[string]string // local name -> target ("src/store.append", or "src/store.Store" for a class)
	spaces   map[string]string // namespace import -> unit
	helpers  map[string]*sitter.Node
	mocks    []*sitter.Node // file-level evidence that the file mocks
	comments map[int]string
}

var (
	tsSuites = set("describe", "describe.only", "context", "suite")
	tsTests  = set("it", "test", "it.only", "test.only", "it.concurrent", "test.concurrent")
	tsSkips  = set("it.skip", "test.skip", "xit", "xtest", "it.todo", "test.todo", "describe.skip", "xdescribe")
	tsLoops  = set("for_statement", "for_in_statement", "while_statement", "do_statement")
)

func (r *typescript) tests(f *file, t *tables) []facts.Test {
	if !f.test {
		return nil
	}
	tf := &tsFile{f: f, r: r, t: t, names: map[string]string{}, spaces: map[string]string{},
		helpers: map[string]*sitter.Node{}, comments: map[int]string{}}
	tf.bind()
	var out []facts.Test
	tf.suite(f.root, "", false, &out)
	return out
}

func (tf *tsFile) bind() {
	f := tf.f
	for _, c := range comments(f, func(string) bool { return false }) {
		tf.comments[c.Line] = c.Text
	}
	for _, top := range named(f.root) {
		switch top.Kind() {
		case "import_statement":
			spec := unquote(f.field(top, "source"))
			unit, internal := tf.r.resolve(f.path, spec)
			if tf.t.isMock(spec) {
				tf.mocks = append(tf.mocks, top)
			}
			if internal {
				tf.bindImport(top, unit)
			}
		case "function_declaration":
			tf.helpers[f.field(top, "name")] = top.ChildByFieldName("body")
		case "expression_statement":
			if call := top.NamedChild(0); call != nil && call.Kind() == "call_expression" {
				if cat, ok := tf.t.calls[f.field(call, "function")]; ok && cat == facts.HazardMock {
					tf.mocks = append(tf.mocks, call)
				}
			}
		}
	}
}

func (tf *tsFile) bindImport(n *sitter.Node, unit string) {
	walk(n, func(x *sitter.Node) bool {
		switch x.Kind() {
		case "import_specifier":
			name := tf.f.field(x, "name")
			local := tf.f.field(x, "alias")
			if local == "" {
				local = name
			}
			tf.names[local] = unit + "." + name
		case "namespace_import":
			for _, id := range named(x) {
				tf.spaces[tf.f.text(id)] = unit
			}
		case "import_clause":
			if first := x.NamedChild(0); first != nil && first.Kind() == "identifier" {
				tf.names[tf.f.text(first)] = unit + ".default"
			}
		}
		return true
	})
}

// suite finds the tests under n; prefix is the names of the enclosing describe blocks.
func (tf *tsFile) suite(n *sitter.Node, prefix string, skipped bool, out *[]facts.Test) {
	walk(n, func(x *sitter.Node) bool {
		if x.Kind() != "call_expression" {
			return true
		}
		callee := tf.f.field(x, "function")
		args := named(x.ChildByFieldName("arguments"))
		if len(args) < 2 || !(tsSuites[callee] || tsTests[callee] || tsSkips[callee]) {
			return true
		}
		name := prefix + unquote(tf.f.text(args[0]))
		isSkip := skipped || tsSkips[callee]
		if tsSuites[callee] || strings.HasSuffix(callee, "describe") || callee == "describe.skip" {
			tf.suite(args[1], name+" ", isSkip, out)
			return false
		}
		*out = append(*out, tf.test(name, x, args[1], isSkip))
		return false
	})
}

func (tf *tsFile) test(name string, at, fn *sitter.Node, skipped bool) facts.Test {
	t := newTest(tf.f, name, at)
	t.fingerprint([]*sitter.Node{at}, named(at.ChildByFieldName("arguments"))[0])
	if skipped {
		reason := tf.comments[line(at)]
		if reason == "" {
			reason = tf.comments[line(at)-1]
		}
		t.skip(reason, at)
	}
	for _, m := range tf.mocks {
		t.hazard(facts.HazardMock, m)
	}
	w := &tsWalk{tf: tf, t: t, vars: map[string]string{}, locals: map[string]bool{}, walking: map[string]bool{}}
	w.walk(fn.ChildByFieldName("body"))
	return t.done()
}

// tsWalk walks one test body, following helpers of the file.
type tsWalk struct {
	tf      *tsFile
	t       *testFacts
	vars    map[string]string // local variable -> class target, for variables built with new
	locals  map[string]bool
	walking map[string]bool
}

func (w *tsWalk) walk(body *sitter.Node) {
	w.t.body(body, func(n *sitter.Node) bool { return tsLoops[n.Kind()] }, w.visit)
}

func (w *tsWalk) visit(n *sitter.Node) bool {
	f := w.tf.f
	switch n.Kind() {
	case "variable_declarator":
		name := f.field(n, "name")
		w.locals[name] = true
		if v := n.ChildByFieldName("value"); v != nil && v.Kind() == "new_expression" {
			if target, ok := w.tf.names[f.field(v, "constructor")]; ok {
				w.vars[name] = target
			}
		}
	case "new_expression":
		if f.text(n) == "new Date()" {
			w.t.hazard(facts.HazardClock, n)
		} else if target, ok := w.tf.names[f.field(n, "constructor")]; ok {
			w.t.call(target, nil)
		}
	case "call_expression":
		w.call(n)
	case "identifier": // a name in a type is a type_identifier, never an identifier
		if _, ok := w.tf.names[f.text(n)]; ok {
			w.t.readName(n, tsUses)
		} else if _, ok := w.tf.spaces[f.text(n)]; ok {
			w.t.readName(n, tsUses)
		}
	}
	return true
}

var tsUses = useGrammar{
	members: map[string]string{"member_expression": "object"},
	callees: map[string][]string{"call_expression": {"function"}, "new_expression": {"constructor"}},
	types:   set("type_annotation", "type_arguments", "type_query"),
}

func (w *tsWalk) call(n *sitter.Node) {
	f := w.tf.f
	fn := n.ChildByFieldName("function")
	callee := f.text(fn)
	args := named(n.ChildByFieldName("arguments"))
	w.t.assertion(callee, w.tf.t.asserts)
	if cat, ok := w.tf.t.calls[callee]; ok {
		w.t.hazard(cat, n)
		return
	}
	if w.alwaysTrue(fn, args) {
		w.t.hazard(facts.HazardAlwaysTrue, n)
		return
	}
	switch fn.Kind() {
	case "identifier":
		if target, ok := w.tf.names[callee]; ok {
			w.t.call(target, scopeArg(w.tf.r.scopes, target, args))
		} else if body, ok := w.tf.helpers[callee]; ok && !w.walking[callee] {
			w.walking[callee] = true
			w.walk(body)
			delete(w.walking, callee)
		}
	case "member_expression":
		obj, prop := f.field(fn, "object"), f.field(fn, "property")
		target := ""
		if unit, ok := w.tf.spaces[obj]; ok {
			target = unit + "." + prop
		} else if class, ok := w.vars[obj]; ok {
			target = class + "." + prop
		} else if class, ok := w.tf.names[obj]; ok {
			target = class + "." + prop
		} else if w.locals[obj] {
			w.t.test.Unresolved++
		}
		if target != "" {
			w.t.call(target, scopeArg(w.tf.r.scopes, target, args))
		}
	}
}

// alwaysTrue recognises expect(x).toBe(x), expect(true).toBe(true) and assert(true).
func (w *tsWalk) alwaysTrue(fn *sitter.Node, args []*sitter.Node) bool {
	f := w.tf.f
	if f.text(fn) == "assert" || f.text(fn) == "assert.ok" {
		return len(args) == 1 && f.text(args[0]) == "true"
	}
	if fn.Kind() != "member_expression" || len(args) != 1 {
		return false
	}
	switch f.field(fn, "property") {
	case "toBe", "toEqual", "toStrictEqual":
	default:
		return false
	}
	inner := fn.ChildByFieldName("object")
	if inner == nil || inner.Kind() != "call_expression" || f.field(inner, "function") != "expect" {
		return false
	}
	actual := named(inner.ChildByFieldName("arguments"))
	return len(actual) == 1 && sameValue(f, actual[0], args[0])
}
