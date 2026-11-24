// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

//go:build cgo

package treesitter

// What a value in a TypeScript test is (vkind), by the locals and helper parameters that hold it.

var (
	tsPlainKinds = set("array", "object", "string", "template_string", "number", "true", "false", "null", "undefined",
		"regex", "arrow_function", "function_expression", "binary_expression", "unary_expression")
	// tsBuiltinObjects are the globals whose methods return plain data: JSON.parse(s), Object.keys(o).
	tsBuiltinObjects = set("JSON", "Object", "Array", "Math", "Number", "String", "Boolean", "Promise", "Buffer",
		"Date", "Reflect", "Symbol", "Intl", "process")
	tsBuiltinFunctions = set("String", "Number", "Boolean", "BigInt", "Symbol", "structuredClone", "parseInt", "parseFloat")
	tsBuiltinClasses   = set("Map", "Set", "WeakMap", "WeakSet", "Array", "Date", "URL", "URLSearchParams", "Error",
		"RegExp", "Uint8Array", "ArrayBuffer", "TextEncoder", "TextDecoder", "AbortController", "Headers", "FormData")
)

// kindOf is what a value of the test is (vkind): built from literals, the language's built-ins,
// external libraries or other outside values, so that a method called on it is not the
// repository's ([1, 2], new Map(), JSON.parse(s), xs.filter(…), userEvent.setup()); made from a
// class of the repository; or unknown. A cast is read by the type it names.
func (w *tsWalk) kindOf(v *node) vkind {
	if v == nil {
		return vkind{}
	}
	if k, cast := w.castKind(v); cast {
		return k
	}
	v = tsUnwrap(v)
	if v == nil {
		return vkind{}
	}
	switch k := v.Kind(); {
	case tsPlainKinds[k]:
		return w.literalKind(v)
	case k == "identifier":
		return w.nameKind(w.tf.f.text(v), nil)
	case k == "member_expression":
		return w.kindOf(v.ChildByFieldName("object")).member()
	case k == "subscript_expression":
		return w.kindOf(v.ChildByFieldName("object")).element()
	case k == "call_expression":
		return w.callKind(v)
	case k == "new_expression":
		return w.newKind(v)
	}
	return vkind{}
}

// castKind is what a cast says a value is (e as T, <T>e), and whether v is a cast that decides it.
func (w *tsWalk) castKind(v *node) (vkind, bool) {
	switch v.Kind() {
	case "as_expression":
		if t := v.NamedChild(1); t != nil && w.tf.f.text(t) != "const" {
			k := w.typeKind(t)
			return k, k.known()
		}
	case "type_assertion":
		return w.typeKind(v.NamedChild(0).NamedChild(0)).or(func() vkind { return w.kindOf(v.NamedChild(1)) }), true
	}
	return vkind{}, false
}

// receiver is the value obj.prop() is called on, as memberTarget reads it: the text of obj with the
// parentheses, casts and non-null assertions around it taken off, or the kind a cast around it
// names or a new object is. A cast that names no kind, as (st as any), gives way to the value it casts, so that the
// method is still found or counted as unresolved rather than dropped.
func (w *tsWalk) receiver(obj *node) (string, vkind) {
	for obj != nil {
		switch obj.Kind() {
		case "parenthesized_expression", "non_null_expression", "satisfies_expression":
			obj = obj.NamedChild(0)
		case "as_expression":
			if k, cast := w.castKind(obj); cast {
				return "", k
			}
			obj = obj.NamedChild(0)
		case "type_assertion":
			if k := w.typeKind(obj.NamedChild(0).NamedChild(0)); k.known() {
				return "", k
			}
			obj = obj.NamedChild(1)
		case "new_expression":
			return "", w.newKind(obj) // new Store().add(1)
		default:
			return w.tf.f.text(obj), vkind{}
		}
	}
	return "", vkind{}
}

// callKind is what a call returns: an outside value for a built-in or external function, or for a
// method of a pure value or of a built-in object (JSON.parse(s)); else unknown.
func (w *tsWalk) callKind(v *node) vkind {
	f := w.tf.f
	fn := v.ChildByFieldName("function")
	switch fn.Kind() {
	case "identifier":
		return w.nameKind(f.text(fn), tsBuiltinFunctions).member()
	case "member_expression":
		obj := tsUnwrap(fn.ChildByFieldName("object"))
		if obj != nil && obj.Kind() == "identifier" {
			return w.nameKind(f.text(obj), tsBuiltinObjects).member()
		}
		return w.kindOf(obj).member()
	}
	return vkind{}
}

