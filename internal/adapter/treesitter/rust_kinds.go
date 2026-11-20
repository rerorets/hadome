// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

//go:build cgo

package treesitter

import "strings"

// What a Rust type or value is (vkind), as a module of the version writes it: the types and
// constants it declares at its top or brings in with use.

var (
	// rustBuiltinTypes are the types of the language and its standard library that need no path.
	rustBuiltinTypes = set("bool", "char", "str", "String", "Vec", "VecDeque", "HashMap", "HashSet", "BTreeMap",
		"BTreeSet", "Option", "Result", "Box", "Rc", "Arc")
	// rustElementTypes are the containers whose first type argument is what an index, a loop or ?
	// takes out.
	rustElementTypes = set("Vec", "VecDeque", "HashSet", "BTreeSet", "Option", "Result")
	// rustIterators are the methods that go through a container's elements as they are.
	rustIterators = set("iter", "iter_mut", "into_iter")
	// rustStd are the crates of the standard library.
	rustStd = set("std", "core", "alloc")
	// rustWrappers are the expressions whose value is that of the one inside: &x, (x), x.await.
	rustWrappers = set("reference_expression", "parenthesized_expression", "await_expression")
	// rustLiteralMacros are the macros that build an outside value of their arguments.
	rustLiteralMacros = set("vec", "format", "concat")
	// rustLiteralKinds are the expressions that build an outside value whatever they hold.
	rustLiteralKinds = set("string_literal", "raw_string_literal", "integer_literal", "float_literal", "boolean_literal",
		"char_literal", "array_expression", "tuple_expression", "unit_expression",
		"closure_expression")
)

// rustTypeKind is the kind of a value of the written type t. name gives the kind a type name, with
// its path or not, stands for.
func rustTypeKind(f *file, t *node, name func(string) vkind) vkind {
	if t == nil {
		return vkind{}
	}
	switch t.Kind() {
	case "primitive_type", "unit_type", "never_type", "function_type":
		return outsideKind
	case "reference_type", "pointer_type":
		return rustTypeKind(f, t.ChildByFieldName("type"), name)
	case "type_identifier", "scoped_type_identifier":
		return name(f.text(t))
	case "array_type":
		return containerOf(rustTypeKind(f, t.ChildByFieldName("element"), name))
	case "tuple_type":
		k := outsideKind
		for _, c := range named(t) {
			if !rustTypeKind(f, c, name).pure() {
				k.holds = true
			}
		}
		return k
	case "dynamic_type", "abstract_type":
		return rustTypeKind(f, t.NamedChild(0), name)
	case "generic_type":
		return rustGenericKind(f, t, name)
	}
	return vkind{}
}

// rustGenericKind is the kind of a generic type: a pointer is what it points to, a container's
// elements are its first argument's kind, another outside type holds what its arguments hold, and
// the repository's generic type is that type.
func rustGenericKind(f *file, t *node, name func(string) vkind) vkind {
	base := f.field(t, "type")
	short := base[strings.LastIndex(base, ":")+1:]
	args := named(t.ChildByFieldName("type_arguments"))
	k := name(base)
	switch {
	case !k.outside:
		return k
	case rustPointers[short] && len(args) > 0:
		return rustTypeKind(f, args[0], name)
	case rustElementTypes[short] && len(args) > 0:
		return containerOf(rustTypeKind(f, args[0], name))
	}
	for _, a := range args {
		if a.Kind() != "lifetime" && !rustTypeKind(f, a, name).pure() {
			k.holds = true
		}
	}
	return k
}

// rustKinds is what the names of the version's modules stand for, read when first needed.
type rustKinds struct {
	r     *rust
	binds map[string]map[string]string // file path -> local name -> the module location and name it brings in; "" for outside
	types map[string]vkind             // "<location>.<name>" -> what the type declared there stands for
	busy  map[string]bool              // the names being read, so that a cycle reads as unknown
}

func newRustKinds(r *rust) *rustKinds {
	return &rustKinds{r: r, binds: map[string]map[string]string{}, types: map[string]vkind{}, busy: map[string]bool{}}
}

