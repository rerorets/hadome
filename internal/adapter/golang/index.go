package golang

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"github.com/rerorets/hadome/internal/facts"
)

// index knows the declarations of the repository's own packages, so that calls from tests can be
// turned into target IDs ("<import path>.<Func>" or "<import path>.<Type>.<Method>").
type index struct {
	modules []string
	// methods holds "<import path>.<Type>.<Method>" for every method.
	methods map[string]bool
	// funcs holds "<import path>.<Func>" for every function. Unexported functions are included
	// because tests in the same package call them.
	funcs map[string]bool
	// results maps a function or method ID to the declarations of its results, in order.
	results map[string][]declRef
	// types holds "<import path>.<Type>" for every type, and typeDecls their declarations.
	// Unexported types are included because tests in the same package call their methods.
	types     map[string]bool
	typeDecls map[string]declRef
	// testTypes maps "<helperKey>.<Type>" to the declaration of a type in a test file. Its methods
	// are not targets, but its fields lead to them.
	testTypes map[string]declRef
	// testVars maps "<helperKey>.<Name>" to the declaration of a package-level variable of a test
	// file, which every test file of the same package can use.
	testVars map[string]declRef
	// values holds "<import path>.<Name>" for every package-level variable. Calling one runs code
	// hadome cannot name, so the call counts as unresolved rather than as no call at all.
	values map[string]bool
	// varDecls maps each package-level variable to its declaration, so that Default.Validate() is a
	// call of a method of Default's type.
	varDecls map[string]declRef
	// consts holds "<import path>.<Name>" for every package-level constant.
	consts map[string]bool
	// support maps the import path of a test support package to its functions by name. They are
	// not targets; a test that calls one is followed into it.
	support map[string]map[string]helper
	// scopes maps a function or method ID to the position of its scope parameter among the call's
	// arguments, or -1 when it takes none (docs/design/rules-tests.md defines the scope).
	scopes map[string]int
	// strs are the package-level constants and variables whose value is a string expression, by
	// "<import path>.<Name>", or "<test package key>.<Name>" for those of test files.
	strs map[string]strDecl
}

func buildIndex(files []parsedFile, modules map[string]string, helpers map[string]map[string]helper) *index {
	idx := &index{funcs: map[string]bool{}, methods: map[string]bool{}, results: map[string][]declRef{}, types: map[string]bool{},
		typeDecls: map[string]declRef{}, testTypes: map[string]declRef{}, testVars: map[string]declRef{},
		values: map[string]bool{}, varDecls: map[string]declRef{}, consts: map[string]bool{},
		scopes: map[string]int{}, support: map[string]map[string]helper{}, strs: map[string]strDecl{}}
	for _, m := range modules {
		idx.modules = append(idx.modules, m)
	}
	for _, pf := range files {
		in := scope{idx: idx, pkgPath: pf.pkgPath, imports: fileImports(pf.file), samePkg: true}
		if pf.test {
			in.samePkg = !strings.HasSuffix(pf.file.Name.Name, "_test")
			in.testKey = helperKey(pf)
			in.helpers = helpers[in.testKey]
			idx.addStrs(pf, in)
			idx.addTestDecls(pf.file, in)
			continue
		}
		idx.addStrs(pf, in)
		if pf.support {
			idx.addSupport(pf)
			continue
		}
		for _, decl := range pf.file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				idx.addFunc(pf.pkgPath, d, in)
			case *ast.GenDecl:
				for _, s := range d.Specs {
					switch s := s.(type) {
					case *ast.TypeSpec:
						id := pf.pkgPath + "." + s.Name.Name
						idx.types[id] = true
						idx.typeDecls[id] = declRef{typ: s.Type, in: in}
					case *ast.ValueSpec:
						idx.addValues(pf.pkgPath, d.Tok, s, in)
					}
				}
			}
		}
	}
	return idx
}

func (idx *index) addFunc(pkgPath string, d *ast.FuncDecl, in scope) {
	id := funcID(pkgPath, d)
	if id == "" {
		return
	}
	idx.scopes[id] = scopeParam(d.Type)
	if refs := resultRefs(d.Type, in); len(refs) > 0 {
		idx.results[id] = refs
	}
	if d.Recv == nil {
		idx.funcs[id] = true
	} else {
		idx.methods[id] = true
	}
}

func (idx *index) addValues(pkgPath string, tok token.Token, s *ast.ValueSpec, in scope) {
	for i, n := range s.Names {
		id := pkgPath + "." + n.Name
		if tok != token.VAR {
			idx.consts[id] = true
			continue
		}
		idx.values[id] = true
		idx.varDecls[id] = specRef(s, i, in)
	}
}

// addTestDecls indexes the package-level types and variables of a test file.
func (idx *index) addTestDecls(f *ast.File, in scope) {
	for _, decl := range f.Decls {
		d, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, s := range d.Specs {
			switch s := s.(type) {
			case *ast.TypeSpec:
				idx.testTypes[in.testKey+"."+s.Name.Name] = declRef{typ: s.Type, in: in}
			case *ast.ValueSpec:
				if d.Tok != token.VAR {
					continue
				}
				for i, n := range s.Names {
					idx.testVars[in.testKey+"."+n.Name] = specRef(s, i, in)
				}
			}
		}
	}
}

// resultRefs are the declarations of a function's results, one for each value it returns:
// (a, b *Store) gives two.
func resultRefs(ft *ast.FuncType, in scope) []declRef {
	if ft.Results == nil {
		return nil
	}
	var out []declRef
	for _, f := range ft.Results.List {
		for range max(1, len(f.Names)) {
			out = append(out, declRef{typ: f.Type, in: in})
		}
	}
	return out
}

// specRef is the declaration of the i-th variable of var a, b T or var a, b = x, y.
func specRef(s *ast.ValueSpec, i int, in scope) declRef {
	ref := declRef{typ: s.Type, in: in}
	switch {
	case len(s.Values) == len(s.Names):
		ref.value = s.Values[i]
	case len(s.Values) == 1: // var s, err = store.Open()
		ref.value, ref.nth = s.Values[0], i
	}
	return ref
}

// result returns the type of a function's or method's n-th result.
func (idx *index) result(id string, n int) *vtype {
	refs := idx.results[id]
	if n >= len(refs) {
		return nil
	}
	return refs[n].in.resolve(refs[n].typ)
}

// varType returns the type of a package-level variable of the code.
func (idx *index) varType(id string, depth int) *vtype { return idx.varDecls[id].typeAt(depth) }

// typeAt returns the type of a declared variable: the one it is declared with, or else the type of
// its initial value.
func (d declRef) typeAt(depth int) *vtype {
	if d.typ != nil {
		return d.in.resolve(d.typ)
	}
	if d.value == nil {
		return nil
	}
	return d.in.nthValueType(d.value, d.nth, depth)
}

// value reports whether id names something a test can use as a value: a function, a variable or
// a constant. Types are not values.
func (idx *index) value(id string) bool {
	return idx.funcs[id] || idx.values[id] || idx.consts[id]
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
