// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

//go:build cgo

package treesitter

import "strings"

// The code/ grammars of Java, Kotlin and Rust.

var javaNumbers = set("decimal_integer_literal", "hex_integer_literal", "octal_integer_literal", "binary_integer_literal",
	"decimal_floating_point_literal")

// javaDurations are the factories of java.time.Duration that take a number.
var javaDurations = set("of", "ofNanos", "ofMillis", "ofSeconds", "ofMinutes", "ofHours", "ofDays")

// portClasses are JVM classes whose constructor takes a port.
var portClasses = set("ServerSocket", "InetSocketAddress", "DatagramSocket")

var javaCodeLang = &codeLang{
	text:    javaText,
	numbers: javaNumbers,
	idents:  set("identifier"),
	declares: func(f *file, n *node) []*node {
		switch n.Kind() {
		case "formal_parameter", "variable_declarator", "enhanced_for_statement", "catch_formal_parameter", "resource":
			if name := n.ChildByFieldName("name"); name != nil {
				return []*node{name}
			}
		case "spread_parameter":
			for _, c := range named(n) {
				if c.Kind() == "variable_declarator" {
					return []*node{c.ChildByFieldName("name")}
				}
			}
		case "lambda_expression":
			p := n.ChildByFieldName("parameters")
			if p != nil && p.Kind() == "identifier" {
				return []*node{p}
			}
			if p != nil && p.Kind() == "inferred_parameters" {
				return named(p)
			}
		}
		return nil
	},
	member: func(n, p *node) bool {
		return p.Kind() == "field_access" && fieldIs(p, "field", n) || p.Kind() == "method_invocation" && fieldIs(p, "name", n)
	},
	swallows: func(f *file, n *node) bool {
		if n.Kind() != "catch_clause" {
			return false
		}
		for _, c := range named(n) {
			if c.Kind() == "catch_formal_parameter" && ignoring(f.field(c, "name")) {
				return false
			}
		}
		return emptyBlock(n.ChildByFieldName("body"))
	},
	pair: func(f *file, n *node) (string, *node, bool) {
		if n.Kind() != "assignment_expression" {
			return "", nil, false
		}
		left := n.ChildByFieldName("left")
		if left == nil || left.Kind() != "field_access" {
			return "", nil, false
		}
		return f.field(left, "field"), n.ChildByFieldName("right"), true
	},
	duration: func(f *file, n *node) bool {
		if n.Kind() != "method_invocation" || !numbersOnly(javaNumbers, n.ChildByFieldName("arguments")) {
			return false
		}
		object, name := f.field(n, "object"), f.field(n, "name") // java.time.Duration as well as Duration
		return className(object) == "Duration" && javaDurations[name] ||
			strings.Contains("."+object, ".TimeUnit.") && strings.HasPrefix(name, "to")
	},
	port: func(f *file, n *node) *node {
		class := className(f.field(n, "type"))
		if n.Kind() != "object_creation_expression" || !portClasses[class] {
			return nil
		}
		return portArgument(javaNumbers, class, n.ChildByFieldName("arguments"))
	},
}

var ktNumbers = set("number_literal", "integer_literal", "long_literal", "real_literal", "float_literal", "hex_literal",
	"bin_literal", "unsigned_literal")

// ktDurationUnits are kotlin.time's extension properties that make a duration of a number.
var ktDurationUnits = set("nanoseconds", "microseconds", "milliseconds", "seconds", "minutes", "hours", "days")

// ktCallee returns what a Kotlin call calls, as written, and its value arguments.
func ktCallee(f *file, n *node) (string, *node) {
	if n.Kind() != "call_expression" || n.NamedChildCount() < 2 {
		return "", nil
	}
	for _, c := range named(n) {
		if c.Kind() == "value_arguments" {
			return f.text(n.NamedChild(0)), c
		}
	}
	return "", nil
}

// ktFirstIdent returns the first identifier among n's children: the name a parameter, a variable
// or a caught exception declares.
func ktFirstIdent(n *node) []*node {
	for _, c := range named(n) {
		if c.Kind() == "identifier" {
			return []*node{c}
		}
	}
	return nil
}

// ktNamedArgument reports a value argument given by name, name = value.
func ktNamedArgument(n *node) bool {
	if n.Kind() != "value_argument" || n.NamedChildCount() != 2 {
		return false
	}
	next := n.NamedChild(0).NextSibling()
	return next != nil && next.Kind() == "="
}