// bindsOf returns what the use declarations at the top of the file f bring in: a local name -> the
// location of the module and the name in it ("<location>.<name>"), or "" for a name from outside the
// version.
func (ks *rustKinds) bindsOf(f *file) map[string]string {
	if m, ok := ks.binds[f.path]; ok {
		return m
	}
	m := map[string]string{}
	ks.binds[f.path] = m
	for _, u := range uses(f, f.root) {
		loc, internal := ks.r.resolve(f.path, u.segs)
		last := u.segs[len(u.segs)-1]
		switch {
		case !internal:
			m[u.local] = ""
		case last != "*" && !strings.HasSuffix(loc, "/"+last):
			m[u.local] = loc + "." + last
		}
	}
	return m
}

// typeName is what a type name written in the file f stands for. names are the names the code
// around it brings in from the version (local name -> "<location>.<name>"), and loc the location of
// its module.
func (ks *rustKinds) typeName(f *file, loc string, names map[string]string, name string) vkind {
	if root, _, scoped := strings.Cut(name, "::"); scoped {
		if rustStd[root] {
			return outsideKind
		}
		return vkind{}
	}
	if id, ok := names[name]; ok {
		return ks.declaredType(id)
	}
	if id, ok := ks.bindsOf(f)[name]; ok {
		if id == "" {
			return outsideKind
		}
		return ks.declaredType(id)
	}
	if ks.item(f, name) != nil {
		return ks.declaredType(loc + "." + name)
	}
	if rustBuiltinTypes[name] {
		return outsideKind
	}
	return vkind{}
}

// declaredType is what the type with the ID "<location>.<name>" stands for where it is declared:
// the repository's struct, enum or trait, or the type an alias names.
func (ks *rustKinds) declaredType(id string) vkind {
	if k, ok := ks.types[id]; ok {
		return k
	}
	if ks.busy[id] {
		return vkind{}
	}
	ks.busy[id] = true
	defer delete(ks.busy, id)
	var k vkind
	dot := strings.LastIndex(id, ".")
	loc, name := id[:dot], id[dot+1:]
	if f := ks.r.files[loc]; f != nil {
		switch d := ks.item(f, name); {
		case d == nil:
		case d.Kind() == "struct_item" || d.Kind() == "enum_item" || d.Kind() == "trait_item" || d.Kind() == "union_item":
			k = repoKind(id)
		case d.Kind() == "type_item":
			k = rustTypeKind(f, d.ChildByFieldName("type"), func(n string) vkind { return ks.typeName(f, loc, nil, n) })
		}
	}
	ks.types[id] = k
	return k
}

// constant is what the constant or static with the ID "<location>.<name>" holds, by its type;
// unknown for an ID that names none.
func (ks *rustKinds) constant(id string) vkind {
	dot := strings.LastIndex(id, ".")
	loc, name := id[:dot], id[dot+1:]
	f := ks.r.files[loc]
	if f == nil {
		return vkind{}
	}
	d := ks.item(f, name)
	if d == nil || (d.Kind() != "const_item" && d.Kind() != "static_item") {
		return vkind{}
	}
	return rustTypeKind(f, d.ChildByFieldName("type"), func(n string) vkind { return ks.typeName(f, loc, nil, n) })
}

// item is the item of a name at the top of the file f, or nil.
func (ks *rustKinds) item(f *file, name string) *node {
	for _, c := range named(f.root) {
		switch c.Kind() {
		case "struct_item", "enum_item", "trait_item", "union_item", "type_item", "const_item", "static_item":
			if f.field(c, "name") == name {
				return c
			}
		}
	}
	return nil
}

// kindOf is what a value of the test is (vkind): an outside value built from literals, the
// standard library or other outside values, an object of the repository's type, or unknown.
func (w *rustWalk) kindOf(v *node) vkind {
	if v == nil {
		return vkind{}
	}
	f := w.sc.f
	switch k := v.Kind(); {
	case rustWrappers[k]:
		return w.kindOf(v.NamedChild(v.NamedChildCount() - 1))
	case k == "try_expression" || k == "index_expression":
		return w.kindOf(v.NamedChild(0)).element()
	case k == "binary_expression" || k == "unary_expression":
		return w.operands(v)
	case rustLiteralKinds[k] || (k == "macro_invocation" && rustLiteralMacros[f.field(v, "macro")]):
		return w.literalKind(v)
	case k == "identifier":
		return w.nameKind(f.text(v))
	case k == "field_expression":
		return w.kindOf(v.ChildByFieldName("value")).member()
	case k == "call_expression" || k == "struct_expression":
		return w.callKind(v)
	}
	return vkind{}
}

