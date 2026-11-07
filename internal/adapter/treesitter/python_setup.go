//go:build cgo

package treesitter

import (
	"path"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

// What a Python test file sets up outside its tests: pytest fixtures, unittest's setUp, and helpers
// held by a name (helper = lambda n: double(n)).

// bindSetup reads the fixtures and the lambda helpers at the top of the file.
func (pf *pyFile) bindSetup() {
	f := pf.f
	for _, n := range named(f.root) {
		switch def := definition(n); {
		case n.Kind() == "decorated_definition" && def.Kind() == "function_definition" && pyFixture(f, n):
			pf.fixtureDefs[f.field(def, "name")] = def
			if class := pf.built(def.ChildByFieldName("body")); class != "" {
				pf.fixtures[f.field(def, "name")] = class
			}
		case n.Kind() == "expression_statement":
			pf.lambdaHelper(n.NamedChild(0))
		}
	}
}

// pyFixture reports whether a decorated function is a pytest fixture.
func pyFixture(f *file, n *sitter.Node) bool {
	for _, d := range named(n) {
		if d.Kind() == "decorator" && strings.Contains(f.text(d), "fixture") {
			return true
		}
	}
	return false
}

// built returns the class a fixture's body returns or yields when it builds one of the product's:
// return Store(), or yield Store().
func (pf *pyFile) built(body *sitter.Node) string {
	class := ""
	walk(body, func(n *sitter.Node) bool {
		if class != "" || n.Kind() == "function_definition" || n.Kind() == "lambda" {
			return false
		}
		if n.Kind() == "return_statement" || n.Kind() == "yield" {
			if v := value(n); v != nil && v.Kind() == "call" {
				class = pf.names[pf.f.field(v, "function")]
			}
		}
		return true
	})
	return class
}

// lambdaHelper records helper = lambda …: as a helper whose body is the lambda's.
func (pf *pyFile) lambdaHelper(n *sitter.Node) {
	if n == nil || n.Kind() != "assignment" {
		return
	}
	left, right := n.ChildByFieldName("left"), n.ChildByFieldName("right")
	if left == nil || left.Kind() != "identifier" || right == nil || right.Kind() != "lambda" {
		return
	}
	if name := pf.f.text(left); pf.helpers[name] == nil {
		pf.helpers[name] = right.ChildByFieldName("body")
	}
}

// given types what a test receives from its setup: the fixtures it takes as parameters, and what
// its class's setUp or setup_method stores on self (self.store = Store()).
func (pf *pyFile) given(def, class *sitter.Node) map[string]string {
	f := pf.f
	out := map[string]string{}
	for _, p := range pyParams(f, def) {
		if owner, _ := pf.fixtureDef(p); owner != nil && owner.fixtures[p] != "" {
			out[p] = owner.fixtures[p]
		}
	}
	if class == nil {
		return out
	}
	for _, m := range named(definition(class).ChildByFieldName("body")) {
		md := definition(m)
		if md.Kind() != "function_definition" || !pySetups[f.field(md, "name")] {
			continue
		}
		walk(md.ChildByFieldName("body"), func(n *sitter.Node) bool {
			if n.Kind() != "assignment" {
				return true
			}
			left, right := n.ChildByFieldName("left"), n.ChildByFieldName("right")
			if left != nil && left.Kind() == "attribute" && strings.HasPrefix(f.text(left), "self.") &&
				right != nil && right.Kind() == "call" {
				if c := pf.names[f.field(right, "function")]; c != "" {
					out[f.text(left)] = c
				}
			}
			return false
		})
	}
	return out
}

// fixtureDef finds the pytest fixture a name stands for: in the file, or else in the conftest.py of
// its directory and the directories above, nearest first. It returns the file that holds it.
func (pf *pyFile) fixtureDef(name string) (*pyFile, *sitter.Node) {
	if d := pf.fixtureDefs[name]; d != nil {
		return pf, d
	}
	for dir := path.Dir(pf.f.path); ; dir = path.Dir(dir) {
		conftest := path.Join(dir, "conftest")
		if conftest != module(pf.f.path) {
			if cf := pf.r.supportFile(conftest, pf.t); cf != nil && cf.fixtureDefs[name] != nil {
				return cf, cf.fixtureDefs[name]
			}
		}
		if dir == "." {
			return nil, nil
		}
	}
}

// pySetups are the methods of a test class that run before each test or once for the class.
var pySetups = set("setUp", "setup_method", "setUpClass", "setup_class", "asyncSetUp")
