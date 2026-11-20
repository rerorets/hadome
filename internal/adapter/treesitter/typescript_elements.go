// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

//go:build cgo

package treesitter

// elements returns the variables of a function that take out an element of a container whose
// written type names one of the repository's types as its elements, by name, with that type's ID:
// s in for (const s of stores) or const s = stores[0], with stores: Store[]. The container is a
// parameter, a local, a field of the class, or a value at the top of the file or imported into it;
// its kind is read as tests read it (tsTypeKind).
func (ty *tsTypes) elements(decl, class *node) map[string]string {
	f := ty.tr.f
	typed := map[string]*node{}
	walk(decl, func(x *node) bool {
		switch x.Kind() {
		case "required_parameter", "optional_parameter":
			if p := x.ChildByFieldName("pattern"); p != nil && p.Kind() == "identifier" {
				typed[f.text(p)] = x.ChildByFieldName("type")
			}
		case "variable_declarator":
			if n := x.ChildByFieldName("name"); n != nil && n.Kind() == "identifier" {
				typed[f.text(n)] = x.ChildByFieldName("type")
			}
		}
		return true
	})
	container := func(v *node) vkind { return ty.container(v, typed, class) }
	out := map[string]string{}
	walk(decl.ChildByFieldName("body"), func(x *node) bool {
		var name *node
		k := vkind{}
		switch x.Kind() {
		case "for_in_statement":
			name, k = x.ChildByFieldName("left"), container(x.ChildByFieldName("right")).element()
			for c := name; c != nil; c = c.nextSibling {
				if c.Kind() == "in" {
					k = vkind{}
				}
			}
		case "variable_declarator":
			if v := tsUnwrap(x.ChildByFieldName("value")); v != nil && v.Kind() == "subscript_expression" && x.ChildByFieldName("type") == nil {
				name, k = x.ChildByFieldName("name"), container(v.ChildByFieldName("object")).element()
			}
		}
		if name != nil && name.Kind() == "identifier" && k.repo != "" {
			out[f.text(name)] = k.repo
		}
		return true
	})
	return out
}

// container is what a value an element is taken out of is: a parameter or local by its written
// type (typed), a field of the class, or a value at the top of the file or imported into it.
func (ty *tsTypes) container(v *node, typed map[string]*node, class *node) vkind {
	tr, f := ty.tr, ty.tr.f
	kind := func(t *node) vkind {
		return tsTypeKind(f, t, func(n string) vkind { return tr.r.kinds.typeName(f.path, n) })
	}
	v = tsUnwrap(v)
	switch {
	case v == nil:
	case v.Kind() == "identifier":
		name := f.text(v)
		if t, local := typed[name]; local {
			return kind(t)
		}
		if d := tr.top[name]; d != nil && d.Kind() == "variable_declarator" {
			return tr.r.kinds.declaredValue(f.path, name)
		}
		return tr.r.kinds.importedValue(f.path, name)
	case v.Kind() == "member_expression" && f.field(v, "object") == "this":
		return kind(tsFieldType(f, class, f.field(v, "property")))
	}
	return vkind{}
}

// tsFieldType is the written type of a field of a class, or of a constructor parameter that is a
// property; nil for none.
func tsFieldType(f *file, class *node, name string) *node {
	if class == nil {
		return nil
	}
	for _, m := range named(class.ChildByFieldName("body")) {
		switch {
		case m.Kind() == "public_field_definition" && f.field(m, "name") == name:
			return m.ChildByFieldName("type")
		case m.Kind() == "method_definition" && f.field(m, "name") == "constructor":
			for _, p := range named(m.ChildByFieldName("parameters")) {
				if tsParameterProperty(p) && f.field(p, "pattern") == name {
					return p.ChildByFieldName("type")
				}
			}
		}
	}
	return nil
}