// callKind is what a call or a struct expression makes: an object for a type of the repository
// built (Store::new(), Store { .. }), an outside value for one of the standard library's
// (String::from(s)), the elements as they are for an iterator over a container (xs.iter()), and an
// outside value for another method of a pure value.
func (w *rustWalk) callKind(v *node) vkind {
	f := w.sc.f
	if v.Kind() == "struct_expression" {
		return repoKind(w.typeOf(v)) // unknown for a struct of none of the crate's types
	}
	fn := v.ChildByFieldName("function")
	switch fn.Kind() {
	case "field_expression":
		obj := w.kindOf(fn.ChildByFieldName("value"))
		if rustIterators[f.field(fn, "field")] {
			return containerOf(obj.element())
		}
		return obj.member()
	case "scoped_identifier":
		if typ := w.typeOf(v); typ != "" {
			return repoKind(typ)
		}
		return w.typeKind(fn.ChildByFieldName("path")).member()
	case "identifier":
		if id, ok := w.sc.r.kinds.bindsOf(f)[f.text(fn)]; ok && id == "" {
			return outsideKind
		}
	}
	return vkind{}
}

// operands is what an operation makes: a comparison or a logical operation a bool, and an
// arithmetic one an outside value when every operand is pure, else unknown, as the operator may be
// a trait implemented by an operand's type (Money + Money).
func (w *rustWalk) operands(v *node) vkind {
	for c := v.firstChild; c != nil; c = c.nextSibling {
		switch c.Kind() {
		case "==", "!=", "<", ">", "<=", ">=", "&&", "||", "!":
			return outsideKind
		}
	}
	for _, o := range named(v) {
		if !w.kindOf(o).pure() {
			return vkind{}
		}
	}
	return outsideKind
}

// literalKind is what a literal is: outside, holding what it names of the repository, or of any
// value whose kind is not pure.
func (w *rustWalk) literalKind(v *node) vkind {
	out := outsideKind
	walk(v, func(x *node) bool {
		switch x.Kind() {
		case "closure_expression":
			return x == v
		case "identifier":
			if w.product(w.sc.f.text(x)) || (w.locals[w.sc.f.text(x)] && !w.nameKind(w.sc.f.text(x)).pure()) {
				out.holds = true
			}
		case "call_expression", "struct_expression", "field_expression", "index_expression":
			if !w.kindOf(x).pure() {
				out.holds = true
			}
			return false
		}
		return !out.holds
	})
	return out
}

// nameKind is what a name holds: an object of the repository's type, a local's kind, a constant of
// the repository by its type, a unit struct of the repository, or a name from outside. Anything
// else is unknown.
func (w *rustWalk) nameKind(name string) vkind {
	if c := w.vars[name]; c != "" {
		return repoKind(c)
	}
	if w.locals[name] {
		return w.kinds[name]
	}
	if id, ok := w.sc.names[name]; ok {
		// a type named as a value is a unit struct: let s = Store
		return w.sc.r.kinds.constant(id).or(func() vkind { return w.sc.r.kinds.declaredType(id) })
	}
	if id, ok := w.sc.r.kinds.bindsOf(w.sc.f)[name]; ok && id == "" {
		return outsideKind
	}
	return vkind{}
}

// typeKind is what a value of a type written in the test's module is; a path is read as a type.
func (w *rustWalk) typeKind(t *node) vkind {
	if t == nil {
		return vkind{}
	}
	f := w.sc.f
	if t.Kind() == "identifier" || t.Kind() == "scoped_identifier" {
		return w.sc.r.kinds.typeName(f, "", w.sc.names, strings.ReplaceAll(f.text(t), " ", ""))
	}
	return rustTypeKind(f, t, func(n string) vkind { return w.sc.r.kinds.typeName(f, "", w.sc.names, n) })
}

// bindLocals makes the names a pattern declares locals of the test holding what they take from a
// value of kind k: the name itself k, the names of a tuple or slice pattern its elements.
func (w *rustWalk) bindLocals(p *node, k vkind) {
	if p == nil {
		return
	}
	f := w.sc.f
	switch p.Kind() {
	case "identifier":
		name := f.text(p)
		w.locals[name] = true
		delete(w.vars, name)
		w.kinds[name] = k
		if k.repo != "" {
			w.vars[name] = k.repo
		}
	case "mut_pattern", "ref_pattern", "reference_pattern":
		w.bindLocals(p.NamedChild(p.NamedChildCount()-1), k)
	case "tuple_pattern", "slice_pattern":
		for _, c := range named(p) {
			w.bindLocals(c, k.element())
		}
	}
}
