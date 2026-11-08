//go:build cgo

package treesitter

import (
	"slices"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

// Each language's spelling of the five breaks (docs/design/adapters-treesitter.md, Mutants).

var lowerBools = map[string]string{"true": "false", "false": "true"}

// statementOf returns a matcher of statements of a kind that hold nothing but a call.
func statementOf(statement string, calls ...string) func(f *file, n *sitter.Node) bool {
	return func(_ *file, n *sitter.Node) bool { return n.Kind() == statement && only(n, calls...) }
}

var tsBreaks = &breaks{
	functions: set("function_declaration", "generator_function_declaration", "method_definition", "arrow_function",
		"function_expression", "generator_function"),
	nested:     set("class_declaration", "class"),
	binary:     set("binary_expression"),
	strings:    set("string", "template_string"),
	not:        set("unary_expression"),
	bools:      set("true", "false"),
	literals:   lowerBools,
	returns:    set("return_statement"),
	call:       statementOf("expression_statement", "call_expression"),
	removed:    ";",
	body:       bodyField,
	zero:       tsZero,
	constants:  tsConstants,
	floatsOnly: true,
	binders: map[string]string{"variable_declarator": "name", "required_parameter": "pattern",
		"optional_parameter": "pattern", "arrow_function": "parameter", "catch_clause": "parameter",
		"for_in_statement": "left", "function_declaration": "name", "class_declaration": "name"},
}

func tsZero(f *file, fn *sitter.Node) string {
	t := fn.ChildByFieldName("return_type")
	if t == nil {
		return ""
	}
	typ := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(f.text(t)), ":"))
	switch typ {
	case "number":
		return "0"
	case "string":
		return `""`
	case "boolean":
		return "false"
	}
	parts := strings.Split(typ, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	for _, v := range []string{"null", "undefined"} {
		if len(parts) > 1 && slices.Contains(parts, v) {
			return v
		}
	}
	return ""
}

var pyBreaks = &breaks{
	functions:     set("function_definition"),
	nested:        set("lambda", "class_definition"),
	binary:        set("comparison_operator", "binary_operator"),
	strings:       set("string", "concatenated_string"),
	not:           set("not_operator"),
	bools:         set("true", "false"),
	literals:      map[string]string{"True": "False", "False": "True"},
	returns:       set("return_statement"),
	call:          statementOf("expression_statement", "call"),
	removed:       "pass",
	body:          bodyField,
	zero:          pyZero,
	constants:     pyConstants,
	binders:       pyBinders,
	strictNumbers: true,
}

var pyBinders = map[string]string{"parameters": "", "assignment": "left", "augmented_assignment": "left",
	"for_statement": "left", "as_pattern_target": "", "named_expression": "name", "global_statement": "",
	"nonlocal_statement": "", "import_statement": "", "import_from_statement": "", "function_definition": "name",
	"class_definition": "name"}

func pyZero(f *file, fn *sitter.Node) string {
	t := fn.ChildByFieldName("return_type")
	if t == nil {
		return ""
	}
	typ := strings.Join(strings.Fields(f.text(t)), " ")
	switch typ {
	case "int":
		return "0"
	case "float":
		return "0.0"
	case "str":
		return `""`
	case "bool":
		return "False"
	}
	if strings.HasPrefix(typ, "Optional[") || strings.HasSuffix(typ, "| None") || strings.HasPrefix(typ, "None |") {
		return "None"
	}
	return ""
}

var rustBreaks = &breaks{
	functions: set("function_item"),
	nested:    set("closure_expression"),
	binary:    set("binary_expression"),
	not:       set("unary_expression"),
	bools:     set("boolean_literal"),
	literals:  lowerBools,
	returns:   set("return_expression"),
	call:      statementOf("expression_statement", "call_expression"),
	removed:   ";",
	tail:      rustTail,
	body:      bodyField,
	zero:      rustZero,
	constants: rustConstants,
	binders: map[string]string{"let_declaration": "pattern", "parameter": "pattern", "for_expression": "pattern",
		"match_arm": "pattern", "let_condition": "pattern", "closure_parameters": "", "const_item": "name",
		"static_item": "name", "function_item": "name"},
}

