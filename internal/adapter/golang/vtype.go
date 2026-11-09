package golang

import "go/ast"

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
