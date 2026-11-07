//go:build cgo

package treesitter

import (
	"maps"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/rerorets/hadome/internal/facts"
)

// tsFile is what a TypeScript test file binds: names imported from the repository, helper
// functions, file-level mocks and comments by line.
type tsFile struct {
	f      *file
	r      *typescript
	t      *tables
	names  map[string]string // local name -> target ("src/store.append", or "src/store.Store" for a class)
	spaces map[string]string // namespace import -> the file it names
	// via are the names imported from a test support file: local name -> the file and the name there.
	// A test that calls one is followed into it.
	via map[string][2]string
	// unresolved are the names imported from what looks like the repository's own code but names
	// no file of it. Calling one is a call hadome cannot identify.
	unresolved map[string]bool
	helpers    map[string]*sitter.Node
	// loaders are the functions of the file that load one module of the repository with import():
	// function name -> unit. A test that takes names from what they return calls that module.
	loaders map[string]string
	// outer types the variables declared outside the tests (in a describe block, set in beforeEach):
	// name -> class, by the class they are built with or declared as. A name given two classes is "".
	outer map[string]string
	mocks []*sitter.Node // file-level evidence that the file mocks
	// drivers are the local names of the HTTP test clients the file imports (supertest's request),
	// whose calls drive the application from outside.
	drivers  map[string]bool
	comments map[int]string
	// params are the parameter lists of the helpers, whose default values run on each call.
	params map[string]*sitter.Node
	// setups are what sets up each variable declared outside the tests (see bindSetups), and
	// setupNodes the same nodes, by ID.
	setups     map[string][]tsSetup
	setupNodes map[uintptr]bool
	// outerLoaded and outerModules are the names outside the tests taken from a module loaded with
	// import(), like the walker's loaded and modules.
	outerLoaded  map[string]string
	outerModules map[string]string
	// sideEffects are the units the file loads with a bare await import('./m') outside its tests.
	sideEffects []string
}

var tsLoops = set("for_statement", "for_in_statement", "while_statement", "do_statement")

func (r *typescript) tests(f *file, t *tables) []facts.Test {
	if !f.test {
		return nil
	}
	tf := r.testFile(f, t)
	var out []facts.Test
	tf.suite(f.root, tsScope{}, &out)
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
			if tf.t.isMock(spec) {
				tf.mocks = append(tf.mocks, top)
			}
			if spec == "supertest" {
				tf.bindDriver(top)
			}
			tf.bindImport(top, spec)
		case "lexical_declaration", "variable_declaration":
			for _, d := range named(top) {
				if spec := tf.required(d.ChildByFieldName("value")); spec != "" {
					tf.bindImport(d.ChildByFieldName("name"), spec)
				}
			}
		case "expression_statement":
			if call := top.NamedChild(0); call != nil && call.Kind() == "call_expression" {
				if cat, ok := tf.t.calls[f.field(call, "function")]; ok && cat == facts.HazardMock {
					tf.mocks = append(tf.mocks, call)
				}
			}
		}
	}
}

// tsWalk walks one test body, following helpers of the file.
type tsWalk struct {
	tf      *tsFile
	t       *testFacts
	at      *sitter.Node      // the call that declares the test
	vars    map[string]string // local variable -> class target, for variables built with new
	locals  map[string]bool
	walking map[string]bool
	// loaded and modules are the names a test takes from a module it loads with import() or
	// require(): a name -> its target, and a module object -> its file.
	loaded  map[string]string
	modules map[string]string
	// used are the outer names whose setup the test has walked, and done the setup nodes walked,
	// so that a value set up once is counted once however often the test uses it.
	used map[string]bool
	done map[uintptr]bool
	// setup is above zero while a setup is walked: functions an object of it holds run only when
	// called, and are followed as helpers then.
	setup int
	// fake is true after the test turned on fake timers: Date and setTimeout then read the fake clock.
	fake bool
}

func (w *tsWalk) walk(body *sitter.Node) {
	w.t.body(body, func(n *sitter.Node) bool { return tsLoops[n.Kind()] }, w.visit)
}

func (w *tsWalk) visit(n *sitter.Node) bool {
	if w.walked(n) {
		return false
	}
	switch n.Kind() {
	case "pair", "method_definition":
		return w.setup == 0 || !tsHeldFunction(n)
	case "variable_declarator":
		w.declare(n)
	case "assignment_expression":
		w.tf.bindLoad(n.ChildByFieldName("left"), n.ChildByFieldName("right"), w.modules, w.loaded)
	case "new_expression":
		w.construct(n)
	case "call_expression":
		w.call(n)
	case "jsx_opening_element", "jsx_self_closing_element":
		w.element(n)
	case "identifier", "shorthand_property_identifier": // a name in a type is a type_identifier, never an identifier
		w.name(n)
	}
	return true
}

// walked reports whether n sets up an outer variable and the test has walked it already.
func (w *tsWalk) walked(n *sitter.Node) bool {
	if !w.tf.setupNodes[n.Id()] {
		return false
	}
	if w.done[n.Id()] {
		return true
	}
	w.done[n.Id()] = true
	return false
}

// declare records a local of the test, which shadows a variable of the same name outside it.
func (w *tsWalk) declare(n *sitter.Node) {
	f := w.tf.f
	name := f.field(n, "name")
	w.locals[name] = true
	delete(w.vars, name)
	delete(w.loaded, name)
	delete(w.modules, name)
	w.bindLoaded(n)
	if v := n.ChildByFieldName("value"); v != nil && v.Kind() == "new_expression" {
		if target, ok := w.tf.names[f.field(v, "constructor")]; ok {
			w.vars[name] = target
		}
	}
}