// rustTail returns the last expression of a block, the value it gives without a return.
func rustTail(body *sitter.Node) *sitter.Node {
	var last *sitter.Node
	for _, c := range named(body) {
		if !strings.Contains(c.Kind(), "comment") {
			last = c
		}
	}
	if last == nil || last.Kind() == "expression_statement" || last.Kind() == "let_declaration" ||
		last.Kind() == "empty_statement" || strings.HasSuffix(last.Kind(), "_item") {
		return nil
	}
	return last
}

var rustIntegers = set("i8", "i16", "i32", "i64", "i128", "isize", "u8", "u16", "u32", "u64", "u128", "usize")

func rustZero(f *file, fn *sitter.Node) string {
	t := fn.ChildByFieldName("return_type")
	if t == nil {
		return ""
	}
	typ := f.text(t)
	switch {
	case rustIntegers[typ]:
		return "0"
	case typ == "f32" || typ == "f64":
		return "0.0"
	case typ == "bool":
		return "false"
	case typ == "String":
		return "String::new()"
	case t.Kind() == "reference_type" && strings.HasSuffix(typ, "str") && !strings.Contains(typ, "mut"):
		return `""`
	case t.Kind() == "generic_type" && f.field(t, "type") == "Option":
		return "None"
	case t.Kind() == "generic_type" && f.field(t, "type") == "Vec":
		return "Vec::new()"
	}
	return ""
}

var javaBreaks = &breaks{
	functions: set("method_declaration", "constructor_declaration"),
	nested:    set("lambda_expression", "class_body"),
	binary:    set("binary_expression"),
	strings:   set("string_literal"),
	not:       set("unary_expression"),
	bools:     set("true", "false"),
	literals:  lowerBools,
	returns:   set("return_statement"),
	call:      statementOf("expression_statement", "method_invocation"),
	removed:   ";",
	body:      bodyField,
	zero:      javaZero,
	constants: javaConstants,
	binders: map[string]string{"variable_declarator": "name", "formal_parameter": "name",
		"catch_formal_parameter": "name", "enhanced_for_statement": "name", "inferred_parameters": "",
		"lambda_expression": "parameters", "resource": "name"},
}

func javaZero(f *file, fn *sitter.Node) string {
	t := fn.ChildByFieldName("type")
	if t == nil {
		return ""
	}
	switch t.Kind() {
	case "integral_type":
		if f.text(t) == "char" {
			return ""
		}
		return "0"
	case "floating_point_type":
		return "0"
	case "boolean_type":
		return "false"
	case "void_type":
		return ""
	}
	if f.text(t) == "String" {
		return `""`
	}
	return "null"
}

var ktBreaks = &breaks{
	functions: set("function_declaration"),
	nested:    set("lambda_literal", "anonymous_function", "object_literal", "class_declaration"),
	binary:    set("binary_expression"),
	strings:   set("string_literal"),
	not:       set("unary_expression"),
	bools:     set("identifier"), // true and false are not keywords to this grammar
	literals:  lowerBools,
	returns:   set("return_expression"),
	call: func(_ *file, n *sitter.Node) bool {
		return n.Kind() == "call_expression" && n.Parent() != nil && n.Parent().Kind() == "block"
	},
	removed: "Unit",
	body: func(fn *sitter.Node) *sitter.Node {
		for _, c := range named(fn) {
			if c.Kind() == "function_body" {
				return c
			}
		}
		return nil
	},
	zero:      ktZero,
	constants: ktConstants,
	binders: map[string]string{"variable_declaration": "", "function_value_parameters": "",
		"lambda_parameters": ""},
}

var ktZeros = map[string]string{"Int": "0", "Short": "0", "Byte": "0", "Long": "0L", "Double": "0.0", "Float": "0f",
	"Boolean": "false", "String": `""`}

func ktZero(f *file, fn *sitter.Node) string {
	var typ, body *sitter.Node
	params := false
	for _, c := range named(fn) {
		switch {
		case c.Kind() == "function_value_parameters":
			params = true
		case params && typ == nil && (c.Kind() == "user_type" || c.Kind() == "nullable_type"):
			typ = c
		case c.Kind() == "function_body":
			body = c
		}
	}
	if typ == nil || body == nil || body.Child(0) == nil || f.text(body.Child(0)) == "=" {
		return "" // an expression body has no return to break
	}
	if typ.Kind() == "nullable_type" {
		return "null"
	}
	return ktZeros[f.text(typ)]
}
