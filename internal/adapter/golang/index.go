package golang

import (
	"github.com/rerorets/hadome/internal/facts"
	"go/ast"
	"go/token"
	"go/types"
	"strings"
)

// index knows the public API of the repository's own packages, so that calls from tests can be
// turned into target IDs ("<import path>.<Func>" or "<import path>.<Type>.<Method>").
type index struct {
	modules []string
	// funcs maps "<import path>.<Func>" to the type it returns, "<import path>.<Type>", or "" when
	// the result is not a type of the same package. Unexported functions are included because tests
	// in the same package call them.
	funcs map[string]string
	// types holds "<import path>.<Type>" for every exported type.
	types map[string]bool
	// values holds "<import path>.<Name>" for every package-level variable. Calling one runs code
	// hadome cannot name, so the call counts as unresolved rather than as no call at all.
	values map[string]bool
	// consts holds "<import path>.<Name>" for every package-level constant.
	consts map[string]bool
	// scopes maps a function or method ID to the position of its scope parameter among the call's
	// arguments, or -1 when it takes none (docs/design/rules-tests.md defines the scope).
	scopes map[string]int
}

func buildIndex(files []parsedFile, modules map[string]string) *index {
	idx := &index{funcs: map[string]string{}, types: map[string]bool{}, values: map[string]bool{}, consts: map[string]bool{}, scopes: map[string]int{}}
	for _, m := range modules {
		idx.modules = append(idx.modules, m)
	}
	for _, pf := range files {
		if pf.test {
			continue
		}
		for _, decl := range pf.file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if id := funcID(pf.pkgPath, d); id != "" {
					idx.scopes[id] = scopeParam(d.Type)
				}
				if d.Recv == nil {
					idx.funcs[pf.pkgPath+"."+d.Name.Name] = resultType(pf.pkgPath, d.Type)
				}
			case *ast.GenDecl:
				for _, s := range d.Specs {
					switch s := s.(type) {
					case *ast.TypeSpec:
						if s.Name.IsExported() {
							idx.types[pf.pkgPath+"."+s.Name.Name] = true
						}
					case *ast.ValueSpec:
						for _, n := range s.Names {
							if d.Tok == token.VAR {
								idx.values[pf.pkgPath+"."+n.Name] = true
							} else {
								idx.consts[pf.pkgPath+"."+n.Name] = true
							}
						}
					}
				}
			}
		}
	}
	return idx
}

// value reports whether id names something a test can use as a value: a function, a variable or
// a constant. Types are not values.
func (idx *index) value(id string) bool {
	_, fn := idx.funcs[id]
	return fn || idx.values[id] || idx.consts[id]
}

// internal reports whether an import path belongs to the repository.
func (idx *index) internal(importPath string) bool {
	for _, m := range idx.modules {
		if importPath == m || strings.HasPrefix(importPath, m+"/") {
			return true
		}
	}
	return false
}

// resultType returns "<pkg>.<Type>" when the first result is T or *T of the same package.
func resultType(pkgPath string, ft *ast.FuncType) string {
	if ft.Results == nil || len(ft.Results.List) == 0 {
		return ""
	}
	t := ft.Results.List[0].Type
	if star, ok := t.(*ast.StarExpr); ok {
		t = star.X
	}
	if id, ok := t.(*ast.Ident); ok && id.IsExported() {
		return pkgPath + "." + id.Name
	}
	return ""
}

// funcID returns "<pkg>.<Func>" or "<pkg>.<Type>.<Method>", or "" for a receiver it cannot name.
func funcID(pkgPath string, d *ast.FuncDecl) string {
	if d.Recv == nil {
		return pkgPath + "." + d.Name.Name
	}
	if recv := receiverType(d.Recv); recv != "" {
		return pkgPath + "." + recv + "." + d.Name.Name
	}
	return ""
}

// exported reports whether other units can call the function: its name is exported, and so is
// its receiver's type.
func exported(d *ast.FuncDecl) bool {
	if !d.Name.IsExported() {
		return false
	}
	return d.Recv == nil || ast.IsExported(receiverType(d.Recv))
}

type param struct {
	name string // lower case; empty when unnamed
	ctx  bool
}

func params(ft *ast.FuncType) []param {
	var out []param
	for _, f := range ft.Params.List {
		ctx := types.ExprString(f.Type) == "context.Context"
		if len(f.Names) == 0 {
			out = append(out, param{ctx: ctx})
		}
		for _, n := range f.Names {
			out = append(out, param{name: strings.ToLower(n.Name), ctx: ctx})
		}
	}
	return out
}

// scopeWordParam returns the position of the first parameter whose name contains a scope word, or -1.
func scopeWordParam(ft *ast.FuncType) int {
	for i, p := range params(ft) {
		if facts.IsScopeName(p.name) {
			return i
		}
	}
	return -1
}

// scopeParam returns the position of the scope parameter: the first whose name contains a scope
// word, or else the first that is not a context.Context. It is -1 when there is none.
func scopeParam(ft *ast.FuncType) int {
	if i := scopeWordParam(ft); i >= 0 {
		return i
	}
	for i, p := range params(ft) {
		if !p.ctx {
			return i
		}
	}
	return -1
}

func receiverType(recv *ast.FieldList) string {
	if len(recv.List) == 0 {
		return ""
	}
	t := recv.List[0].Type
	if star, ok := t.(*ast.StarExpr); ok {
		t = star.X
	}
	if idx, ok := t.(*ast.IndexExpr); ok { // generic receiver T[P]
		t = idx.X
	}
	if id, ok := t.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}
