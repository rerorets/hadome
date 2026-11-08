package golang

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

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
	cx, nested := complexity(fset, d.Body)
	return facts.Symbol{
		ID: id, Kind: kind, Path: pf.path, Line: fset.Position(d.Pos()).Line, EndLine: fset.Position(d.End()).Line,
		Exported: exported(d), Doc: d.Doc != nil, DocElsewhere: d.Recv != nil && standardMethod(d),
		Scoped: scopeWordParam(d.Type) >= 0, Params: len(params(d.Type)), Complexity: cx, Nested: nested,
	}
}

// standardMethods are the methods standard library interfaces define (fmt.Stringer, error,
// http.Handler, json.Marshaler, sort.Interface, …), by name and signature. Their interface
// documents them, so they need no doc comment of their own.
var standardMethods = map[string][]string{
	"Error":         {"() string"},
	"String":        {"() string"},
	"ServeHTTP":     {"(http.ResponseWriter, *http.Request)"},
	"Unwrap":        {"() error", "() []error"},
	"Is":            {"(error) bool"},
	"As":            {"(any) bool", "(interface{}) bool"},
	"Format":        {"(fmt.State, rune)"},
	"MarshalJSON":   {"() ([]byte, error)"},
	"UnmarshalJSON": {"([]byte) error"},
	"MarshalText":   {"() ([]byte, error)"},
	"UnmarshalText": {"([]byte) error"},
	"Len":           {"() int"},
	"Less":          {"(int, int) bool"},
	"Swap":          {"(int, int)"},
}

// standardMethod reports whether a method has the name and the signature of a standard method.
func standardMethod(d *ast.FuncDecl) bool {
	sig := signature(d.Type)
	for _, want := range standardMethods[d.Name.Name] {
		if sig == want {
			return true
		}
	}
	return false
}

// signature writes a function type's parameter and result types without names: "(int, int) bool".
func signature(ft *ast.FuncType) string {
	list := func(fl *ast.FieldList) []string {
		var out []string
		if fl == nil {
			return out
		}
		for _, f := range fl.List {
			n := max(len(f.Names), 1)
			for range n {
				out = append(out, types.ExprString(f.Type))
			}
		}
		return out
	}
	s := "(" + strings.Join(list(ft.Params), ", ") + ")"
	switch rs := list(ft.Results); len(rs) {
	case 0:
	case 1:
		s += " " + rs[0]
	default:
		s += " (" + strings.Join(rs, ", ") + ")"
	}
	return s
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
// && and ||. Function literals inside are counted on their own and returned as nested, with the
// literals nested in them.
func complexity(fset *token.FileSet, body *ast.BlockStmt) (int, []facts.Nested) {
	n := 1
	var nested []facts.Nested
	if body == nil {
		return n, nil
	}
	ast.Inspect(body, func(node ast.Node) bool {
		switch x := node.(type) {
		case *ast.FuncLit:
			c, inner := complexity(fset, x.Body)
			nested = append(append(nested, facts.Nested{Line: fset.Position(x.Pos()).Line, Complexity: c}), inner...)
			return false
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
	return n, nested
}
