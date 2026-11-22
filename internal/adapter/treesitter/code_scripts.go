// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

//go:build cgo

package treesitter

import "strings"

// The code/ grammars of TypeScript and JavaScript, and of Python.

var (
	tsNumbers   = set("number")
	pyNumbers   = set("integer", "float")
	rustNumbers = set("integer_literal", "float_literal")
)

var tsPatternIdents = set("identifier", "shorthand_property_identifier_pattern")

// tsPatternSkip leaves out what a pattern holds that declares nothing: a type, a default value, the
// key of a renamed property ({ a: b } declares b).
func tsPatternSkip(x *node) bool {
	p := x.Parent()
	return x.Kind() == "type_annotation" || p != nil && (p.Kind() == "assignment_pattern" && fieldIs(p, "right", x) ||
		p.Kind() == "pair_pattern" && fieldIs(p, "key", x))
}

var tsCodeLang = &codeLang{
	text:    tsText,
	numbers: tsNumbers,
	idents:  set("identifier"),
	declares: func(f *file, n *node) []*node {
		switch n.Kind() {
		case "required_parameter", "optional_parameter":
			return declaredIn(n.ChildByFieldName("pattern"), tsPatternIdents, tsPatternSkip)
		case "arrow_function":
			if p := n.ChildByFieldName("parameter"); p != nil {
				return []*node{p}
			}
		case "variable_declarator":
			return declaredIn(n.ChildByFieldName("name"), tsPatternIdents, tsPatternSkip)
		case "catch_clause":
			return declaredIn(n.ChildByFieldName("parameter"), tsPatternIdents, tsPatternSkip)
		case "for_in_statement":
			return declaredIn(n.ChildByFieldName("left"), tsPatternIdents, tsPatternSkip)
		}
		return nil
	},
	member: func(n, p *node) bool { return false }, // members and keys are property_identifier
	swallows: func(f *file, n *node) bool {
		switch n.Kind() {
		case "catch_clause":
			return !ignoring(f.field(n, "parameter")) && emptyBlock(n.ChildByFieldName("body"))
		case "call_expression": // promise.catch(() => {})
			fn := n.ChildByFieldName("function")
			if fn == nil || fn.Kind() != "member_expression" || f.field(fn, "property") != "catch" {
				return false
			}
			as := arguments(n.ChildByFieldName("arguments"))
			if len(as) != 1 || as[0].Kind() != "arrow_function" && as[0].Kind() != "function_expression" && as[0].Kind() != "function" {
				return false
			}
			body := as[0].ChildByFieldName("body")
			return body != nil && body.Kind() == "statement_block" && emptyBlock(body)
		}
		return false
	},
	pair: func(f *file, n *node) (string, *node, bool) {
		switch n.Kind() {
		case "pair":
			key := n.ChildByFieldName("key")
			if key == nil {
				return "", nil, false
			}
			return unquote(f.text(key)), n.ChildByFieldName("value"), true
		case "assignment_expression":
			left := n.ChildByFieldName("left")
			if left != nil && left.Kind() == "member_expression" {
				return f.field(left, "property"), n.ChildByFieldName("right"), true
			}
		}
		return "", nil, false
	},
	duration: func(f *file, n *node) bool {
		return n.Kind() == "call_expression" && f.field(n, "function") == "AbortSignal.timeout" &&
			numbersOnly(tsNumbers, n.ChildByFieldName("arguments"))
	},
	port: func(f *file, n *node) *node {
		if n.Kind() != "call_expression" {
			return nil
		}
		fn := n.ChildByFieldName("function")
		if fn == nil || fn.Kind() != "member_expression" || f.field(fn, "property") != "listen" {
			return nil
		}
		if as := arguments(n.ChildByFieldName("arguments")); len(as) > 0 && as[0].Kind() == "number" {
			return as[0]
		}
		return nil
	},
}

var pyIdents = set("identifier")

// pyParamNames returns the names a parameter list declares.
func pyParamNames(ps *node) []*node {
	var out []*node
	for _, p := range named(ps) {
		switch p.Kind() {
		case "identifier":
			out = append(out, p)
		case "typed_parameter", "list_splat_pattern", "dictionary_splat_pattern":
			for _, c := range named(p) {
				if c.Kind() == "identifier" {
					out = append(out, c)
					break
				}
			}
		case "default_parameter", "typed_default_parameter":
			if name := p.ChildByFieldName("name"); name != nil {
				out = append(out, name)
			}
		}
	}
	return out
}

