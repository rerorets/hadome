//go:build cgo

package treesitter

import (
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

// What a TypeScript test file sets up outside its tests: helpers, modules loaded with import(),
// variables of describe blocks, and HTTP test clients.

// dynamicImport returns the file a call loads when it is import() or require() of a file of the
// repository, named by a string.
func (tf *tsFile) dynamicImport(call *sitter.Node) string {
	if call == nil || call.Kind() != "call_expression" {
		return ""
	}
	if fn := tf.f.field(call, "function"); fn != "import" && fn != "require" {
		return ""
	}
	args := arguments(call.ChildByFieldName("arguments"))
	if len(args) != 1 || args[0].Kind() != "string" {
		return ""
	}
	if p, kind := tf.r.project.resolve(tf.f.path, unquote(tf.f.text(args[0]))); kind == tsFound {
		return p
	}
	return ""
}

// bindLoaders finds the functions of the file, at any depth: declarations and functions held by a
// const (const renderPage = () => render(<Page />)), which test files often declare inside a
// describe block. Each is a helper a test may call; the first of a name wins. A function whose body
// loads exactly one module of the repository with import() is also a loader of that module: a helper
// that resets modules and loads a fresh copy is common.
func (tf *tsFile) bindLoaders() {
	f := tf.f
	walk(f.root, func(n *sitter.Node) bool {
		name, body, params := "", (*sitter.Node)(nil), (*sitter.Node)(nil)
		switch n.Kind() {
		case "function_declaration":
			name, body, params = f.field(n, "name"), n.ChildByFieldName("body"), tsParams(n)
		case "variable_declarator":
			if v := n.ChildByFieldName("value"); v != nil && (v.Kind() == "arrow_function" || v.Kind() == "function_expression") {
				name, body, params = f.field(n, "name"), v.ChildByFieldName("body"), tsParams(v)
			} else if v != nil && v.Kind() == "object" {
				tf.objectHelpers(f.field(n, "name"), v)
			}
		}
		if name == "" || body == nil {
			return true
		}
		if _, seen := tf.helpers[name]; !seen {
			tf.helpers[name], tf.params[name] = body, params
		}
		tf.loaderOf(name, body)
		return true
	})
}

// loaderOf records a function as a loader of the one module of the repository its body loads with
// import(), if it loads exactly one.
func (tf *tsFile) loaderOf(name string, body *sitter.Node) {
	units := map[string]bool{}
	walk(body, func(x *sitter.Node) bool {
		if u := tf.dynamicImport(x); u != "" {
			units[u] = true
		}
		return true
	})
	if len(units) == 1 {
		for u := range units {
			tf.loaders[name] = u
		}
	}
}

// bindLoaded records the names a declaration takes from a module loaded with import() or require(),
// directly or through a loader: const { send } = await import('./mail'), or const mail = await fresh().
func (w *tsWalk) bindLoaded(n *sitter.Node) {
	w.tf.bindLoad(n.ChildByFieldName("name"), n.ChildByFieldName("value"), w.modules, w.loaded)
}

// loadedUnit is the file v loads with import() or require(), directly or through a loader, seen through await,
// parentheses and type assertions: (await import('./mail')) as Mail.
func (tf *tsFile) loadedUnit(v *sitter.Node) string {
	v = tsUnwrap(v)
	unit := tf.dynamicImport(v)
	if unit == "" && v != nil && v.Kind() == "call_expression" {
		unit = tf.loaders[tf.f.field(v, "function")]
	}
	return unit
}

// bindLoad records what pattern takes from value when value loads a module of the repository:
// the module object, or the names an object pattern takes from it. It reports whether it did.
func (tf *tsFile) bindLoad(pattern, value *sitter.Node, modules, loaded map[string]string) bool {
	f := tf.f
	unit := tf.loadedUnit(value)
	if unit == "" || pattern == nil {
		return false
	}
	switch pattern.Kind() {
	case "identifier":
		modules[f.text(pattern)] = unit
	case "object_pattern":
		for _, p := range named(pattern) {
			switch p.Kind() {
			case "shorthand_property_identifier_pattern":
				loaded[f.text(p)] = tf.r.target(unit, f.text(p))
			case "pair_pattern":
				loaded[f.field(p, "value")] = tf.r.target(unit, f.field(p, "key"))
			}
		}
	}
	return true
}

// tsUnwrap is the expression under await, parentheses, and type and non-null assertions.
func tsUnwrap(v *sitter.Node) *sitter.Node {
	for v != nil {
		switch v.Kind() {
		case "await_expression", "parenthesized_expression", "as_expression", "satisfies_expression", "non_null_expression":
			v = v.NamedChild(0)
		default:
			return v
		}
	}
	return v
}

// bindDriver records the names an import of an HTTP test client binds: import request from
// 'supertest', or import * as request from 'supertest'.
func (tf *tsFile) bindDriver(n *sitter.Node) {
	if tf.drivers == nil {
		tf.drivers = map[string]bool{}
	}
	walk(n, func(x *sitter.Node) bool {
		switch x.Kind() {
		case "import_clause":
			if first := x.NamedChild(0); first != nil && first.Kind() == "identifier" {
				tf.drivers[tf.f.text(first)] = true
			}
		case "namespace_import":
			for _, id := range named(x) {
				tf.drivers[tf.f.text(id)] = true
			}
		}
		return true
	})
}

// tsPlaywright is the package of Playwright's test runner, and tsBrowserFixtures the fixtures it
// hands a test that drive the application from outside: a browser page, a browser context, and an
// HTTP client for the application's API.
const tsPlaywright = "@playwright/test"

var tsBrowserFixtures = set("page", "context", "request")

// playwright reports whether an import of spec takes Playwright's test runner: the package itself,
// or a test support file that imports it, such as fixtures.ts holding base.extend(...). The support
// file's own imports are read, not followed further.
func (tf *tsFile) playwright(spec string) bool {
	if spec == tsPlaywright {
		return true
	}
	p, kind := tf.r.project.resolve(tf.f.path, spec)
	if kind == tsExternal {
		return false
	}
	sf := tf.r.files[p]
	if sf == nil || !sf.support {
		return false
	}
	for _, top := range named(sf.root) {
		if top.Kind() == "import_statement" && unquote(sf.field(top, "source")) == tsPlaywright {
			return true
		}
	}
	return false
}

// receivesBrowser reports whether a test function takes one of Playwright's fixtures that drive
// the application from outside: async ({ page }) => ….
func (tf *tsFile) receivesBrowser(fn *sitter.Node) bool {
	params := named(tsParams(fn))
	if !tf.browser || len(params) == 0 {
		return false
	}
	found := false
	walk(params[0], func(x *sitter.Node) bool {
		switch x.Kind() {
		case "shorthand_property_identifier_pattern":
			found = found || tsBrowserFixtures[tf.f.text(x)]
		case "pair_pattern":
			found = found || tsBrowserFixtures[tf.f.field(x, "key")]
			return false
		}
		return true
	})
	return found
}

// objectHelpers records the functions an object of the file holds, as helpers named obj.key:
// const helpers = { twice: (n) => double(n), build() { return new Store() } }.
func (tf *tsFile) objectHelpers(obj string, o *sitter.Node) {
	f := tf.f
	for _, m := range named(o) {
		key, body := "", (*sitter.Node)(nil)
		switch m.Kind() {
		case "pair":
			if v := m.ChildByFieldName("value"); v != nil && (v.Kind() == "arrow_function" || v.Kind() == "function_expression") {
				key, body = f.field(m, "key"), v.ChildByFieldName("body")
			}
		case "method_definition":
			key, body = f.field(m, "name"), m.ChildByFieldName("body")
		}
		if _, seen := tf.helpers[obj+"."+key]; key != "" && body != nil && !seen {
			tf.helpers[obj+"."+key] = body
		}
	}
}

// bindOuter types the variables declared outside the tests: let store: Store, let store = new
// Store(), or store = new Store() in a beforeEach. A test that calls store.add() calls Store.add.
func (tf *tsFile) bindOuter() {
	f := tf.f
	note := func(name, class string) {
		if name == "" || class == "" {
			return
		}
		if had, ok := tf.outer[name]; ok && had != class {
			class = ""
		}
		tf.outer[name] = class
	}
	walk(f.root, func(n *sitter.Node) bool {
		switch n.Kind() {
		case "variable_declarator":
			name := f.field(n, "name")
			if v := n.ChildByFieldName("value"); v != nil && v.Kind() == "new_expression" {
				note(name, tf.names[f.field(v, "constructor")])
			} else if typ := n.ChildByFieldName("type"); typ != nil {
				note(name, tf.names[tsTypeName(f.text(typ))])
			}
		case "assignment_expression":
			if r := n.ChildByFieldName("right"); r != nil && r.Kind() == "new_expression" {
				note(f.field(n, "left"), tf.names[f.field(r, "constructor")])
			}
		}
		return true
	})
}

// outerVars is a test's own copy of the typed outer variables.
func (tf *tsFile) outerVars() map[string]string {
	out := map[string]string{}
	for name, class := range tf.outer {
		if class != "" {
			out[name] = class
		}
	}
	return out
}

// tsTypeName is the class a type annotation names: ": Store | undefined" -> Store.
func tsTypeName(t string) string {
	t = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(t), ":"))
	if i := strings.IndexAny(t, "|<[&"); i >= 0 {
		t = t[:i]
	}
	return strings.TrimSpace(t)
}
