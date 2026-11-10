//go:build cgo

package treesitter

import (
	"maps"
	"path"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

// What runs around a TypeScript test: the setup of the variables it uses, the hooks of its describe
// blocks, and the default values of the helpers it calls.

var (
	tsHooks     = set("beforeEach", "beforeAll", "before", "test.beforeEach", "test.beforeAll")
	tsFunctions = set("arrow_function", "function_expression", "function_declaration", "generator_function",
		"generator_function_declaration", "method_definition")
)

// tsSetup is one node that sets up a variable, and the program or describe body it runs in: only
// the tests in that scope run it.
type tsSetup struct{ node, scope *sitter.Node }

// tsSetups collects the setups of a file's outer variables (see bindSetups).
type tsSetups struct {
	tf         *tsFile
	declared   map[string]bool
	assigned   map[string][]tsSetup
	statements []tsSetup
}

// bindSetups records what sets up each variable declared outside the tests, at the top of the file
// or in a describe block: its initializer, what is assigned to it there or in a hook, and the
// statements there that use it (register(app)). A test that uses the variable runs them. A
// variable holding a function is a helper instead, followed when called. Names taken from a module
// loaded with import() are recorded as such, and a bare await import('./m') as the file's side
// effect.
func (tf *tsFile) bindSetups() {
	s := &tsSetups{tf: tf, declared: map[string]bool{}, assigned: map[string][]tsSetup{}}
	s.visit(tf.f.root, tf.f.root, true, false)
	for name, values := range s.assigned {
		if s.declared[name] {
			tf.setups[name] = append(tf.setups[name], values...)
		}
	}
	for _, st := range s.statements {
		for name := range s.uses(st.node) {
			tf.setups[name] = append(tf.setups[name], st)
		}
	}
	for _, setups := range tf.setups {
		for _, st := range setups {
			tf.setupNodes[st.node.Id()] = true
		}
	}
}

// visit collects under n. scope is the program or describe body n is in; outer is true for the
// statements of that scope, and hook for the body of a hook.
func (s *tsSetups) visit(n, scope *sitter.Node, outer, hook bool) {
	switch n.Kind() {
	case "call_expression":
		if s.nested(n, scope) {
			return
		}
	case "variable_declarator":
		if outer && s.declarator(n, scope) {
			return
		}
	case "assignment_expression":
		if outer || hook {
			s.assignment(n, scope)
		}
	case "expression_statement":
		if outer && s.statement(n, scope) {
			return
		}
	}
	if tsFunctions[n.Kind()] {
		outer, hook = false, false
	}
	for i := uint(0); i < n.ChildCount(); i++ {
		s.visit(n.Child(i), scope, outer, hook)
	}
}

// nested visits the body of a describe block as a scope of its own and the body of a hook as a
// hook, and leaves out the body of a test, whose variables are its locals. It reports whether n
// was one of them.
func (s *tsSetups) nested(n, scope *sitter.Node) bool {
	if d, ok := s.tf.declaration(n); ok {
		if body := d.fn.ChildByFieldName("body"); d.suite && body != nil {
			s.visit(body, body, true, false)
		}
		return true
	}
	if !tsHooks[s.tf.f.field(n, "function")] {
		return false
	}
	if fn := tsLastFunction(n); fn != nil && fn.ChildByFieldName("body") != nil {
		s.visit(fn.ChildByFieldName("body"), scope, false, true)
	}
	return true
}

// note records a value assigned to a name, unless it is a function: a helper, followed when called.
func (s *tsSetups) note(name string, value, scope *sitter.Node) {
	if v := tsUnwrap(value); v != nil && !tsFunctions[v.Kind()] {
		s.assigned[name] = append(s.assigned[name], tsSetup{value, scope})
	}
}

// declarator records an outer declaration, and reports whether it loads a module.
func (s *tsSetups) declarator(n, scope *sitter.Node) bool {
	pattern, value := n.ChildByFieldName("name"), n.ChildByFieldName("value")
	if scope.Kind() == "program" && s.tf.required(value) != "" {
		return true // bind reads a require() at the top of the file, into a support file too
	}
	if s.tf.bindLoad(pattern, value, s.tf.outerModules, s.tf.outerLoaded) {
		return true
	}
	for _, name := range tsPatternNames(s.tf.f, pattern) {
		s.declared[name] = true
		if value != nil {
			s.note(name, value, scope)
		}
	}
	return false
}

func (s *tsSetups) assignment(n, scope *sitter.Node) {
	left, right := n.ChildByFieldName("left"), n.ChildByFieldName("right")
	if left != nil && left.Kind() == "identifier" && !s.tf.bindLoad(left, right, s.tf.outerModules, s.tf.outerLoaded) {
		s.note(s.tf.f.text(left), right, scope)
	}
}

// statement records a statement of a scope that may use outer variables, or a bare await
// import('./m'), and reports whether it was the latter.
func (s *tsSetups) statement(n, scope *sitter.Node) bool {
	e := n.NamedChild(0)
	if unit := s.tf.dynamicImport(tsUnwrap(e)); unit != "" {
		s.tf.sideEffects = append(s.tf.sideEffects, s.tf.unitOf(unit))
		return true
	}
	if e != nil && e.Kind() == "call_expression" {
		if _, ok := s.tf.declaration(e); ok || tsHooks[s.tf.f.field(e, "function")] {
			return false
		}
	}
	s.statements = append(s.statements, tsSetup{e, scope})
	return false
}

// uses are the outer variables a statement names outside the functions it passes.
func (s *tsSetups) uses(n *sitter.Node) map[string]bool {
	out := map[string]bool{}
	walk(n, func(x *sitter.Node) bool {
		if x.Kind() == "identifier" && s.declared[s.tf.f.text(x)] {
			out[s.tf.f.text(x)] = true
		}
		return !tsFunctions[x.Kind()]
	})
	return out
}

// unitOf is the unit of a file of the repository.
func (tf *tsFile) unitOf(p string) string {
	if f := tf.r.files[p]; f != nil {
		return f.unit
	}
	return path.Dir(p)
}

// tsPatternNames are the names a declaration binds: store, or a and b in const { a, b: [b] } = x.
func tsPatternNames(f *file, pattern *sitter.Node) []string {
	if pattern == nil {
		return nil
	}
	var out []string
	walk(pattern, func(x *sitter.Node) bool {
		switch x.Kind() {
		case "identifier", "shorthand_property_identifier_pattern":
			out = append(out, f.text(x))
		case "pair_pattern":
			walk(x.ChildByFieldName("value"), func(v *sitter.Node) bool {
				if v.Kind() == "identifier" || v.Kind() == "shorthand_property_identifier_pattern" {
					out = append(out, f.text(v))
				}
				return true
			})
			return false
		}
		return true
	})
	return out
}

// tsLastFunction is the last argument of a call that is a function: the body of a hook.
func tsLastFunction(call *sitter.Node) *sitter.Node {
	args := arguments(call.ChildByFieldName("arguments"))
	for i := len(args) - 1; i >= 0; i-- {
		if k := args[i].Kind(); k == "arrow_function" || k == "function_expression" {
			return args[i]
		}
	}
	return nil
}

// preludes are what n, the program or the body of a describe block, runs before each of its tests:
// the bodies of its hooks, and its statements that skip the tests or fake the clock.
func (tf *tsFile) preludes(n *sitter.Node) []*sitter.Node {
	if n == nil || (n.Kind() != "program" && n.Kind() != "statement_block") {
		return nil
	}
	var out []*sitter.Node
	for _, s := range named(n) {
		call := tsUnwrap(s.NamedChild(0))
		if s.Kind() != "expression_statement" || call == nil || call.Kind() != "call_expression" {
			continue
		}
		callee := tf.f.field(call, "function")
		switch {
		case tsHooks[callee]:
			if fn := tsLastFunction(call); fn != nil {
				out = append(out, fn.ChildByFieldName("body"))
			}
		case tsInBodySkips[callee]:
			if _, ok := tf.declaration(call); !ok {
				out = append(out, call)
			}
		default:
			// A test or a suite is never a prelude, so its body, which may be the whole suite, is
			// not searched for fake timers.
			if _, ok := tf.declaration(call); !ok && tf.fakesTimers(call) {
				out = append(out, call)
			}
		}
	}
	return out
}

// fakesTimers reports whether a statement turns on fake timers: vi.useFakeTimers(), or
// vi.useFakeTimers().setSystemTime(t).
func (tf *tsFile) fakesTimers(call *sitter.Node) bool {
	found := false
	walk(call, func(x *sitter.Node) bool {
		found = found || (x.Kind() == "call_expression" && tsFakeTimers[tf.f.field(x, "function")])
		return !found
	})
	return found
}

// prelude walks something that runs before the test. Its own locals stay its own.
func (w *tsWalk) prelude(n *sitter.Node) {
	locals, vars := maps.Clone(w.locals), maps.Clone(w.vars)
	w.walk(n)
	w.locals, w.vars = locals, vars
}

// tsParams is the parameter list of a function, or the one parameter of x => ….
func tsParams(fn *sitter.Node) *sitter.Node {
	if fn == nil {
		return nil
	}
	if p := fn.ChildByFieldName("parameters"); p != nil {
		return p
	}
	return fn.ChildByFieldName("parameter")
}

// params makes the names a parameter list binds locals: (row) => in it.each, ({ page }) => in
// Playwright, and the parameters of a helper while it is followed. Default values bind nothing.
func (w *tsWalk) params(p *sitter.Node) {
	walk(p, func(x *sitter.Node) bool {
		switch x.Kind() {
		case "required_parameter", "optional_parameter":
			w.params(x.ChildByFieldName("pattern"))
			return false
		case "assignment_pattern":
			w.params(x.ChildByFieldName("left"))
			return false
		case "identifier", "shorthand_property_identifier_pattern":
			w.locals[w.tf.f.text(x)] = true
		case "type_annotation":
			return false
		}
		return true
	})
}

// defaults walks the default values of a helper's parameters, which run when the helper is called
// without them: function make(store = createStore()).
func (w *tsWalk) defaults(params *sitter.Node) {
	if params == nil {
		return
	}
	for _, p := range named(params) {
		switch p.Kind() {
		case "required_parameter", "optional_parameter":
			if v := p.ChildByFieldName("value"); v != nil {
				w.walk(v)
			}
		case "assignment_pattern":
			if v := p.ChildByFieldName("right"); v != nil {
				w.walk(v)
			}
		}
	}
}

// useOuter walks the setup of a variable declared outside the test, the first time the test uses it.
func (w *tsWalk) useOuter(name string) {
	if w.locals[name] || w.used[name] {
		return
	}
	setups, ok := w.tf.setups[name]
	if !ok {
		return
	}
	w.used[name] = true
	w.setup++
	for _, s := range setups {
		if w.inScope(s.scope) {
			w.t.setups = append(w.t.setups, codeIn{w.tf.f, s.node})
			w.walk(s.node)
		}
	}
	w.setup--
}

// inScope reports whether the test runs in scope, a program or describe body. In a support file the
// walker follows a helper outside any test, so only the program of that file applies.
func (w *tsWalk) inScope(scope *sitter.Node) bool {
	if w.at == nil {
		return scope.Kind() == "program"
	}
	return scope.StartByte() <= w.at.StartByte() && w.at.EndByte() <= scope.EndByte()
}

// tsHeldFunction reports whether n, a member of an object, holds a function that runs only when
// called: { add: () => … } or { add() { … } }. A getter runs when read, so it is not one.
func tsHeldFunction(n *sitter.Node) bool {
	if n.Kind() == "pair" {
		v := n.ChildByFieldName("value")
		return v != nil && (v.Kind() == "arrow_function" || v.Kind() == "function_expression")
	}
	for i := uint(0); i < n.ChildCount(); i++ {
		if k := n.Child(i).Kind(); k == "get" {
			return false
		}
	}
	return true
}
