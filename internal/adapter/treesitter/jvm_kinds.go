// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

//go:build cgo

package treesitter

import (
	"path"
	"strings"
)

// What a Java or Kotlin type or value is (vkind), as a file of the version writes it: the classes
// of the product, the fields they declare and Kotlin's top-level properties.

var (
	// jvmBuiltinTypes are the types of the languages and their standard libraries that need no import.
	jvmBuiltinTypes = set("String", "CharSequence", "StringBuilder", "Integer", "Long", "Short", "Byte", "Double",
		"Float", "Boolean", "Character", "Number", "Int", "UInt", "ULong", "Char", "Unit", "Nothing", "Regex",
		"List", "MutableList", "ArrayList", "LinkedList", "Set", "MutableSet", "HashSet", "LinkedHashSet", "TreeSet",
		"Map", "MutableMap", "HashMap", "LinkedHashMap", "TreeMap", "Collection", "Iterable", "Iterator", "Optional",
		"Stream", "Array", "Sequence", "Pair", "Triple", "IntArray", "LongArray", "ByteArray", "CharArray",
		"DoubleArray", "BooleanArray", "IntRange")
	// jvmElementTypes are the containers whose one type argument is what an index or a loop takes out.
	jvmElementTypes = set("List", "MutableList", "ArrayList", "LinkedList", "Set", "MutableSet", "HashSet",
		"LinkedHashSet", "TreeSet", "Collection", "Iterable", "Iterator", "Optional", "Stream", "Array", "Sequence")
	// ktBuilders are the standard library's functions that build a string or a collection.
	ktBuilders = set("buildString", "StringBuilder", "listOf", "mutableListOf", "arrayListOf", "listOfNotNull",
		"setOf", "mutableSetOf", "hashSetOf", "mapOf", "mutableMapOf", "hashMapOf", "linkedMapOf", "emptyList",
		"emptySet", "emptyMap", "buildList", "buildSet", "buildMap", "arrayOf")
	// jvmStd are the packages of the standard libraries.
	jvmStd = set("java", "javax", "kotlin", "kotlinx")
	// javaStatics are the standard library's classes whose static methods return outside values:
	// List.of("a"), String.valueOf(n).
	javaStatics = set("String", "List", "Map", "Set", "Arrays", "Collections", "Objects", "Math", "Integer", "Long",
		"Double", "Boolean", "Character", "Optional", "Stream", "IntStream", "Collectors", "Pattern")
	javaLiteralKinds = set("string_literal", "text_block", "decimal_integer_literal", "hex_integer_literal",
		"octal_integer_literal", "binary_integer_literal", "decimal_floating_point_literal", "true", "false",
		"character_literal", "null_literal", "binary_expression", "unary_expression", "array_initializer",
		"lambda_expression")
	ktLiteralKinds = set("string_literal", "multiline_string_literal", "number_literal", "integer_literal",
		"long_literal", "real_literal", "boolean_literal", "character_literal", "null_literal",
		"comparison_expression", "equality_expression", "conjunction_expression", "disjunction_expression",
		"lambda_literal")
	// ktOperations are the operations a class may define as operator functions (Money + Money).
	ktOperations = set("additive_expression", "multiplicative_expression", "prefix_expression", "range_expression")
)

// jvmTypeKind is the kind of a value of the written type t, Java's or Kotlin's. name gives the kind
// a type name, dotted or not, stands for.
func jvmTypeKind(f *file, t *node, name func(string) vkind) vkind {
	if t == nil {
		return vkind{}
	}
	switch t.Kind() {
	case "integral_type", "floating_point_type", "boolean_type", "void_type", "function_type":
		return outsideKind
	case "nullable_type", "type_projection", "parenthesized_type", "annotated_type":
		return jvmTypeKind(f, t.NamedChild(t.NamedChildCount()-1), name)
	case "type_identifier", "scoped_type_identifier":
		if f.text(t) == "var" {
			return vkind{}
		}
		return name(f.text(t))
	case "array_type":
		return containerOf(jvmTypeKind(f, t.ChildByFieldName("element"), name))
	case "generic_type", "user_type":
		var base []string
		var args []*node
		for _, c := range named(t) {
			switch c.Kind() {
			case "type_arguments":
				args = append(args, named(c)...)
			case "identifier", "type_identifier", "scoped_type_identifier":
				base = append(base, f.text(c))
			}
		}
		return jvmGenericKind(f, strings.Join(base, "."), args, name)
	}
	return vkind{}
}

// jvmGenericKind is the kind of a generic type: a container's elements are its argument's kind,
// another outside type holds what its arguments hold, and the repository's class is that class.
func jvmGenericKind(f *file, base string, args []*node, name func(string) vkind) vkind {
	short := lastElem(base)
	k := name(base)
	switch {
	case !k.outside || len(args) == 0:
		return k
	case jvmElementTypes[short] && len(args) == 1:
		return containerOf(jvmTypeKind(f, args[0], name))
	}
	for _, a := range args {
		if !jvmTypeKind(f, a, name).pure() {
			k.holds = true
		}
	}
	return k
}

