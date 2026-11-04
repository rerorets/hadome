//go:build cgo

package treesitter

import (
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/rerorets/hadome/internal/facts"
)

var (
	rustTestAttrs = set("test", "tokio::test", "async_std::test", "rstest", "sqlx::test", "test_log::test")
	rustLoops     = set("for_expression", "while_expression", "loop_expression")
)

// rustScope is what the code of one test module binds.
type rustScope struct {
	f       *file
	r       *rust
	t       *tables
	names   map[string]string // local name -> target ("src/store.append", "src/store.Store")
	spaces  map[string]string // local name -> module location
	helpers map[string]*sitter.Node
	mocks   []*sitter.Node
}

// tests reads a test file whole, or a source file's #[cfg(test)] modules.
func (r *rust) tests(f *file, t *tables) []facts.Test {
	if f.test {
		return r.moduleTests(f, t, f.root, "", false)
	}
	var out []facts.Test
	for _, c := range named(f.root) {
		if c.Kind() == "mod_item" && testModule(f, c) && c.ChildByFieldName("body") != nil {
			out = append(out, r.moduleTests(f, t, c.ChildByFieldName("body"), f.field(c, "name")+"::", true)...)
		}
	}
	return out
}

func (r *rust) moduleTests(f *file, t *tables, body *sitter.Node, prefix string, inline bool) []facts.Test {
	sc := &rustScope{f: f, r: r, t: t, names: map[string]string{}, spaces: map[string]string{}, helpers: map[string]*sitter.Node{}}
	for _, u := range append(uses(f, f.root), uses(f, body)...) {
		sc.bind(u, inline)
	}
	var tests []*sitter.Node
	for _, c := range named(body) {
		if c.Kind() != "function_item" {
			continue
		}
		if hasTestAttr(f, c) {
			tests = append(tests, c)
		} else {
			sc.helpers[f.field(c, "name")] = c.ChildByFieldName("body")
		}
	}
	var out []facts.Test
	for _, fn := range tests {
		out = append(out, sc.test(prefix+f.field(fn, "name"), fn))
	}
	return out
}

func hasTestAttr(f *file, fn *sitter.Node) bool {
	for _, a := range attributes(f, fn) {
		if rustTestAttrs[strings.SplitN(a, "(", 2)[0]] {
			return true
		}
	}
	return false
}

// bind records what one use path brings in. In an inline test module, super is the file's module.
func (sc *rustScope) bind(u useLeaf, inline bool) {
	segs := u.segs
	if inline && segs[0] == "super" {
		segs = append([]string{"self"}, segs[1:]...)
	}
	if sc.t.isMock(segs[0]) {
		sc.mocks = append(sc.mocks, u.at)
	}
	loc, internal := sc.r.resolve(sc.f.path, segs)
	last := segs[len(segs)-1]
	switch {
	case !internal:
	case last == "*":
		for _, name := range sc.r.defs[loc] {
			sc.names[name] = loc + "." + name
		}
	case strings.HasSuffix(loc, "/"+last):
		sc.spaces[u.local] = loc
	default:
		sc.names[u.local] = loc + "." + last
	}
}

func (sc *rustScope) test(name string, fn *sitter.Node) facts.Test {
	t := newTest(sc.f, name, fn)
	// The attributes sit before the function, and they hold the inputs of a parameterised test.
	nodes := []*sitter.Node{fn}
	for p := fn.PrevSibling(); p != nil && (p.Kind() == "attribute_item" || strings.Contains(p.Kind(), "comment")); p = p.PrevSibling() {
		nodes = append([]*sitter.Node{p}, nodes...)
	}
	t.fingerprint(nodes, fn.ChildByFieldName("name"))
	for _, a := range attributes(sc.f, fn) {
		if a == "ignore" || strings.HasPrefix(a, "ignore ") || strings.HasPrefix(a, "ignore=") {
			_, reason, _ := strings.Cut(a, "=")
			t.skip(unquote(strings.TrimSpace(reason)), fn)
		}
		if a == "should_panic" || strings.HasPrefix(a, "should_panic(") {
			t.test.Asserts++ // the test fails unless it panics
		}
	}
	for _, m := range sc.mocks {
		t.hazard(facts.HazardMock, m)
	}
	w := &rustWalk{sc: sc, t: t, vars: map[string]string{}, locals: map[string]bool{}, walking: map[string]bool{}}
	w.walk(fn.ChildByFieldName("body"))
	return t.done()
}

type rustWalk struct {
	sc      *rustScope
	t       *testFacts
	vars    map[string]string
	locals  map[string]bool
	walking map[string]bool
}

func (w *rustWalk) walk(body *sitter.Node) {
	w.t.body(body, func(n *sitter.Node) bool { return rustLoops[n.Kind()] }, w.visit)
}

