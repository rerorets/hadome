package golang

import (
	"go/ast"
	"go/token"
)

// The adapter types values from declarations only: the type a variable, field, parameter or
// function result is declared with, or the literal or call a variable is made from. It runs no type
// checker and loads no other module, so a value whose type is only inferred across packages it did
// not read stays untyped, and a call on it stays unresolved.

// scope is where an expression is written: its package, its file's imports, the helpers of its
// test package and, inside a function, the names declared around it.
type scope struct {
	idx     *index
	pkgPath string
	imports map[string]string // local name -> import path
	// samePkg is true when unqualified names may refer to the package's non-test code: in the
	// package itself and in its internal test files, but not in an external _test package.
	samePkg bool
	// testKey names the test package (helperKey) whose test-file types and variables unqualified
	// names may refer to; it is empty in non-test code.
	testKey string
	helpers map[string]helper
	// local is the innermost function whose names are in scope. It is nil outside a function.
	local *block
}

// block holds the names one function declares: a test, a helper or a function literal. Lookups
// go outward through parent, so a function literal sees the function around it; a helper's parent
// is nil, because it does not see the variables of the test that calls it.
type block struct {
	// vars maps a variable to its type. A nil type is a variable whose type is not known, which
	// still hides a package or package-level variable of the same name.
	vars map[string]*vtype
	// types holds the types declared inside the function.
	types  map[string]ast.Expr
	parent *block
}

func newBlock(parent *block) *block {
	return &block{vars: map[string]*vtype{}, types: map[string]ast.Expr{}, parent: parent}
}

// lookup finds the variable of the name in the innermost function that declares it.
func (s scope) lookup(name string) (*vtype, bool) {
	for b := s.local; b != nil; b = b.parent {
		if t, ok := b.vars[name]; ok {
			return t, true
		}
	}
	return nil, false
}

// localType finds the type of the name declared inside the functions in scope.
func (s scope) localType(name string) (ast.Expr, bool) {
	for b := s.local; b != nil; b = b.parent {
		if t, ok := b.types[name]; ok {
			return t, true
		}
	}
	return nil, false
}

// vtype is a type as declared: a repository type, or a literal type such as []struct{…} written in a
// test. Its fields and elements are read when asked for, so a recursive type is no problem.
type vtype struct {
	// id is "<import path>.<Type>" of a type of the repository's non-test code. Methods called on a
	// value of the type are targets. It is empty for other types.
	id   string
	expr ast.Expr // the type's declaration, or the literal type itself
	in   scope    // where expr is written
}

// maxDepth bounds the chains of named types and package-level variables followed, so that code
// that does not compile cannot make the adapter loop.
const maxDepth = 16

// declRef is a declaration in the index, with the scope it is written in.
type declRef struct {
	typ   ast.Expr // the declared type, or nil
	value ast.Expr // a variable's initial value, or nil
	in    scope
}

// qualify turns pkg.Name (or Name in the same package) into "<import path>.<Name>" when known.
func (s scope) qualify(e ast.Expr, known func(string) bool) string {
	switch e := e.(type) {
	case *ast.SelectorExpr:
		if pkg, ok := e.X.(*ast.Ident); ok && !s.shadowed(pkg.Name) {
			if p, ok := s.imports[pkg.Name]; ok && known(p+"."+e.Sel.Name) {
				return p + "." + e.Sel.Name
			}
		}
	case *ast.Ident:
		if s.samePkg && known(s.pkgPath+"."+e.Name) {
			return s.pkgPath + "." + e.Name
		}
	}
	return ""
}

// shadowed reports whether a function in scope declares a variable of the name, hiding a package
// of it.
func (s scope) shadowed(name string) bool {
	_, ok := s.lookup(name)
	return ok
}

