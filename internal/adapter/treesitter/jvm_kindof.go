// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

//go:build cgo

package treesitter

// What a value in a Java or Kotlin test is (vkind), by the locals that hold it.

// typeKind is what a value of a type written in the walker's file is.
func (w *jvmWalk) typeKind(t *node) vkind {
	return jvmTypeKind(w.jf.f, t, func(name string) vkind { return w.jf.j.typeName(w.jf.names, w.jf.external, name) })
}

// nameKind is what a name holds: an object of the product's class, a local's kind, a top-level
// property of the product, or an object of the standard library. Anything else is unknown.
func (w *jvmWalk) nameKind(name string) vkind {
	if c := w.vars[name]; c != "" {
		return repoKind(c)
	}
	if w.locals[name] {
		return w.kinds[name]
	}
	if t := w.jf.names[name]; t != "" {
		return w.jf.j.field(packageOf(t), lastElem(t))
	}
	return vkind{}
}

// bindLocal makes a name a local of the test holding a value of kind k.
func (w *jvmWalk) bindLocal(name string, k vkind) {
	w.locals[name] = true
	if w.kinds == nil {
		w.kinds = map[string]vkind{}
	}
	w.kinds[name] = k
	if k.repo != "" && w.vars[name] == "" {
		w.vars[name] = k.repo
	}
}

// receiverKind is what the value the call n is made on is, when it is written as a value rather
// than a name: a cast, a value in parentheses or a new object, as in ((Store) o).add(1) and
// new Store().add(1). A receiver that is a name is left to invoke, which reads it as a name.
func (w *jvmWalk) receiverKind(n *node) vkind {
	var recv *node
	kindOf := w.javaKindOf
	switch n.Kind() {
	case "method_invocation":
		recv = n.ChildByFieldName("object")
	case "call_expression": // Kotlin: the receiver of the navigation it calls
		if callee := n.NamedChild(0); callee.Kind() == "navigation_expression" && callee.NamedChildCount() == 2 {
			recv, kindOf = callee.NamedChild(0), w.ktKindOf
		}
	}
	if recv == nil {
		return vkind{}
	}
	switch recv.Kind() {
	case "parenthesized_expression", "cast_expression", "as_expression", "object_creation_expression":
		return kindOf(recv)
	}
	return vkind{}
}

// javaKindOf is what a Java value is (vkind): an outside value built from literals, the standard
// library or other outside values, an object of the product's class, a field of one by its
// declared type, or unknown.
func (w *jvmWalk) javaKindOf(e *node) vkind {
	if e == nil {
		return vkind{}
	}
	f := w.jf.f
	switch k := e.Kind(); {
	case k == "parenthesized_expression":
		return w.javaKindOf(e.NamedChild(0))
	case k == "cast_expression":
		return w.typeKind(e.ChildByFieldName("type")).or(func() vkind { return w.javaKindOf(e.ChildByFieldName("value")) })
	case javaLiteralKinds[k] || k == "array_creation_expression":
		return w.literalKind(e, w.javaKindOf)
	case k == "identifier":
		return w.nameKind(f.text(e))
	case k == "field_access":
		return w.memberKind(e.ChildByFieldName("object"), f.field(e, "field"), w.javaKindOf)
	case k == "array_access":
		return w.javaKindOf(e.ChildByFieldName("array")).element()
	case k == "method_invocation":
		return w.javaCallKind(e.ChildByFieldName("object"))
	case k == "object_creation_expression":
		if class, ok := w.jf.names[typeName(f.field(e, "type"))]; ok && w.jf.j.isClass(class) {
			return repoKind(class)
		}
		return w.typeKind(e.ChildByFieldName("type"))
	}
	return vkind{}
}

// memberKind is what obj.member is: a field of a class of the product by its declaration, or a
// member of the value obj.
func (w *jvmWalk) memberKind(obj *node, member string, kindOf func(*node) vkind) vkind {
	name := w.jf.f.text(obj)
	if class := w.jf.names[name]; class != "" && w.jf.j.isClass(class) && !w.locals[name] {
		return w.jf.j.field(class, member)
	}
	return kindOf(obj).member()
}

