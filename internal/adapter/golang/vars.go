package golang

import (
	"go/ast"
	"go/token"
)

// packageVars types the package-level variables of the test's file: var shared = store.New(), or
// var shared *store.Store set elsewhere (TestMain, init).
func (w *walker) packageVars() {
	for _, d := range w.file.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.VAR {
			continue
		}
		for _, spec := range g.Specs {
			vs := spec.(*ast.ValueSpec)
			for i, name := range vs.Names {
				t := ""
				if i < len(vs.Values) {
					t = w.typeOf(vs.Values[i])
				} else if vs.Type != nil {
					t = w.declaredType(vs.Type)
				}
				if t != "" {
					w.vars[name.Name] = t
				}
			}
		}
	}
}

// declaredType is the repository type a declaration names: *store.Store or store.Store.
func (w *walker) declaredType(e ast.Expr) string {
	if s, ok := e.(*ast.StarExpr); ok {
		e = s.X
	}
	return w.qualify(e, func(id string) bool { return w.idx.types[id] })
}