// resolve returns the type a type expression names, or nil when it is not one the adapter can read
// (a builtin, a type parameter, a type of a package outside the repository). *T is read as T,
// because a method called through a pointer is the type's method.
func (s scope) resolve(e ast.Expr) *vtype {
	switch e := e.(type) {
	case nil:
		return nil
	case *ast.ParenExpr:
		return s.resolve(e.X)
	case *ast.StarExpr:
		return s.resolve(e.X)
	case *ast.IndexExpr: // a generic type instantiated, T[int]
		return s.resolve(e.X)
	case *ast.IndexListExpr:
		return s.resolve(e.X)
	case *ast.Ident, *ast.SelectorExpr:
		if id, ok := e.(*ast.Ident); ok {
			if t, ok := s.localType(id.Name); ok {
				return &vtype{expr: t, in: s}
			}
		}
		if id := s.qualify(e, func(id string) bool { return s.idx.types[id] }); id != "" {
			d := s.idx.typeDecls[id]
			return &vtype{id: id, expr: d.typ, in: d.in}
		}
		if id, ok := e.(*ast.Ident); ok && s.testKey != "" {
			if d, ok := s.idx.testTypes[s.testKey+"."+id.Name]; ok {
				return &vtype{expr: d.typ, in: d.in}
			}
		}
		return nil
	default:
		return &vtype{expr: e, in: s}
	}
}

// valueType returns the type of a value, or nil when its declarations do not say.
func (s scope) valueType(e ast.Expr) *vtype { return s.valueTypeAt(e, 0) }

func (s scope) valueTypeAt(e ast.Expr, depth int) *vtype {
	if depth > maxDepth {
		return nil
	}
	switch e := e.(type) {
	case *ast.ParenExpr:
		return s.valueTypeAt(e.X, depth+1)
	case *ast.StarExpr:
		return s.valueTypeAt(e.X, depth+1)
	case *ast.UnaryExpr:
		switch e.Op {
		case token.AND:
			return s.valueTypeAt(e.X, depth+1)
		case token.ARROW:
			return s.valueTypeAt(e.X, depth+1).elem()
		}
	case *ast.CompositeLit:
		return s.resolve(e.Type)
	case *ast.CallExpr:
		return s.resultType(e, depth)
	case *ast.Ident, *ast.SelectorExpr:
		return s.nameType(e, depth)
	case *ast.IndexExpr:
		return s.valueTypeAt(e.X, depth+1).elem()
	case *ast.SliceExpr:
		return s.valueTypeAt(e.X, depth+1)
	case *ast.TypeAssertExpr:
		return s.resolve(e.Type)
	}
	return nil
}

// nameType returns the type of a variable: one a function in scope declares, a package-level one
// of the test package's files (shared) or of the code (Default, store.Default), or the type of a
// field (tc.store).
func (s scope) nameType(e ast.Expr, depth int) *vtype {
	if id, ok := e.(*ast.Ident); ok {
		if t, ok := s.lookup(id.Name); ok {
			return t
		}
		if d, ok := s.idx.testVars[s.testKey+"."+id.Name]; ok && s.testKey != "" {
			return d.typeAt(depth + 1)
		}
	}
	if id := s.qualify(e, func(id string) bool { _, ok := s.idx.varDecls[id]; return ok }); id != "" {
		return s.idx.varType(id, depth+1)
	}
	sel, ok := e.(*ast.SelectorExpr)
	if !ok || s.isPackage(sel.X) {
		return nil
	}
	return s.valueTypeAt(sel.X, depth+1).field(sel.Sel.Name)
}

// isPackage reports whether e names an imported package.
func (s scope) isPackage(e ast.Expr) bool {
	x, ok := e.(*ast.Ident)
	if !ok || s.shadowed(x.Name) {
		return false
	}
	_, ok = s.imports[x.Name]
	return ok
}

