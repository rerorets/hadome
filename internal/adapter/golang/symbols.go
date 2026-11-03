package golang

import (
	"go/ast"
	"go/token"

	"github.com/rerorets/hadome/internal/facts"
)

// symbolsOf lists the functions, methods and named types declared in one non-test file, so that a
// target that disappeared can be told apart from a target that lost its tests, and so that sizes
// and doc comments can be checked.
func symbolsOf(fset *token.FileSet, pf parsedFile) []facts.Symbol {
	var out []facts.Symbol
	for _, decl := range pf.file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if id := funcID(pf.pkgPath, d); id != "" {
				out = append(out, funcSymbol(fset, pf, id, d))
			}
		case *ast.GenDecl:
			out = append(out, namedTypes(fset, pf, d)...)
		}
	}
	return out
}

func funcSymbol(fset *token.FileSet, pf parsedFile, id string, d *ast.FuncDecl) facts.Symbol {
	kind := facts.SymbolFunction
	if d.Recv != nil {
		kind = facts.SymbolMethod
	}
	return facts.Symbol{
		ID: id, Kind: kind, Path: pf.path, Line: fset.Position(d.Pos()).Line, EndLine: fset.Position(d.End()).Line,
		Exported: exported(d), Doc: d.Doc != nil, Scoped: scopeWordParam(d.Type) >= 0,
		Params: len(params(d.Type)), Complexity: complexity(d.Body),
	}
}

// namedTypes lists the named types of a declaration. A type's doc comment is on its spec, or on the
// declaration when that holds only the one type.
func namedTypes(fset *token.FileSet, pf parsedFile, d *ast.GenDecl) []facts.Symbol {
	var out []facts.Symbol
	for _, spec := range d.Specs {
		ts, ok := spec.(*ast.TypeSpec)
		if !ok {
			continue
		}
		s := facts.Symbol{
			ID: pf.pkgPath + "." + ts.Name.Name, Kind: facts.SymbolType, Path: pf.path,
			Line: fset.Position(ts.Pos()).Line, EndLine: fset.Position(ts.End()).Line,
			Exported: ts.Name.IsExported(), Doc: ts.Doc != nil || (d.Doc != nil && len(d.Specs) == 1),
		}
		if it, ok := ts.Type.(*ast.InterfaceType); ok {
			s.Kind, s.Methods = facts.SymbolInterface, methodCount(it)
		}
		out = append(out, s)
	}
	return out
}

func methodCount(it *ast.InterfaceType) int {
	n := 0
	for _, m := range it.Methods.List {
		if _, isFunc := m.Type.(*ast.FuncType); isFunc {
			n += len(m.Names)
		}
	}
	return n
}

// complexity is the cyclomatic complexity of a body: 1, plus one per if, for, case (not default),
// && and ||. Function literals inside count toward the function that holds them.
func complexity(body *ast.BlockStmt) int {
	n := 1
	if body == nil {
		return n
	}
	ast.Inspect(body, func(node ast.Node) bool {
		switch x := node.(type) {
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt:
			n++
		case *ast.CaseClause:
			if x.List != nil {
				n++
			}
		case *ast.CommClause:
			if x.Comm != nil {
				n++
			}
		case *ast.BinaryExpr:
			if x.Op == token.LAND || x.Op == token.LOR {
				n++
			}
		}
		return true
	})
	return n
}
