package golang

import (
	"go/ast"
	"strings"
)

// testify suites: their tests are methods that suite.Run calls.

const suitePkg = "github.com/stretchr/testify/suite"

// suiteOwn are the methods of testify's suite.Suite that neither check nor reach the code.
var suiteOwn = map[string]bool{"T": true, "SetT": true, "SetS": true, "Run": true, "Require": true, "Assert": true}

// suite walks the setup and test methods of the suite suite.Run runs: new(StoreSuite) or
// &StoreSuite{}. A test that runs a suite tests what its methods test.
func (w *walker) suite(arg ast.Expr) {
	name := ""
	switch a := ast.Unparen(arg).(type) {
	case *ast.CallExpr:
		if id, ok := a.Fun.(*ast.Ident); ok && id.Name == "new" && len(a.Args) == 1 {
			if t, ok := a.Args[0].(*ast.Ident); ok {
				name = t.Name
			}
		}
	case *ast.UnaryExpr:
		if lit, ok := a.X.(*ast.CompositeLit); ok {
			if t, ok := lit.Type.(*ast.Ident); ok {
				name = t.Name
			}
		}
	}
	if name == "" || w.suiteType != "" {
		return
	}
	for _, m := range w.suiteMethods(name) {
		if !strings.HasPrefix(m.Name.Name, "Test") && !strings.HasPrefix(m.Name.Name, "Setup") && m.Name.Name != "BeforeTest" {
			continue
		}
		w.walkMethod(name, m)
	}
}

// walkMethod walks one method of a suite, with the receiver typed as the suite, so that s.store.Add()
// reads the suite's declared field.
func (w *walker) walkMethod(suite string, m *ast.FuncDecl) {
	recv := ""
	if names := m.Recv.List[0].Names; len(names) == 1 {
		recv = names[0].Name
	}
	w.recv, w.suiteType = recv, suite
	if recv != "" {
		w.vars[recv] = w.resolve(ast.NewIdent(suite))
	}
	ast.Inspect(m.Body, w.visit)
	w.recv, w.suiteType = "", ""
}

// suiteMethods lists the methods of a type declared in the test's file.
func (w *walker) suiteMethods(name string) []*ast.FuncDecl {
	var out []*ast.FuncDecl
	for _, d := range w.file.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 || fn.Body == nil {
			continue
		}
		t := fn.Recv.List[0].Type
		if s, ok := t.(*ast.StarExpr); ok {
			t = s.X
		}
		if id, ok := t.(*ast.Ident); ok && id.Name == name {
			out = append(out, fn)
		}
	}
	return out
}

// suiteCall reads a call on a suite method's receiver: s.Equal(…) or s.Require().Equal(…) from the embedded assertions, or s.helper() of the suite.
func (w *walker) suiteCall(c *ast.CallExpr, sel *ast.SelectorExpr) bool {
	if w.recv == "" {
		return false
	}
	switch x := sel.X.(type) {
	case *ast.CallExpr: // s.Require().Equal()
		if w.onReceiver(x.Fun, "Require", "Assert") {
			w.test.Asserts++
			return true
		}
	case *ast.Ident: // s.Equal() or s.helper()
		if x.Name == w.recv {
			w.receiverCall(sel.Sel.Name)
			return true
		}
	}
	return false
}

// onReceiver reports whether e is s.<one of names> on the suite's receiver.
func (w *walker) onReceiver(e ast.Expr, names ...string) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok || id.Name != w.recv {
		return false
	}
	for _, n := range names {
		if sel.Sel.Name == n {
			return true
		}
	}
	return false
}

// receiverCall follows a method of the suite, or counts an embedded assertion such as s.Equal.
func (w *walker) receiverCall(name string) {
	for _, m := range w.suiteMethods(w.suiteType) {
		if m.Name.Name != name {
			continue
		}
		if !w.walking[name] {
			w.walking[name] = true
			ast.Inspect(m.Body, w.visit)
			delete(w.walking, name)
		}
		return
	}
	if !suiteOwn[name] {
		w.test.Asserts++
	}
}