var ktCodeLang = &codeLang{
	text:    ktText,
	numbers: ktNumbers,
	idents:  set("identifier", "simple_identifier"),
	declares: func(f *file, n *node) []*node {
		switch n.Kind() {
		case "parameter", "variable_declaration", "class_parameter", "catch_block":
			return ktFirstIdent(n)
		}
		return nil
	},
	member: func(n, p *node) bool {
		if p.Kind() == "navigation_expression" || p.Kind() == "navigation_suffix" {
			return p.NamedChild(0).Id() != n.Id()
		}
		return ktNamedArgument(p) && p.NamedChild(0).Id() == n.Id()
	},
	swallows: func(f *file, n *node) bool {
		if n.Kind() != "catch_block" {
			return false
		}
		if name := ktFirstIdent(n); len(name) == 1 && ignoring(f.text(name[0])) {
			return false
		}
		for _, c := range named(n) {
			if c.Kind() == "block" {
				return emptyBlock(c)
			}
		}
		return false
	},
	pair: func(f *file, n *node) (string, *node, bool) {
		if !ktNamedArgument(n) {
			return "", nil, false
		}
		return f.text(n.NamedChild(0)), n.NamedChild(1), true
	},
	duration: func(f *file, n *node) bool {
		if n.Kind() == "navigation_expression" && n.NamedChildCount() == 2 && ktNumbers[n.NamedChild(0).Kind()] {
			return ktDurationUnits[strings.TrimPrefix(f.text(n.NamedChild(1)), ".")]
		}
		callee, args := ktCallee(f, n)
		i := strings.LastIndex(callee, ".")
		return i >= 0 && className(callee[:i]) == "Duration" && javaDurations[callee[i+1:]] && numbersOnly(ktNumbers, args)
	},
	port: func(f *file, n *node) *node {
		callee, args := ktCallee(f, n)
		if !portClasses[className(callee)] {
			return nil
		}
		return portArgument(ktNumbers, className(callee), args)
	},
}

var rustIdents = set("identifier")

// rustPatternSkip leaves out the parts of a pattern that name no variable: the path of a tuple
// struct or a struct (Some in Some(x)), a type.
func rustPatternSkip(x *node) bool {
	p := x.Parent()
	return p != nil && (p.Kind() == "tuple_struct_pattern" || p.Kind() == "struct_pattern") && fieldIs(p, "type", x) ||
		x.Kind() == "scoped_identifier" || strings.HasSuffix(x.Kind(), "_type")
}

var rustCodeLang = &codeLang{
	text:    rustText,
	numbers: rustNumbers,
	idents:  rustIdents,
	declares: func(f *file, n *node) []*node {
		switch n.Kind() {
		case "parameter", "let_declaration", "for_expression", "let_condition":
			return declaredIn(n.ChildByFieldName("pattern"), rustIdents, rustPatternSkip)
		case "closure_parameters":
			return declaredIn(n, rustIdents, rustPatternSkip)
		}
		return nil
	},
	// Fields are field_identifier; a shorthand initializer (S { x }) names the field with the variable,
	// so its name stays.
	member: func(n, p *node) bool { return p.Kind() == "shorthand_field_initializer" },
	swallows: func(f *file, n *node) bool {
		if n.Kind() != "expression_statement" || n.NamedChildCount() == 0 {
			return false
		}
		call := n.NamedChild(0)
		if call.Kind() != "call_expression" {
			return false
		}
		fn := call.ChildByFieldName("function")
		return fn != nil && fn.Kind() == "field_expression" && f.field(fn, "field") == "ok" &&
			len(arguments(call.ChildByFieldName("arguments"))) == 0
	},
	pair: func(f *file, n *node) (string, *node, bool) {
		if n.Kind() != "field_initializer" {
			return "", nil, false
		}
		return f.field(n, "field"), n.ChildByFieldName("value"), true
	},
	duration: func(f *file, n *node) bool {
		if n.Kind() != "call_expression" {
			return false
		}
		callee := f.field(n, "function")
		return strings.Contains(callee, "Duration::from_") && numbersOnly(rustNumbers, n.ChildByFieldName("arguments"))
	},
	constants: set("const_item", "static_item"),
}
