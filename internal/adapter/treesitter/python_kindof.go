// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

//go:build cgo

package treesitter

// What a value in a Python test is (vkind), by the locals that hold it.

// kindOf is what a value of the test is (vkind): an outside value built from literals, built-ins
// or external modules ([1, 2], list(xs), json.loads(s), xs.copy()), an object of the repository's
// class, or unknown.
func (w *pyWalk) kindOf(v *node) vkind {
	if v == nil {
		return vkind{}
	}
	f := w.pf.f
	switch k := v.Kind(); {
	case k == "parenthesized_expression" || k == "await":
		return w.kindOf(v.NamedChild(0))
	case k == "binary_operator" || k == "unary_operator":
		return w.operands(v) // a class may define the operator: Money(1) + Money(2)
	case pyLiteralKinds[k]:
		return w.literalKind(v)
	case k == "identifier":
		return w.nameKind(f.text(v))
	case k == "attribute":
		return w.kindOf(v.ChildByFieldName("object")).member()
	case k == "subscript":
		return w.kindOf(v.ChildByFieldName("value")).element()
	case k == "call":
		return w.callKind(v)
	}
	return vkind{}
}

// callKind is what a call returns: an outside value for a built-in function, a copy of what its
// argument holds for one that copies (list(xs)), an object for a class of the repository, and an
// outside value for a method of a pure value.
func (w *pyWalk) callKind(v *node) vkind {
	f := w.pf.f
	fn := v.ChildByFieldName("function")
	if fn.Kind() == "attribute" {
		return w.kindOf(fn.ChildByFieldName("object")).member()
	}
	name := f.text(fn)
	if w.locals[name] || w.pf.r.kinds.top(w.pf.f.path, name) != nil {
		return vkind{}
	}
	if from, ok := w.pf.r.kinds.importsOf(w.pf.f.path)[name]; ok {
		if from[0] == "" {
			return outsideKind
		}
		if c := w.pf.r.kinds.declaredType(from[0], from[1]); c.repo != "" {
			return c
		}
		return vkind{}
	}
	args := named(v.ChildByFieldName("arguments"))
	switch {
	case pyCopies[name] && len(args) == 1:
		return containerOf(w.kindOf(args[0]).element())
	case pyCopies[name] || pyBuiltinFunctions[name]:
		return outsideKind
	}
	return vkind{}
}

// operands is what an arithmetic operation makes: outside when every operand is pure, else unknown,
// as the operator may be a method of an operand's class.
func (w *pyWalk) operands(v *node) vkind {
	for _, o := range named(v) {
		if !w.kindOf(o).pure() {
			return vkind{}
		}
	}
	return outsideKind
}

// literalKind is what a literal is: outside, holding what it names of the repository, or of any
// value whose kind is not pure, as in [Store()].
func (w *pyWalk) literalKind(v *node) vkind {
	out := outsideKind
	walk(v, func(x *node) bool {
		switch x.Kind() {
		case "lambda", "list_comprehension", "dictionary_comprehension", "set_comprehension", "generator_expression":
			return false
		case "identifier", "call", "attribute", "subscript":
			if !w.kindOf(x).pure() {
				out.holds = true
			}
			return false
		}
		return !out.holds
	})
	return out
}

// nameKind is what a name holds: an object a fixture or a class gives, a local's kind, a variable
// imported from the repository by its declaration, or a name from outside. Anything else is
// unknown.
func (w *pyWalk) nameKind(name string) vkind {
	if c := w.vars[name]; c != "" {
		return repoKind(c)
	}
	if w.locals[name] {
		return w.kinds[name]
	}
	if from, ok := w.pf.r.kinds.importsOf(w.pf.f.path)[name]; ok && from[0] == "" {
		return outsideKind
	}
	return w.pf.r.kinds.importedValue(w.pf.f.path, name)
}

// bindLocals makes the names a target declares locals of the test holding what they take from a
// value of kind k: the name itself k, and the names of a tuple or list pattern its elements.
func (w *pyWalk) bindLocals(target *node, k vkind) {
	f := w.pf.f
	switch target.Kind() {
	case "identifier":
		name := f.text(target)
		w.locals[name] = true
		delete(w.vars, name) // reassigning a fixture's name drops what the fixture gave
		w.kinds[name] = k
		if k.repo != "" {
			w.vars[name] = k.repo
		}
	case "pattern_list", "tuple_pattern", "list_pattern":
		for _, c := range named(target) {
			if c.Kind() == "list_splat_pattern" {
				w.bindLocals(c.NamedChild(0), k)
			} else {
				w.bindLocals(c, k.element())
			}
		}
	}
}

// typeKind is what a value of a type written in the walker's file is.
func (w *pyWalk) typeKind(t *node) vkind {
	p := w.pf.f.path
	return pyTypeKind(w.pf.f, t, func(name string) vkind { return w.pf.r.kinds.typeName(p, name) })
}

// importedMember names what obj.attr() calls when obj is a name imported from the repository whose
// target is owner: a method of the class of the variable obj, by its declaration; nothing for one of
// an outside value, which reads obj; else a member of obj.
func (w *pyWalk) importedMember(owner, obj, attr string) string {
	switch k := w.pf.r.kinds.importedValue(w.pf.f.path, obj); {
	case k.repo != "":
		return k.repo + "." + attr
	case k.outside:
		w.t.test.Reads++ // FRAMES.count(…) of FRAMES: list[str] = […] reads FRAMES
		return ""
	}
	return owner + "." + attr
}