// typeName is what a type name stands for in a file that binds names (simple name -> target) and
// imports external from outside the repository: one of the product's classes, or a type of the
// standard library or of an external library.
func (j *jvm) typeName(names map[string]string, external map[string]bool, name string) vkind {
	name = strings.Join(strings.Fields(name), "")
	if root, _, dotted := strings.Cut(name, "."); dotted {
		if jvmStd[root] {
			return outsideKind
		}
		return vkind{}
	}
	if t := names[name]; t != "" {
		if j.isClass(t) {
			return repoKind(t)
		}
		return vkind{}
	}
	if external[name] || jvmBuiltinTypes[name] {
		return outsideKind
	}
	return vkind{}
}

// jvmNames is what a file of the version binds: its package's classes and its imports.
type jvmNames struct {
	names    map[string]string
	external map[string]bool
}

// namesOf returns what the file f binds, read once.
func (j *jvm) namesOf(f *file) jvmNames {
	if b, ok := j.bound[f.path]; ok {
		return b
	}
	b := jvmNames{names: map[string]string{}, external: map[string]bool{}}
	for c := range j.classes[f.unit] {
		b.names[c] = f.unit + "." + c
	}
	imports := javaImports
	if path.Ext(f.path) == ".kt" || path.Ext(f.path) == ".kts" {
		imports = ktImports
	}
	imports(f, func(name string, wildcard bool, _ *node) { j.bindImport(b.names, b.external, name, wildcard) })
	j.bound[f.path] = b
	return b
}

// bindImport binds what one import brings in: the product's classes it names, or, for an import
// from outside the repository, its simple name as external.
func (j *jvm) bindImport(names map[string]string, external map[string]bool, name string, wildcard bool) {
	pkg, class, internal := j.resolve(name, wildcard)
	switch {
	case !internal && !wildcard:
		external[lastElem(name)] = true
	case !internal:
	case wildcard && class == "":
		for c := range j.classes[pkg] {
			names[c] = pkg + "." + c
		}
	case class != "":
		rest := name[len(pkg)+1:] // "Store", or "Store.create" for a static import
		names[lastElem(rest)] = pkg + "." + rest
	}
}

// field is what a field of a class of the product holds, by its declared type, else by its value;
// a Kotlin object's or companion object's property, or a top-level property when class is a
// package. A field that is not declared is unknown.
func (j *jvm) field(class, name string) vkind {
	d, ok := j.decls[class]
	if !ok {
		d, ok = j.decls[class+"."+name]
		if !ok || d.n.Kind() != "property_declaration" {
			return vkind{}
		}
		return j.property(d.f, d.n)
	}
	var found *node
	walk(d.n, func(x *node) bool {
		switch {
		case found != nil:
			return false
		case x != d.n && (x.Kind() == "class_declaration" || x.Kind() == "method_declaration" || x.Kind() == "function_declaration"):
			return false
		case x.Kind() == "field_declaration":
			for _, v := range named(x) {
				if v.Kind() == "variable_declarator" && d.f.field(v, "name") == name {
					found = x
				}
			}
			return false
		case x.Kind() == "property_declaration":
			if v := ktDeclared(x); v != nil && d.f.text(v.NamedChild(0)) == name {
				found = x
			}
			return false
		}
		return true
	})
	if found == nil {
		return vkind{}
	}
	if found.Kind() == "property_declaration" {
		return j.property(d.f, found)
	}
	b := j.namesOf(d.f)
	return jvmTypeKind(d.f, found.ChildByFieldName("type"), func(n string) vkind { return j.typeName(b.names, b.external, n) })
}

// property is what a Kotlin property declaration holds: its declared type, else a string or a
// collection its value builds.
func (j *jvm) property(f *file, n *node) vkind {
	b := j.namesOf(f)
	v := ktDeclared(n)
	var t *node
	if v != nil && v.NamedChildCount() > 1 {
		t = v.NamedChild(1)
	}
	k := jvmTypeKind(f, t, func(name string) vkind { return j.typeName(b.names, b.external, name) })
	if k.known() {
		return k
	}
	for c := n.NamedChild(0); c != nil; c = c.nextSibling {
		if c.IsNamed() && c != v && c.Kind() != "modifiers" && !strings.Contains(c.Kind(), "comment") {
			return ktStaticValue(f, c, b.names)
		}
	}
	return vkind{}
}

// ktDeclared is the variable_declaration of a property declaration, or nil.
func ktDeclared(n *node) *node {
	for _, c := range named(n) {
		if c.Kind() == "variable_declaration" && c.NamedChildCount() > 0 {
			return c
		}
	}
	return nil
}

// ktStaticValue is what a Kotlin value is, read without running anything: a literal, an operation
// on such values, or a string or collection a standard builder makes of them.
func ktStaticValue(f *file, v *node, names map[string]string) vkind {
	switch {
	case ktLiteralKinds[v.Kind()]:
		return outsideKind
	case ktOperations[v.Kind()]:
		for _, o := range named(v) {
			if !ktStaticValue(f, o, names).pure() {
				return vkind{}
			}
		}
		return outsideKind
	case v.Kind() == "call_expression":
		callee := v.NamedChild(0)
		if name := f.text(callee); callee.Kind() == "identifier" && names[name] == "" && ktBuilders[name] {
			out := outsideKind
			walk(v.NamedChild(1), func(x *node) bool {
				if x.Kind() == "value_argument" && !ktStaticValue(f, x.NamedChild(x.NamedChildCount()-1), names).pure() {
					out.holds = true
				}
				return x.Kind() != "value_argument"
			})
			return out
		}
	}
	return vkind{}
}