// resultType returns the type of a call's first result: a function or method of the repository, a
// helper of the test files, new(T) or a conversion T(x).
func (s scope) resultType(c *ast.CallExpr, depth int) *vtype {
	fun := ast.Unparen(c.Fun)
	switch f := fun.(type) {
	case *ast.IndexExpr: // a generic function instantiated, New[int]()
		fun = f.X
	case *ast.IndexListExpr:
		fun = f.X
	}
	if t, local := s.localResult(fun, c); local {
		return t
	}
	if h, ok := s.supportFunc(fun); ok {
		return h.resultType(s)
	}
	if id := s.qualify(fun, func(id string) bool { return s.idx.funcs[id] }); id != "" {
		return s.idx.result(id)
	}
	if t := s.qualify(fun, func(id string) bool { return s.idx.types[id] }); t != "" {
		return s.resolve(fun) // a conversion
	}
	switch f := fun.(type) {
	case *ast.ParenExpr: // a conversion to a written type, (*T)(x)
		return s.resolve(f.X)
	case *ast.SelectorExpr:
		if s.isPackage(f.X) {
			return nil
		}
		if t := s.valueTypeAt(f.X, depth+1); t != nil && t.id != "" {
			return s.idx.result(t.id + "." + f.Sel.Name)
		}
	}
	return nil
}

// localResult returns the result type of a call of a name the test knows: a function held in a
// variable, new(T), or a helper of the test files. It reports false for any other call.
func (s scope) localResult(fun ast.Expr, c *ast.CallExpr) (*vtype, bool) {
	id, ok := fun.(*ast.Ident)
	if !ok {
		return nil, false
	}
	if _, local := s.lookup(id.Name); local {
		return nil, true
	}
	if id.Name == "new" && len(c.Args) == 1 {
		return s.resolve(c.Args[0]), true
	}
	if h, ok := s.helpers[id.Name]; ok {
		return h.resultType(s), true
	}
	return nil, false
}

// resultType is the type of the helper's first result, read in the helper's own file.
func (h helper) resultType(caller scope) *vtype {
	r := h.decl.Type.Results
	if r == nil || len(r.List) == 0 {
		return nil
	}
	return h.scope(caller).resolve(r.List[0].Type)
}

// scope is where the helper is written: its own file's imports, and no function around it.
func (h helper) scope(caller scope) scope {
	in := caller
	in.imports, in.local = h.imports, nil
	if h.pkgPath != "" { // a function of a test support package
		in.pkgPath, in.samePkg, in.testKey = h.pkgPath, true, ""
	}
	return in
}

// shape is the literal form the type is declared as: the struct, slice or map behind its names.
func (t *vtype) shape() (ast.Expr, scope) {
	if t == nil {
		return nil, scope{}
	}
	e, in := t.expr, t.in
	for i := 0; i < maxDepth; i++ {
		switch x := e.(type) {
		case *ast.ParenExpr:
			e = x.X
		case *ast.Ident, *ast.SelectorExpr, *ast.IndexExpr, *ast.IndexListExpr:
			u := in.resolve(x)
			if u == nil {
				return nil, scope{}
			}
			e, in = u.expr, u.in
		default:
			return e, in
		}
	}
	return nil, scope{}
}

// field returns the type of a struct field, an embedded one included.
func (t *vtype) field(name string) *vtype {
	e, in := t.shape()
	st, ok := e.(*ast.StructType)
	if !ok {
		return nil
	}
	for _, f := range st.Fields.List {
		if len(f.Names) == 0 && embeddedName(f.Type) == name {
			return in.resolve(f.Type)
		}
		for _, n := range f.Names {
			if n.Name == name {
				return in.resolve(f.Type)
			}
		}
	}
	return nil
}

// elem returns the type of the elements of a slice, an array, a map or a channel.
func (t *vtype) elem() *vtype {
	e, in := t.shape()
	switch x := e.(type) {
	case *ast.ArrayType:
		return in.resolve(x.Elt)
	case *ast.MapType:
		return in.resolve(x.Value)
	case *ast.ChanType:
		return in.resolve(x.Value)
	}
	return nil
}

// isChan reports whether the type is a channel, whose range gives its elements first.
func (t *vtype) isChan() bool {
	e, _ := t.shape()
	_, ok := e.(*ast.ChanType)
	return ok
}

// embeddedName is the field name of an embedded field: Store for store.Store or *Store.
func embeddedName(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.StarExpr:
		return embeddedName(e.X)
	case *ast.IndexExpr:
		return embeddedName(e.X)
	case *ast.IndexListExpr:
		return embeddedName(e.X)
	case *ast.SelectorExpr:
		return e.Sel.Name
	case *ast.Ident:
		return e.Name
	}
	return ""
}
