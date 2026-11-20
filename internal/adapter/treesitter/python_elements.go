// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

//go:build cgo

package treesitter

// pyElements returns the variables of a function that take out an element of a container whose
// annotation names one of the repository's classes as its elements, by name, with that class's ID:
// s in for s in stores or s = stores[0], with stores: list[Store]. The container is a parameter, an
// attribute of the class, or a value at the top of the module or imported into it; its kind is read
// as tests read it (pyTypeKind).
func pyElements(m pyModule, fn tfunc, classDef *node) map[string]string {
	f, ks := m.tr.f, m.kinds
	kind := func(t *node) vkind {
		return pyTypeKind(f, t, func(n string) vkind { return ks.typeName(f.path, n) })
	}
	params := pyParamTypes(f, definition(fn.decl))
	container := func(v *node) vkind {
		switch {
		case v == nil:
		case v.Kind() == "identifier":
			name := f.text(v)
			if t, ok := params[name]; ok {
				return kind(t)
			}
			if ks.top(f.path, name) != nil {
				return ks.declaredValue(m.mod, name)
			}
			return ks.importedValue(f.path, name)
		case v.Kind() == "attribute" && f.field(v, "object") == "self":
			return kind(pyAttributeType(f, classDef, f.field(v, "attribute")))
		}
		return vkind{}
	}
	out := map[string]string{}
	walk(fn.body, func(x *node) bool {
		var name *node
		k := vkind{}
		switch x.Kind() {
		case "for_statement":
			name, k = x.ChildByFieldName("left"), container(x.ChildByFieldName("right")).element()
		case "assignment":
			if v := x.ChildByFieldName("right"); v != nil && v.Kind() == "subscript" && x.ChildByFieldName("type") == nil {
				name, k = x.ChildByFieldName("left"), container(v.ChildByFieldName("value")).element()
			}
		}
		if name != nil && name.Kind() == "identifier" && k.repo != "" {
			out[f.text(name)] = k.repo
		}
		return true
	})
	return out
}

// pyParamTypes returns the annotations of a function's annotated parameters, by name.
func pyParamTypes(f *file, def *node) map[string]*node {
	out := map[string]*node{}
	for _, p := range named(def.ChildByFieldName("parameters")) {
		if p.Kind() == "typed_parameter" || p.Kind() == "typed_default_parameter" {
			name := f.field(p, "name")
			if name == "" && p.NamedChildCount() > 0 {
				name = f.text(p.NamedChild(0))
			}
			out[name] = p.ChildByFieldName("type")
		}
	}
	return out
}

// pyAttributeType is the annotation of an attribute of a class: the class body's (stores:
// list[Store]), __init__'s (self.stores: list[Store] = …), or that of the parameter __init__ sets
// it from; nil for none.
func pyAttributeType(f *file, classDef *node, name string) *node {
	if classDef == nil {
		return nil
	}
	var out *node
	for _, m := range named(classDef.ChildByFieldName("body")) {
		md := definition(m)
		switch {
		case md.Kind() == "expression_statement" && md.NamedChild(0) != nil && md.NamedChild(0).Kind() == "assignment":
			if as := md.NamedChild(0); f.field(as, "left") == name && as.ChildByFieldName("type") != nil {
				out = as.ChildByFieldName("type")
			}
		case md.Kind() == "function_definition" && f.field(md, "name") == "__init__":
			params := pyParamTypes(f, md)
			walk(md.ChildByFieldName("body"), func(n *node) bool {
				left := n.ChildByFieldName("left")
				if n.Kind() != "assignment" || left == nil || left.Kind() != "attribute" || f.field(left, "object") != "self" ||
					f.field(left, "attribute") != name {
					return true
				}
				if t := n.ChildByFieldName("type"); t != nil {
					out = t
				} else if t, ok := params[f.field(n, "right")]; ok {
					out = t
				}
				return true
			})
		}
	}
	return out
}
