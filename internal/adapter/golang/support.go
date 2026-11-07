package golang

import (
	"go/ast"
	"path"
	"strings"

	"github.com/rerorets/hadome/internal/adapter/walk"
	"github.com/rerorets/hadome/internal/config"
)

// markSupport marks the files of test support packages that are not tests themselves: packages
// under a test helper directory (testutil/), and packages named <name>test that import a package
// called <name> or sit in a directory of that name, as httptest and fstest do. A file matching
// files.product, or generated, stays what it was.
func markSupport(files []parsedFile, product []string) {
	dirs := map[string]bool{}
	for _, pf := range files {
		if !pf.test && supportPackage(pf) {
			dirs[path.Dir(pf.path)] = true
		}
	}
	for i := range files {
		pf := &files[i]
		pf.support = dirs[path.Dir(pf.path)] && !pf.test && !pf.generated && !config.MatchesAny(product, pf.path)
	}
}

// supportPackage reports whether a file's package exists for the tests. A name that merely ends in
// test (latest) imports no package called like the rest of it, and is product code.
func supportPackage(pf parsedFile) bool {
	if walk.HelperDir(pf.path) {
		return true
	}
	prefix, ok := strings.CutSuffix(pf.file.Name.Name, "test")
	if !ok || prefix == "" {
		return false
	}
	if path.Base(path.Dir(path.Dir(pf.path))) == prefix {
		return true
	}
	for name, p := range fileImports(pf.file) {
		if name == prefix || path.Base(p) == prefix {
			return true
		}
	}
	return false
}

// addSupport records the functions of a test support file, which tests are followed into.
func (idx *index) addSupport(pf parsedFile) {
	funcs := idx.support[pf.pkgPath]
	if funcs == nil {
		funcs = map[string]helper{}
		idx.support[pf.pkgPath] = funcs
	}
	imports := fileImports(pf.file)
	for _, decl := range pf.file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Body != nil {
			funcs[fn.Name.Name] = helper{decl: fn, path: pf.path, imports: imports, pkgPath: pf.pkgPath}
		}
	}
}

// supportFunc returns the function of a test support package that pkg.Name names, if it is one.
func (s scope) supportFunc(e ast.Expr) (helper, bool) {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok || !s.isPackage(sel.X) {
		return helper{}, false
	}
	h, ok := s.idx.support[s.imports[sel.X.(*ast.Ident).Name]][sel.Sel.Name]
	return h, ok
}

// followSupport walks a function of a test support package at each call, as if it were part of the
// test. Its names resolve in its own package: its file's imports, and the package's other
// functions as helpers.
func (w *walker) followSupport(id string, h helper, c *ast.CallExpr) {
	if w.walking[id] {
		return
	}
	w.walking[id] = true
	prevScope, prevPath, prevArgs := w.scope, w.curPath, w.args
	args := w.bindArgs(h.decl.Type, c)
	w.scope = scope{idx: w.idx, pkgPath: h.pkgPath, imports: h.imports, samePkg: true,
		frame: &frame{vars: map[string]*vtype{}, types: map[string]ast.Expr{}, helpers: w.idx.support[h.pkgPath]}}
	w.curPath, w.args = h.path, args
	w.collectTParams(h.decl.Type, h.decl.Body)
	ast.Inspect(h.decl.Body, w.visit)
	w.scope, w.curPath, w.args = prevScope, prevPath, prevArgs
	delete(w.walking, id)
}