func (w *rustWalk) visit(n *sitter.Node) bool {
	f := w.sc.f
	switch n.Kind() {
	case "let_declaration":
		name := f.field(n, "pattern")
		w.locals[name] = true
		if v := n.ChildByFieldName("value"); v != nil {
			if typ := w.typeOf(v); typ != "" {
				w.vars[name] = typ
			}
		}
	case "macro_invocation":
		w.macro(n)
	case "call_expression":
		w.call(n)
	case "identifier", "crate", "super":
		if w.product(f.text(n)) && n.Parent() != nil && n.Parent().Kind() != "token_tree" { // macroNames counts those
			w.t.readName(n, rustUses)
		}
	}
	return true
}

var rustUses = useGrammar{
	members: map[string]string{"scoped_identifier": "path", "field_expression": "value"},
	names:   map[string]string{"scoped_identifier": "name"},
	callees: map[string][]string{"call_expression": {"function"}},
	types:   set("type_arguments"),
}

// product reports whether a name in a test brings in product code.
func (w *rustWalk) product(name string) bool {
	_, named := w.sc.names[name]
	_, space := w.sc.spaces[name]
	return named || space || name == "crate" || name == "super"
}

// typeOf returns the type a value is built as: Store::new(...) or Store { .. } of a bound type.
func (w *rustWalk) typeOf(v *sitter.Node) string {
	f := w.sc.f
	switch v.Kind() {
	case "call_expression":
		if fn := v.ChildByFieldName("function"); fn.Kind() == "scoped_identifier" {
			return w.sc.names[f.field(fn, "path")]
		}
	case "struct_expression":
		return w.sc.names[f.field(v, "name")]
	}
	return ""
}

func (w *rustWalk) macro(n *sitter.Node) {
	f := w.sc.f
	name := f.field(n, "macro") + "!"
	w.t.assertion(name, w.sc.t.asserts, "panic")
	if cat, ok := w.sc.t.calls[name]; ok {
		w.t.hazard(cat, n)
		return
	}
	var args string
	for _, c := range named(n) {
		if c.Kind() == "token_tree" {
			args = strings.TrimSuffix(strings.TrimPrefix(f.text(c), "("), ")")
		}
	}
	w.macroNames(n)
	parts := strings.Split(args, ",")
	switch {
	case name == "assert!" && strings.TrimSpace(args) == "true",
		(name == "assert_eq!" || name == "assert_ne!") && len(parts) == 2 && strings.TrimSpace(parts[0]) == strings.TrimSpace(parts[1]):
		w.t.hazard(facts.HazardAlwaysTrue, n)
	}
}

// macroNames reads the names inside a macro's arguments. The parser keeps them as tokens, not as
// expressions, so a call cannot be told from a read: a product name counts as a read, and a helper
// named there is walked.
func (w *rustWalk) macroNames(n *sitter.Node) {
	f := w.sc.f
	walk(n, func(c *sitter.Node) bool {
		if c.Kind() != "identifier" && c.Kind() != "crate" && c.Kind() != "super" || c.Parent() == nil || c.Parent().Kind() != "token_tree" {
			return true
		}
		name := f.text(c)
		if w.product(name) {
			w.t.test.Reads++
		} else if body, ok := w.sc.helpers[name]; ok && !w.walking[name] {
			w.walking[name] = true
			w.walk(body)
			delete(w.walking, name)
		}
		return true
	})
}

func (w *rustWalk) call(n *sitter.Node) {
	f := w.sc.f
	fn := n.ChildByFieldName("function")
	callee := f.text(fn)
	w.t.assertion(callee, w.sc.t.asserts)
	if cat, ok := w.sc.t.calls[callee]; ok {
		w.t.hazard(cat, n)
		return
	}
	if w.sc.r.automocks[strings.SplitN(callee, "::", 2)[0]] {
		w.t.hazard(facts.HazardMock, n)
		return
	}
	target := ""
	switch fn.Kind() {
	case "identifier":
		if tg, ok := w.sc.names[callee]; ok {
			target = tg
		} else if body, ok := w.sc.helpers[callee]; ok && !w.walking[callee] {
			w.walking[callee] = true
			w.walk(body)
			delete(w.walking, callee)
		}
	case "scoped_identifier":
		target = w.scoped(f.field(fn, "path"), f.field(fn, "name"))
	case "field_expression":
		obj, field := f.field(fn, "value"), f.field(fn, "field")
		if typ, ok := w.vars[obj]; ok {
			target = typ + "." + field
		} else if w.locals[obj] {
			w.t.test.Unresolved++
		}
	}
	if target != "" {
		w.t.call(target, scopeArg(w.sc.r.scopes, target, named(n.ChildByFieldName("arguments"))))
	}
}

// scoped resolves path::name: a bound module, a bound type, or a crate/self/super path.
func (w *rustWalk) scoped(p, name string) string {
	if loc, ok := w.sc.spaces[p]; ok {
		return loc + "." + name
	}
	if typ, ok := w.sc.names[p]; ok {
		return typ + "." + name
	}
	segs := strings.Split(p, "::")
	if segs[0] == "crate" || segs[0] == "self" || segs[0] == "super" {
		if loc, internal := w.sc.r.resolve(w.sc.f.path, segs); internal {
			return loc + "." + name
		}
	}
	return ""
}
