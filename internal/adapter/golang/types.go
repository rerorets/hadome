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
	// method is "<import path>.<Type>.<Method>" for a method value (f := s.Append): calling the
	// value calls the method. It is empty for other values.
	method string
	// ext is "<import path>.<Type>" of a concrete type of a module outside the repository. A method
	// called on a value of it runs that module's code, like a call of one of its functions. It is
	// empty for other types.
	ext string
}

// maxDepth bounds the chains of named types and package-level variables followed, so that code
// that does not compile cannot make the adapter loop.
const maxDepth = 16

// declRef is a declaration in the index, with the scope it is written in.
type declRef struct {
	typ   ast.Expr // the declared type, or nil
	value ast.Expr // a variable's initial value, or nil
	// nth is which of value's results the variable takes, for var a, b = f(); it is 0 otherwise.
	nth int
	in  scope
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
		if t := s.resolve(e.X); t != nil {
			return t
		}
		// A pointer to an interface has no methods, so a pointer whose method is called points to a
		// concrete type.
		return s.external(e.X)
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
		if t := s.resolve(e.Type); t != nil {
			return t
		}
		return s.external(e.Type) // an interface cannot be made by a composite literal
	case *ast.CallExpr:
		return s.resultType(e, 0, depth)
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

// nthValueType returns the type of the n-th value e gives: the n-th result of a call, or, with n 0,
// the type of any other value. The second value of v, ok := m[k] and the like is not read.
func (s scope) nthValueType(e ast.Expr, n, depth int) *vtype {
	if c, ok := ast.Unparen(e).(*ast.CallExpr); ok {
		return s.resultType(c, n, depth)
	}
	if n == 0 {
		return s.valueTypeAt(e, depth)
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
	x := s.valueTypeAt(sel.X, depth+1)
	if f := x.field(sel.Sel.Name); f != nil {
		return f
	}
	if x != nil && x.id != "" && s.idx.methods[x.id+"."+sel.Sel.Name] {
		return &vtype{method: x.id + "." + sel.Sel.Name}
	}
	return nil
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

// resultType returns the type of a call's n-th result: a function or method of the repository, a
// helper of the test files, new(T) or a conversion T(x).
func (s scope) resultType(c *ast.CallExpr, n, depth int) *vtype {
	fun := ast.Unparen(c.Fun)
	switch f := fun.(type) {
	case *ast.IndexExpr: // a generic function instantiated, New[int]()
		fun = f.X
	case *ast.IndexListExpr:
		fun = f.X
	}
	if t, local := s.localResult(fun, c, n); local {
		return t
	}
	if h, ok := s.supportFunc(fun); ok {
		return h.resultType(s, n)
	}
	if id := s.qualify(fun, func(id string) bool { return s.idx.funcs[id] }); id != "" {
		return s.idx.result(id, n)
	}
	if n > 0 { // a conversion gives one value
		return nil
	}
	if t := s.qualify(fun, func(id string) bool { return s.idx.types[id] }); t != "" {
		return s.resolve(fun) // a conversion
	}
	switch f := fun.(type) {
	case *ast.ParenExpr: // a conversion to a written type, (*T)(x)
		return s.resolve(f.X)
	case *ast.SelectorExpr:
		if s.isPackage(f.X) {
			if p := s.imports[f.X.(*ast.Ident).Name]; !s.idx.internal(p) {
				return externalResult(p, f.Sel.Name, n, false)
			}
			return nil
		}
		t := s.valueTypeAt(f.X, depth+1)
		switch {
		case t == nil:
		case t.id != "":
			return s.idx.result(t.id+"."+f.Sel.Name, n)
		case t.ext != "":
			return externalResult(t.ext, f.Sel.Name, n, true)
		}
	}
	return nil
}

// localResult returns the result type of a call of a name the test knows: a function held in a
// variable, new(T), or a helper of the test files. It reports false for any other call.
func (s scope) localResult(fun ast.Expr, c *ast.CallExpr, n int) (*vtype, bool) {
	id, ok := fun.(*ast.Ident)
	if !ok {
		return nil, false
	}
	if t, local := s.lookup(id.Name); local {
		if t != nil && t.method != "" {
			return s.idx.result(t.method, n), true
		}
		return nil, true
	}
	if id.Name == "new" && len(c.Args) == 1 && n == 0 {
		return s.resolve(c.Args[0]), true
	}
	if h, ok := s.helpers[id.Name]; ok {
		return h.resultType(s, n), true
	}
	return nil, false
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

// isInterface reports whether the type is an interface, whose methods run the code of whatever
// value it holds.
func (t *vtype) isInterface() bool {
	e, _ := t.shape()
	_, ok := e.(*ast.InterfaceType)
	return ok
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
