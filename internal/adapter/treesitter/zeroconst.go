//go:build cgo

package treesitter

import (
	"slices"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

// Returning the zero value in place of a constant whose value is already the zero value changes
// nothing, so no test could catch that mutant. These read, from syntax only and from one file, the
// names whose declaration hadome can read as a literal (docs/design/adapters-treesitter.md, Mutants).

// consts maps a name declared outside functions to the text of its literal value, or to "" when
// the name is declared in a form not read: reassignable, without a value, or twice with values that
// disagree.
type consts map[string]string

func (c consts) add(name, value string) {
	if old, ok := c[name]; ok && old != value {
		value = ""
	}
	c[name] = value
}

// unread records every identifier under n as declared in a form not read.
func (c consts) unread(f *file, n *sitter.Node) {
	for _, name := range identifiers(f, n) {
		c.add(name, "")
	}
}

// identifiers lists the names of the identifiers under n, type names left out.
func identifiers(f *file, n *sitter.Node) []string {
	var out []string
	walk(n, func(c *sitter.Node) bool {
		if k := c.Kind(); strings.Contains(k, "identifier") && !strings.Contains(k, "type") {
			out = append(out, f.text(c))
		}
		return true
	})
	return out
}

// zeroValue reports whether the text of a returned value is the function's zero value, written as
// a literal in any spelling: 0.0 and 0x0 for 0, single quotes for "". An integer zero with a minus in front is
// zero too, except where every number is a float and -0 differs from 0.
func (m *mutator) zeroValue(text string) bool {
	if text == m.zero || zeroLiteral(text, m.zero, m.b.strictNumbers) {
		return true
	}
	rest, negative := strings.CutPrefix(text, "-")
	if !negative || m.b.floatsOnly {
		return false
	}
	rest = strings.TrimSpace(rest)
	_, float := zeroNumber(rest)
	return !float && zeroLiteral(rest, m.zero, m.b.strictNumbers)
}

// zeroConstant reports whether v names a constant of the file whose value is the function's zero
// value, and that the function does not declare a name of its own that hides it.
func (m *mutator) zeroConstant(v *sitter.Node) bool {
	if v.Kind() != "identifier" { // every grammar's plain name, Kotlin's too
		return false
	}
	name := m.f.text(v)
	value, ok := m.consts[name]
	if !ok || !zeroLiteral(value, m.zero, m.b.strictNumbers) {
		return false
	}
	for n := m.decl; n != nil; n = n.Parent() {
		if (n == m.decl || m.b.functions[n.Kind()]) && m.binds(n, name) {
			return false
		}
	}
	return true
}

// binds reports whether n declares name anywhere inside it: as a parameter, a local, a loop
// variable, a pattern.
func (m *mutator) binds(n *sitter.Node, name string) bool {
	found := false
	walk(n, func(c *sitter.Node) bool {
		field, ok := m.b.binders[c.Kind()]
		if !ok || found {
			return !found
		}
		target := c
		if field != "" {
			target = c.ChildByFieldName(field)
		}
		found = target != nil && slices.Contains(identifiers(m.f, target), name)
		return !found
	})
	return found
}

var emptyStrings = set(`""`, `''`, "``", `""""""`, `''''''`)

// zeroLiteral reports whether the text of a literal is the zero value zero. A number is zero
// however it is written; strict tells integers from floats, for a language where a test sees the
// difference.
func zeroLiteral(value, zero string, strict bool) bool {
	switch zero {
	case "0", "0L", "0.0", "0f":
		isZero, float := zeroNumber(value)
		return isZero && (!strict || float == (zero == "0.0"))
	case `""`:
		return emptyStrings[value]
	case "":
		return false
	}
	return value == zero // false, False, null, undefined, None
}

var numberSuffixes = set("", "l", "f", "d", "i8", "i16", "i32", "i64", "i128", "isize", "u8", "u16", "u32", "u64",
	"u128", "usize", "f32", "f64")

// zeroNumber reports whether a numeric literal's value is zero, and whether it is a float.
func zeroNumber(t string) (isZero, float bool) {
	s := strings.ToLower(strings.ReplaceAll(t, "_", ""))
	digits, rest := "0123456789.", s
	if len(s) > 2 && s[0] == '0' && strings.ContainsRune("xbo", rune(s[1])) {
		digits, rest = "0123456789abcdef", s[2:]
	}
	i := strings.IndexFunc(rest, func(r rune) bool { return !strings.ContainsRune(digits, r) })
	if i < 0 {
		i = len(rest)
	}
	mantissa, suffix := rest[:i], rest[i:]
	if strings.Trim(mantissa, "0.") != "" || !strings.Contains(mantissa, "0") {
		return false, false
	}
	float = strings.Contains(mantissa, ".")
	if digits != "0123456789abcdef" && strings.HasPrefix(suffix, "e") {
		var ok bool
		if suffix, ok = afterExponent(suffix); !ok {
			return false, false
		}
		float = true
	}
	if !numberSuffixes[suffix] {
		return false, false
	}
	return true, float || floatSuffixes[suffix]
}

var floatSuffixes = set("f", "d", "f32", "f64")

// afterExponent returns what follows the exponent that starts a suffix, such as "f64" after
// "e-3f64", and false when the exponent has no digits.
func afterExponent(suffix string) (string, bool) {
	exp := strings.TrimLeft(suffix[1:], "+-")
	j := strings.IndexFunc(exp, func(r rune) bool { return r < '0' || r > '9' })
	if j < 0 {
		j = len(exp)
	}
	return exp[j:], j > 0
}

// valueAfterEquals returns the named child after a declaration's "=", for grammars without a field.
func valueAfterEquals(n *sitter.Node) *sitter.Node {
	seen := false
	for i := uint(0); i < n.ChildCount(); i++ {
		c := n.Child(i)
		if seen && c.IsNamed() {
			return c
		}
		seen = seen || (!c.IsNamed() && c.Kind() == "=")
	}
	return nil
}

func textOr(f *file, n *sitter.Node) string {
	if n == nil {
		return ""
	}
	return f.text(n)
}

func tsConstants(f *file) consts {
	out := consts{}
	for _, n := range named(f.root) {
		if n.Kind() == "export_statement" && n.ChildByFieldName("declaration") != nil {
			n = n.ChildByFieldName("declaration")
		}
		if n.Kind() != "lexical_declaration" && n.Kind() != "variable_declaration" {
			continue
		}
		constant := n.Child(0) != nil && f.text(n.Child(0)) == "const"
		for _, d := range named(n) {
			name := d.ChildByFieldName("name")
			switch {
			case d.Kind() != "variable_declarator" || name == nil:
			case constant && name.Kind() == "identifier":
				out.add(f.text(name), textOr(f, d.ChildByFieldName("value")))
			default:
				out.unread(f, name)
			}
		}
	}
	return out
}

func pyConstants(f *file) consts {
	out := consts{}
	walk(f.root, func(n *sitter.Node) bool {
		switch n.Kind() {
		case "function_definition", "class_definition":
			out.add(f.field(n, "name"), "")
			return false
		case "lambda":
			return false
		case "assignment":
			left := n.ChildByFieldName("left")
			if left != nil && left.Kind() == "identifier" {
				out.add(f.text(left), textOr(f, n.ChildByFieldName("right")))
			} else {
				out.unread(f, left)
			}
			return true
		}
		if field, ok := pyBinders[n.Kind()]; ok {
			if field != "" {
				out.unread(f, n.ChildByFieldName(field))
			} else {
				out.unread(f, n)
			}
		}
		return true
	})
	// A function that declares a name global can reassign it.
	walk(f.root, func(n *sitter.Node) bool {
		if n.Kind() == "global_statement" {
			out.unread(f, n)
		}
		return true
	})
	return out
}

func rustConstants(f *file) consts {
	out := consts{}
	walk(f.root, func(n *sitter.Node) bool {
		switch n.Kind() {
		case "function_item", "impl_item", "trait_item":
			return false
		case "use_declaration":
			out.unread(f, n)
			return false
		case "const_item", "static_item":
			value := textOr(f, n.ChildByFieldName("value"))
			for _, c := range named(n) {
				if c.Kind() == "mutable_specifier" {
					value = ""
				}
			}
			out.add(f.field(n, "name"), value)
			return false
		}
		return true
	})
	return out
}

func javaConstants(f *file) consts {
	out := consts{}
	walk(f.root, func(n *sitter.Node) bool {
		switch n.Kind() {
		case "method_declaration", "constructor_declaration", "lambda_expression", "block", "static_initializer":
			return false
		case "field_declaration", "constant_declaration":
			final := n.Kind() == "constant_declaration"
			for _, c := range named(n) {
				if c.Kind() == "modifiers" {
					for i := uint(0); i < c.ChildCount(); i++ {
						final = final || f.text(c.Child(i)) == "final"
					}
				}
			}
			for _, d := range named(n) {
				if d.Kind() != "variable_declarator" {
					continue
				}
				value := ""
				if final {
					value = textOr(f, d.ChildByFieldName("value"))
				}
				out.add(f.field(d, "name"), value)
			}
			return false
		case "formal_parameter", "enum_constant": // a record's components, an enum's constants
			out.add(f.field(n, "name"), "")
			return false
		}
		return true
	})
	return out
}

func ktConstants(f *file) consts {
	out := consts{}
	walk(f.root, func(n *sitter.Node) bool {
		switch n.Kind() {
		case "function_body", "lambda_literal", "anonymous_function", "getter", "setter", "anonymous_initializer":
			return false
		case "class_parameter", "enum_entry":
			if len(named(n)) > 0 {
				out.unread(f, named(n)[0])
			}
			return false
		case "property_declaration":
			ktProperty(f, n, out)
			return false
		}
		return true
	})
	return out
}

// ktProperty records a property: its literal value when it is a val nothing can override or
// compute, and as not read otherwise.
func ktProperty(f *file, n *sitter.Node, out consts) {
	value := ""
	if ktReadable(f, n) {
		value = textOr(f, valueAfterEquals(n))
	}
	for _, c := range named(n) {
		switch c.Kind() {
		case "variable_declaration":
			if name := identifiers(f, c); len(name) > 0 {
				out.add(name[0], value)
			}
		case "multi_variable_declaration":
			out.unread(f, c)
		}
	}
}

// ktReadable reports a val without open, abstract or override, and without a getter, a setter or
// a delegate.
func ktReadable(f *file, n *sitter.Node) bool {
	val := false
	for i := uint(0); i < n.ChildCount(); i++ {
		c := n.Child(i)
		switch {
		case !c.IsNamed() && c.Kind() == "val":
			val = true
		case c.Kind() == "modifiers":
			words := strings.Fields(f.text(c))
			if slices.Contains(words, "open") || slices.Contains(words, "abstract") || slices.Contains(words, "override") {
				return false
			}
		case c.Kind() == "getter" || c.Kind() == "setter" || c.Kind() == "property_delegate":
			return false
		}
	}
	return val
}