// newKind is what new makes: an object of a class of the repository, or an outside value for a
// built-in or external class.
func (w *tsWalk) newKind(v *node) vkind {
	ctor := w.tf.f.field(v, "constructor")
	if target, ok := w.tf.names[ctor]; ok && !w.locals[ctor] {
		return repoKind(target)
	}
	return w.nameKind(ctor, tsBuiltinClasses).member()
}

// literalKind is what a literal is: outside, holding what it names of the repository, or of any
// value whose kind is not pure, as in [new Store()].
func (w *tsWalk) literalKind(v *node) vkind {
	out := outsideKind
	if k := v.Kind(); k != "array" && k != "object" {
		return out
	}
	walk(v, func(x *node) bool {
		switch x.Kind() {
		case "arrow_function", "function_expression":
			return false
		case "identifier", "shorthand_property_identifier", "new_expression", "call_expression":
			if !w.kindOf(x).pure() {
				out.holds = true
			}
			return false
		}
		return !out.holds
	})
	return out
}

// nameKind is what a name holds: a local's kind, a class instance, a variable imported from the
// repository by its declaration, an external import, or one of builtins unless the repository or
// the test's loaded modules bind it. Anything else is unknown.
func (w *tsWalk) nameKind(name string, builtins map[string]bool) vkind {
	if c := w.vars[name]; c != "" {
		return repoKind(c)
	}
	if w.locals[name] {
		return w.kinds[name]
	}
	if _, ok := w.loaded[name]; ok {
		return vkind{}
	}
	if _, ok := w.modules[name]; ok {
		return vkind{}
	}
	if _, ok := w.tf.names[name]; ok {
		return w.tf.r.kinds.importedValue(w.tf.f.path, name)
	}
	if _, ok := w.tf.spaces[name]; ok {
		return vkind{}
	}
	if w.tf.external[name] || builtins[name] {
		return outsideKind
	}
	return vkind{}
}

// typeKind is what a value of a type written in the walker's file is.
func (w *tsWalk) typeKind(t *node) vkind {
	p := w.tf.f.path
	return tsTypeKind(w.tf.f, t, func(name string) vkind { return w.tf.r.kinds.typeName(p, name) })
}

// patternKinds is what each name a declaration's pattern takes from a value of kind k holds: the
// name itself holds k, the names of an array pattern its elements, and the names of an object
// pattern its members.
func (w *tsWalk) patternKinds(pattern *node, k vkind) map[string]vkind {
	out := map[string]vkind{}
	f := w.tf.f
	var take func(n *node, k vkind)
	take = func(n *node, k vkind) {
		if n == nil {
			return
		}
		switch n.Kind() {
		case "identifier", "shorthand_property_identifier_pattern":
			out[f.text(n)] = k
		case "array_pattern":
			for _, c := range named(n) {
				if c.Kind() == "rest_pattern" {
					take(c.NamedChild(0), k)
				} else {
					take(c, k.element())
				}
			}
		case "object_pattern":
			for _, name := range tsPatternNames(f, n) {
				out[name] = k.member()
			}
		case "assignment_pattern":
			take(n.ChildByFieldName("left"), vkind{})
		}
	}
	take(pattern, k)
	return out
}

// loop records the variables a for … of or for … in loop declares: the elements of what it goes
// through, or its keys, which are strings.
func (w *tsWalk) loop(n *node) {
	k := w.kindOf(n.ChildByFieldName("right")).element()
	for c := n.ChildByFieldName("left"); c != nil; c = c.nextSibling {
		if c.Kind() == "in" {
			k = outsideKind
		}
	}
	w.bindLocals(n.ChildByFieldName("left"), k)
}

// bindLocals makes the names a pattern declares locals of the test that hold what they take from a
// value of kind k (see patternKinds).
func (w *tsWalk) bindLocals(pattern *node, k vkind) {
	for local, lk := range w.patternKinds(pattern, k) {
		w.locals[local] = true
		delete(w.vars, local)
		delete(w.loaded, local)
		delete(w.modules, local)
		delete(w.rows, local)
		w.kinds[local] = lk
		if lk.repo != "" {
			w.vars[local] = lk.repo
		}
	}
}
