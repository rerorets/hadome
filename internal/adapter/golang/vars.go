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
			w.recordSpec(spec.(*ast.ValueSpec))
		}
	}
}