func (w *tsWalk) construct(n *sitter.Node) {
	f := w.tf.f
	if f.text(n) == "new Date()" {
		if !w.fake {
			w.t.hazard(facts.HazardClock, n)
		}
	} else if target, ok := w.tf.names[f.field(n, "constructor")]; ok {
		w.t.call(target, nil)
	} else if w.tf.unresolved[f.field(n, "constructor")] || w.tf.via[f.field(n, "constructor")] != [2]string{} {
		w.t.test.Unresolved++ // a class hadome cannot find, or one of a support file
	}
}

// name records a read of a name of the repository, or runs the setup of an outer variable.
func (w *tsWalk) name(n *sitter.Node) {
	text := w.tf.f.text(n)
	_, imported := w.tf.names[text]
	_, space := w.tf.spaces[text]
	if imported || space {
		w.t.readName(n, tsUses)
	} else {
		w.useOuter(text)
	}
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
	if w.tool(n, fn, callee, args) {
		return
	}
	if w.alwaysTrue(fn, args) {
		w.t.hazard(facts.HazardAlwaysTrue, n)
		return
	}
	switch fn.Kind() {
	case "identifier":
		if target, ok := w.loaded[callee]; ok {
			w.t.call(target, scopeArg(w.tf.r.scopes, target, args))
		} else if target, ok := w.tf.names[callee]; ok {
			w.t.call(target, scopeArg(w.tf.r.scopes, target, args))
		} else if v, ok := w.tf.via[callee]; ok {
			w.followSupport(v[0], v[1])
		} else if w.tf.unresolved[callee] {
			w.t.test.Unresolved++
		} else {
			w.follow(callee)
		}
	case "member_expression":
		if target := w.memberTarget(f.field(fn, "object"), f.field(fn, "property")); target != "" {
			w.t.call(target, scopeArg(w.tf.r.scopes, target, args))
		}
	}
}

// tool records a call of the testing tools: an assertion, an HTTP test client, a skip, fake
// timers, loading a module for what it does, or a hazard. It reports whether the call is done with.
func (w *tsWalk) tool(n, fn *sitter.Node, callee string, args []*sitter.Node) bool {
	if (callee != "expect" && callee != "expect.soft") || w.matched(n) {
		w.t.assertion(callee, w.tf.t.asserts)
	}
	if w.tf.drivers[callee] || w.appRequest(fn, args) {
		w.t.test.Drives = true
	}
	if w.skips(n, callee, args) || w.timers(callee) {
		return true
	}
	if unit := w.tf.dynamicImport(n); unit != "" && tsDiscarded(n) {
		w.t.call(w.tf.unitOf(unit), nil) // await import('./setup'): the module runs for what it does when loaded
		return true
	}
	cat, ok := w.tf.t.calls[callee]
	if ok && (!w.fake || (cat != facts.HazardClock && cat != facts.HazardSleep)) {
		w.t.hazard(cat, n)
	}
	return ok
}

// memberTarget names what obj.prop() calls: a function of a module or a method of a class. Else
// it follows a function an object of the file holds, and a method of a local value of unknown type
// counts as unresolved.
func (w *tsWalk) memberTarget(obj, prop string) string {
	obj = strings.TrimSuffix(obj, "!") // store!.add(): the assertion that store is set
	for _, files := range []map[string]string{w.tf.spaces, w.modules} {
		if p, ok := files[obj]; ok {
			if sf := w.tf.r.files[p]; sf != nil && sf.support {
				w.followSupport(w.tf.r.declaring(p, prop))
				return ""
			}
			return w.tf.r.target(p, prop)
		}
	}
	for _, owners := range []map[string]string{w.vars, w.tf.names} {
		if owner, ok := owners[obj]; ok {
			return owner + "." + prop
		}
	}
	if w.follow(obj + "." + prop) { // a function an object of the file holds
		return ""
	}
	if w.locals[obj] || w.tf.unresolved[obj] {
		w.t.test.Unresolved++
	}
	return ""
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

// element records a JSX element of a component of the repository as a call: rendering <Page /> runs
// Page. Lowercase names are HTML elements.
func (w *tsWalk) element(n *sitter.Node) {
	f := w.tf.f
	name := n.ChildByFieldName("name")
	if name == nil {
		return
	}
	target := ""
	switch name.Kind() {
	case "identifier":
		target = w.tf.names[f.text(name)]
		if v, ok := w.tf.via[f.text(name)]; ok {
			w.followSupport(v[0], v[1])
		} else if w.tf.unresolved[f.text(name)] {
			w.t.test.Unresolved++
		} else if target == "" {
			w.follow(f.text(name)) // a component the file declares: function Harness() { return <Form /> }
		}
	case "member_expression":
		target = w.memberTarget(f.field(name, "object"), f.field(name, "property"))
	}
	if target != "" {
		w.t.call(target, nil)
	}
}

// follow walks a helper of the file the test calls, once per chain of calls, and reports whether
// there was one.
func (w *tsWalk) follow(name string) bool {
	body, ok := w.tf.helpers[name]
	if !ok {
		return false
	}
	if key := w.tf.f.path + "#" + name; !w.walking[key] {
		w.walking[key] = true
		locals := maps.Clone(w.locals)
		w.params(w.tf.params[name])
		w.defaults(w.tf.params[name])
		w.walk(body)
		w.locals = locals
		delete(w.walking, key)
	}
	return true
}
