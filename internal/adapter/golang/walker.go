package golang

import (
	"go/ast"
	"go/token"
	"strings"
)

// reading is what reading any function of the version needs: positions, the index of the
// repository's declarations, and the hazard and io tables.
type reading struct {
	fset *token.FileSet
	idx  *index
	tb   *tables
}

// newWalker returns a walker for a function declared in the file: a test, a TestMain, or a function
// of the product code.
func newWalker(rd reading, pf parsedFile, helpers map[string]helper) *walker {
	return &walker{
		scope: scope{idx: rd.idx, pkgPath: pf.pkgPath, imports: fileImports(pf.file), samePkg: !strings.HasSuffix(pf.file.Name.Name, "_test"),
			testKey: helperKey(pf), helpers: helpers, local: newBlock(nil)},
		fset: rd.fset, tables: rd.tb, tParams: map[string]bool{}, calls: map[string]*callCount{},
		walking: map[string]bool{}, curPath: pf.path,
		callees: map[ast.Expr]bool{}, fields: map[*ast.Ident]bool{}, file: pf.file,
	}
}