// pyTargets returns the names an assignment's target binds: a name, or the names of a tuple or a
// list of them; an attribute or a subscript binds none.
func pyTargets(n *node) []*node {
	if n == nil {
		return nil
	}
	switch n.Kind() {
	case "identifier":
		return []*node{n}
	case "pattern_list", "tuple_pattern", "list_pattern", "list_splat_pattern", "parenthesized_expression", "tuple", "list":
		var out []*node
		for _, c := range named(n) {
			out = append(out, pyTargets(c)...)
		}
		return out
	}
	return nil
}

// pyBroad reports an except clause that catches every error: a bare except, or one naming Exception
// or BaseException. One that names a narrower error is how Python asks forgiveness rather than
// permission (except KeyError: pass): the author expected that error.
func pyBroad(f *file, n *node) bool {
	var caught *node
	for _, c := range named(n) {
		if c.Kind() != "block" && !strings.Contains(c.Kind(), "comment") {
			caught = c
			break
		}
	}
	if caught == nil {
		return true
	}
	if caught.Kind() == "as_pattern" {
		if a := caught.ChildByFieldName("alias"); a != nil && ignoring(strings.TrimSpace(f.text(a))) {
			return false
		}
		if caught.NamedChildCount() > 0 {
			caught = caught.NamedChild(0)
		}
	}
	for _, name := range strings.FieldsFunc(f.text(caught), func(r rune) bool { return strings.ContainsRune("(), \n", r) }) {
		if name == "Exception" || name == "BaseException" || name == "builtins.Exception" {
			return true
		}
	}
	return false
}

var pyCodeLang = &codeLang{
	text:    pyText,
	blocks:  set("block"),
	numbers: pyNumbers,
	idents:  pyIdents,
	declares: func(f *file, n *node) []*node {
		switch n.Kind() {
		case "parameters", "lambda_parameters":
			return pyParamNames(n)
		case "assignment", "augmented_assignment", "for_statement", "for_in_clause":
			return pyTargets(n.ChildByFieldName("left"))
		case "as_pattern":
			if a := n.ChildByFieldName("alias"); a != nil {
				return declaredIn(a, pyIdents, nil)
			}
		case "named_expression":
			if name := n.ChildByFieldName("name"); name != nil {
				return []*node{name}
			}
		}
		return nil
	},
	member: func(n, p *node) bool {
		return p.Kind() == "attribute" && fieldIs(p, "attribute", n) || p.Kind() == "keyword_argument" && fieldIs(p, "name", n)
	},
	swallows: func(f *file, n *node) bool {
		if n.Kind() != "except_clause" || !pyBroad(f, n) {
			return false
		}
		var block *node
		for _, c := range named(n) {
			if c.Kind() == "block" {
				block = c
			}
		}
		if block == nil {
			return false
		}
		for _, c := range named(block) {
			switch {
			case strings.Contains(c.Kind(), "comment"), c.Kind() == "pass_statement":
			case c.Kind() == "expression_statement" && c.NamedChildCount() == 1 && c.NamedChild(0).Kind() == "ellipsis":
			default:
				return false
			}
		}
		return true
	},
	pair: func(f *file, n *node) (string, *node, bool) {
		switch n.Kind() {
		case "keyword_argument":
			return f.field(n, "name"), n.ChildByFieldName("value"), true
		case "assignment":
			left := n.ChildByFieldName("left")
			if left != nil && left.Kind() == "attribute" {
				return f.field(left, "attribute"), n.ChildByFieldName("right"), true
			}
		}
		return "", nil, false
	},
	duration: func(f *file, n *node) bool {
		if n.Kind() != "call" {
			return false
		}
		callee := f.field(n, "function")
		if callee != "timedelta" && !strings.HasSuffix(callee, ".timedelta") {
			return false
		}
		as := arguments(n.ChildByFieldName("arguments"))
		for _, a := range as {
			if a.Kind() == "keyword_argument" {
				a = a.ChildByFieldName("value")
			}
			if a == nil || !pyNumbers[a.Kind()] {
				return false
			}
		}
		return len(as) > 0
	},
}