// javaCallKind is what a Java method called on obj returns: an outside value for a static method of
// the standard library (List.of), else what a member of obj is. An unqualified call is unknown.
func (w *jvmWalk) javaCallKind(obj *node) vkind {
	if obj == nil {
		return vkind{}
	}
	if name := w.jf.f.text(obj); obj.Kind() == "identifier" && javaStatics[name] && w.jf.names[name] == "" && !w.locals[name] {
		return outsideKind
	}
	return w.javaKindOf(obj).member()
}

// ktKindOf is what a Kotlin value is (vkind), as javaKindOf: literals, the standard library's
// builders and the members of outside values are outside.
func (w *jvmWalk) ktKindOf(e *node) vkind {
	if e == nil {
		return vkind{}
	}
	f := w.jf.f
	switch k := e.Kind(); {
	case k == "parenthesized_expression":
		return w.ktKindOf(e.NamedChild(0))
	case k == "as_expression":
		return w.typeKind(e.ChildByFieldName("right")).or(func() vkind { return w.ktKindOf(e.ChildByFieldName("left")) })
	case ktLiteralKinds[k]:
		return w.literalKind(e, w.ktKindOf)
	case ktOperations[k]:
		return w.ktOperands(e)
	case k == "identifier":
		return w.nameKind(f.text(e))
	case k == "navigation_expression" && e.NamedChildCount() == 2:
		return w.memberKind(e.NamedChild(0), f.text(e.NamedChild(1)), w.ktKindOf)
	case k == "index_expression":
		return w.ktKindOf(e.NamedChild(0)).element()
	case k == "call_expression":
		return w.ktCallKind(e)
	}
	return vkind{}
}

// ktOperands is what an operation makes: outside when every operand is pure, else unknown, as a
// class may define the operator (Money + Money).
func (w *jvmWalk) ktOperands(e *node) vkind {
	for _, o := range named(e) {
		if !w.ktKindOf(o).pure() {
			return vkind{}
		}
	}
	return outsideKind
}

// ktCallKind is what a Kotlin call returns: an object for a class of the product, what a standard
// builder makes of its arguments, and for a method what a member of its receiver is.
func (w *jvmWalk) ktCallKind(e *node) vkind {
	f := w.jf.f
	callee := e.NamedChild(0)
	if class := w.jf.ktBuilt(e); class != "" && w.jf.j.isClass(class) {
		return repoKind(class)
	}
	switch name := f.text(callee); {
	case callee.Kind() == "identifier" && ktBuilders[name] && w.jf.names[name] == "" && w.helper(name).n == nil:
		return w.literalKind(e.NamedChild(1), w.ktKindOf)
	case callee.Kind() == "navigation_expression" && callee.NamedChildCount() == 2:
		return w.ktKindOf(callee.NamedChild(0)).member()
	}
	return vkind{}
}

// literalKind is what a literal, or a builder's arguments, make: an outside value, holding what
// they name of the product or of any value whose kind is not pure.
func (w *jvmWalk) literalKind(e *node, kindOf func(*node) vkind) vkind {
	out := outsideKind
	walk(e, func(x *node) bool {
		switch x.Kind() {
		case "lambda_expression", "lambda_literal":
			return false
		case "identifier", "method_invocation", "object_creation_expression", "field_access", "call_expression",
			"navigation_expression":
			if x != e && !kindOf(x).pure() {
				out.holds = true
			}
			return x == e
		}
		return !out.holds
	})
	return out
}

// ktLoop records the variables a for loop declares: the elements of what it goes through, and for
// a destructuring declaration their members.
func (w *jvmWalk) ktLoop(decl, over *node) {
	k := w.ktKindOf(over).element()
	switch decl.Kind() {
	case "variable_declaration":
		w.bindLocal(w.jf.f.text(decl.NamedChild(0)), k)
	case "multi_variable_declaration":
		for _, v := range named(decl) {
			if v.Kind() == "variable_declaration" {
				w.bindLocal(w.jf.f.text(v.NamedChild(0)), k.member())
			}
		}
	}
}

// javaLoop records the variable an enhanced for loop declares when the walk reaches its name, before
// the body: its declared type, else the elements of what it goes through.
func (w *jvmWalk) javaLoop(n *node) {
	p := n.Parent()
	if p == nil || p.Kind() != "enhanced_for_statement" || p.ChildByFieldName("name") != n {
		return
	}
	k := w.typeKind(p.ChildByFieldName("type")).or(func() vkind { return w.javaKindOf(p.ChildByFieldName("value")).element() })
	w.bindLocal(w.jf.f.text(n), k)
}
