package golang

import (
	"go/ast"
	"go/token"
	"path"
	"slices"

	"github.com/rerorets/hadome/internal/facts"
)

// testMain is a package's func TestMain(m *testing.M). It runs before every test of its directory:
// the internal and the external test package build into one test binary.
type testMain struct {
	fset    *token.FileSet
	pf      parsedFile
	decl    *ast.FuncDecl
	helpers map[string]helper
}

// findTestMains finds the TestMain of each directory, by directory.
func findTestMains(fset *token.FileSet, files []parsedFile, helpers map[string]map[string]helper) map[string]*testMain {
	out := map[string]*testMain{}
	for _, pf := range files {
		if !pf.test {
			continue
		}
		imports := fileImports(pf.file)
		for _, decl := range pf.file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if ok && fn.Recv == nil && fn.Body != nil && fn.Name.Name == "TestMain" && takesM(fn.Type, imports) {
				out[path.Dir(pf.path)] = &testMain{fset: fset, pf: pf, decl: fn, helpers: helpers[helperKey(pf)]}
			}
		}
	}
	return out
}

func takesM(ft *ast.FuncType, imports map[string]string) bool {
	if ft.Params == nil || len(ft.Params.List) != 1 {
		return false
	}
	star, ok := ft.Params.List[0].Type.(*ast.StarExpr)
	return ok && isTesting(star.X, "M", imports)
}

// give walks TestMain for one test, adding what it calls to the test's calls and its clock and
// sleep to the test's hazards. Its checks, skips and unresolved calls are not the test's: what a
// test checks is decided by its own body. A nil main gives nothing.
func (main *testMain) give(w *walker, idx *index, tb *tables) {
	if main == nil {
		return
	}
	mw := newWalker(main.fset, main.pf, idx, tb, main.helpers)
	mw.calls = w.calls
	mw.bindParams(main.decl.Type, nil)
	ast.Inspect(main.decl.Body, mw.visit)
	for _, h := range mw.test.Hazards {
		if (h.Category == facts.HazardClock || h.Category == facts.HazardSleep) && !slices.Contains(w.test.Hazards, h) {
			w.test.Hazards = append(w.test.Hazards, h)
		}
	}
}
