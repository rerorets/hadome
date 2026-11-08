//go:build cgo

package treesitter

import (
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// What a Kotlin test class inherits, and the shapes of Kotlin code that build or call product
// objects in ways the plain reading of a call misses.

// ktCollect reads the members of a class into scope (its fields, helpers and setup) and returns its
// tests and its mock fields.
func (jf *jvmFile) ktCollect(n *sitter.Node, scope *jvmScope) (tests, mocks []*sitter.Node) {
	f := jf.f
	for name, class := range jf.ktConstructorFields(n) {
		scope.fields[name] = class
	}
	for _, m := range ktClassMembers(n) {
		switch m.Kind() {
		case "property_declaration":
			anns := ktAnnotations(f, m)
			mock, spy := hasAny(anns, jvmMockAnnotations), hasAny(anns, jvmSpyAnnotations)
			if mock {
				mocks = append(mocks, m)
			}
			if !mock || spy {
				if name, class := jf.ktFieldType(m); class != "" {
					scope.fields[name] = class
				}
			}
			if !mock {
				if name, uses := jf.ktNoteValueField(m); name != "" {
					scope.values[name] = uses
				}
			}
		case "function_declaration":
			anns := ktAnnotations(f, m)
			if hasAny(anns, jvmTestAnnotations) {
				tests = append(tests, m)
				continue
			}
			jf.helpers[f.field(m, "name")] = ktBody(m)
			if hasAny(anns, jvmSetupAnnotations) {
				scope.setups = append(scope.setups, jvmCode{jf, ktBody(m)})
			}
		case "anonymous_initializer": // init { … } runs as each test's instance is built
			scope.setups = append(scope.setups, jvmCode{jf, m})
		}
	}
	return tests, mocks
}

// ktInherit adds to scope what a test class inherits from the test class it extends, which may be
// declared in another test file: its fields, helpers, mock fields and setup, and those of the class
// that one extends in turn.
func (jf *jvmFile) ktInherit(n *sitter.Node, scope *jvmScope, depth int) {
	super := ktSuperclass(jf.f, n)
	if super == "" || jf.kt == nil || depth > 8 {
		return
	}
	d, ok := jf.j.testType(jf.f.unit, super)
	if !ok || (d.f == jf.f && d.n.Id() == n.Id()) {
		return
	}
	sjf := jf
	if d.f != jf.f {
		sjf = jf.kt.testFile(d.f, jf.t)
	}
	sjf.ktInherit(d.n, scope, depth+1)
	_, mocks := sjf.ktCollect(d.n, scope)
	for name, body := range sjf.helpers {
		scope.inherited[name] = jvmCode{sjf, body}
	}
	for name := range sjf.valueFields {
		if _, declared := scope.values[name]; !declared {
			scope.values[name] = true
		}
	}
	for _, m := range mocks {
		scope.mocks = append(scope.mocks, jvmCode{sjf, m})
	}
}

// ktSuperclass is the simple name of the class a class extends: the supertype it calls a
// constructor of (class StoreTest : StoreTestSupport()).
func ktSuperclass(f *file, n *sitter.Node) string {
	super := ""
	for _, c := range named(n) {
		if c.Kind() != "delegation_specifiers" {
			continue
		}
		walk(c, func(x *sitter.Node) bool {
			if super == "" && x.Kind() == "constructor_invocation" && x.NamedChildCount() > 0 {
				super = lastElem(typeName(f.text(x.NamedChild(0))))
			}
			return super == ""
		})
	}
	return super
}

// ktReadsProperty reports whether an identifier is a variable of a product class whose property
// the test reads (cache.ttlSeconds), not one whose method it calls (cache.get()).
func (w *jvmWalk) ktReadsProperty(n *sitter.Node) bool {
	p := n.Parent()
	if w.vars[w.jf.f.text(n)] == "" || p == nil || p.Kind() != "navigation_expression" || !isField(p, "", n) {
		return false
	}
	call := p.Parent()
	return call == nil || call.Kind() != "call_expression" || !isField(call, "", p)
}

// ktTypedCall recognises a call with type arguments and a lambda, which this grammar reads as two
// comparisons: assertThrows<E> { … } and assertThrows<E>({ … }) are (assertThrows < E) > { … }.
// It returns the callee and the lambda's expression, or nil.
func ktTypedCall(n *sitter.Node) (callee, arg *sitter.Node) {
	if n.NamedChildCount() != 2 || ktOperator(n) != ">" {
		return nil, nil
	}
	left, right := n.NamedChild(0), n.NamedChild(1)
	if left.Kind() != "binary_expression" || left.NamedChildCount() != 2 || ktOperator(left) != "<" {
		return nil, nil
	}
	lambda := right
	if right.Kind() == "parenthesized_expression" && right.NamedChildCount() == 1 {
		lambda = right.NamedChild(0)
	}
	callee = left.NamedChild(0)
	if lambda.Kind() != "lambda_literal" || (callee.Kind() != "identifier" && callee.Kind() != "navigation_expression") {
		return nil, nil
	}
	return callee, right
}

// ktOperator is the operator of a binary expression: its first unnamed child.
func ktOperator(n *sitter.Node) string {
	for i := uint(0); i < n.ChildCount(); i++ {
		if c := n.Child(i); !c.IsNamed() {
			return c.Kind()
		}
	}
	return ""
}

// ktBuilt is the product class of the object an expression builds: a constructor call (Store()),
// apply or also on one, which return their receiver (Store().apply { … }), or a constant of an enum
// class (Kind.DAILY).
func (jf *jvmFile) ktBuilt(e *sitter.Node) string {
	f := jf.f
	switch e.Kind() {
	case "call_expression":
		callee := e.NamedChild(0)
		if callee.Kind() != "navigation_expression" {
			return jf.names[f.text(callee)]
		}
		if callee.NamedChildCount() == 2 && ktReturnsReceiver[f.text(callee.NamedChild(1))] {
			return jf.ktBuilt(callee.NamedChild(0))
		}
	case "navigation_expression":
		if e.NamedChildCount() == 2 {
			if class := jf.names[f.text(e.NamedChild(0))]; jf.j.entries[class][f.text(e.NamedChild(1))] {
				return class
			}
		}
	}
	return ""
}

// ktClassMembers lists a class's members with those of its companion object, whose properties and
// functions the class's tests use by their bare names.
func ktClassMembers(n *sitter.Node) []*sitter.Node {
	var out []*sitter.Node
	for _, m := range ktMembers(n) {
		if m.Kind() == "companion_object" {
			out = append(out, ktMembers(m)...)
			continue
		}
		out = append(out, m)
	}
	return out
}
