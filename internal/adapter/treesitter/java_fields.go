// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

//go:build cgo

package treesitter

// How a Java test class types its fields, so that calls through them reach the product.

// javaField reads a field of a test class: a mock, typed only when it is a spy, or a field typed
// into fields and noted when its initializer uses the product.
func (jf *jvmFile) javaField(m *node, fields map[string]string) {
	anns := annotations(jf.f, m)
	mock, spy := hasAny(anns, jvmMockAnnotations), hasAny(anns, jvmSpyAnnotations)
	if mock {
		jf.mocks = append(jf.mocks, m)
	}
	if !mock || spy {
		jf.javaFieldTypes(m, fields)
	}
	if mock {
		return
	}
	for _, d := range named(m) {
		if d.Kind() == "variable_declarator" {
			jf.noteValueField(jf.f.field(d, "name"), d.ChildByFieldName("value"))
		}
	}
}

// javaFieldTypes types the fields of a test class, as ktFieldType does: by the constructor that
// builds a field, or, with no initializer (injected), by its declared type. A field built some other
// way (mock(), a factory) is left untyped.
func (jf *jvmFile) javaFieldTypes(n *node, into map[string]string) {
	f := jf.f
	declared := typeName(f.field(n, "type"))
	for _, d := range named(n) {
		if d.Kind() != "variable_declarator" {
			continue
		}
		name, v := f.field(d, "name"), d.ChildByFieldName("value")
		switch {
		case v == nil:
			if class := jf.names[declared]; class != "" {
				into[name] = class
			}
		default:
			if class := jf.javaBuilt(v); class != "" {
				into[name] = class
			}
		}
	}
}

// javaBuilt is the product class of the object an expression builds with new, or "".
func (jf *jvmFile) javaBuilt(v *node) string {
	if v.Kind() != "object_creation_expression" {
		return ""
	}
	return jf.names[typeName(jf.f.field(v, "type"))]
}

// javaSetupAssigns retypes the fields a setup assigns (store = mock(Store.class) in a @BeforeEach) as
// javaFieldTypes types a field with the assigned value as its initializer, so that a field assigned a
// mock before each test is a mock, not its declared type. The last assignment wins. Assignments
// inside lambdas and anonymous classes, and to the setup's own locals, are not the field's.
func (jf *jvmFile) javaSetupAssigns(body *node, fields map[string]string) {
	f := jf.f
	locals := map[string]bool{}
	walk(body, func(n *node) bool {
		switch n.Kind() {
		case "lambda_expression", "class_body":
			return false
		case "local_variable_declaration":
			for _, d := range named(n) {
				if d.Kind() == "variable_declarator" {
					locals[f.field(d, "name")] = true
				}
			}
		case "assignment_expression":
			target, value := n.ChildByFieldName("left"), n.ChildByFieldName("right")
			if f.field(n, "operator") != "=" || target == nil || value == nil {
				break
			}
			if target.Kind() == "field_access" && f.field(target, "object") == "this" {
				target = target.ChildByFieldName("field")
			} else if target.Kind() != "identifier" || locals[f.text(target)] {
				break
			}
			retype(fields, f.text(target), jf.javaBuilt(value))
		}
		return true
	})
}
