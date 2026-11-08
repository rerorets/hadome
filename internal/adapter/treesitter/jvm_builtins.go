//go:build cgo

package treesitter

import (
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// The locals a Java or Kotlin test builds as strings or standard collections. A method called on
// one is the language's own (sql.contains("SELECT")): neither a call of the product nor a call
// hadome could not resolve.

// What a local holds, when it is the language's own.
const (
	builtinString     = "string"     // a string: its members are strings, numbers or collections of them
	builtinCollection = "collection" // a standard collection, whose elements may be anything
)

// jvmBuiltinTypes are the declared types of locals that hold a string or a standard collection.
var jvmBuiltinTypes = map[string]string{"String": builtinString, "StringBuilder": builtinString,
	"CharSequence": builtinString, "List": builtinCollection, "MutableList": builtinCollection,
	"Set": builtinCollection, "MutableSet": builtinCollection, "Map": builtinCollection,
	"MutableMap": builtinCollection, "Collection": builtinCollection, "ArrayList": builtinCollection,
	"LinkedList": builtinCollection, "HashMap": builtinCollection, "HashSet": builtinCollection,
	"LinkedHashMap": builtinCollection, "LinkedHashSet": builtinCollection, "TreeMap": builtinCollection,
	"TreeSet": builtinCollection}

// ktBuilders are the standard library's functions that build a string or a collection.
var ktBuilders = map[string]string{"buildString": builtinString, "StringBuilder": builtinString,
	"listOf": builtinCollection, "mutableListOf": builtinCollection, "arrayListOf": builtinCollection,
	"listOfNotNull": builtinCollection, "setOf": builtinCollection, "mutableSetOf": builtinCollection,
	"hashSetOf": builtinCollection, "mapOf": builtinCollection, "mutableMapOf": builtinCollection,
	"hashMapOf": builtinCollection, "linkedMapOf": builtinCollection, "emptyList": builtinCollection,
	"emptySet": builtinCollection, "emptyMap": builtinCollection, "buildList": builtinCollection,
	"buildSet": builtinCollection, "buildMap": builtinCollection, "arrayOf": builtinCollection}

// noteBuiltin records what a local holds: builtinString, builtinCollection, or "" for anything else.
func (w *jvmWalk) noteBuiltin(name, kind string) {
	if w.builtins == nil {
		w.builtins = map[string]string{}
	}
	w.builtins[name] = kind
}

// builtinType is what a declared type holds, when it is a string or a standard collection and not
// a class of the product of the same name.
func (w *jvmWalk) builtinType(t string) string {
	t = typeName(t)
	if w.jf.names[t] != "" {
		return ""
	}
	return jvmBuiltinTypes[t]
}

// ktBuiltin is what a Kotlin expression holds: a string literal or a member of a string
// ("…".trimIndent().split(" ")) is a string, a call of a builder a collection.
func (w *jvmWalk) ktBuiltin(e *sitter.Node) string {
	f := w.jf.f
	switch e.Kind() {
	case "string_literal", "multiline_string_literal":
		return builtinString
	case "identifier":
		return w.builtins[f.text(e)]
	case "call_expression":
		callee := e.NamedChild(0)
		if callee.Kind() != "identifier" {
			return w.ktBuiltin(callee)
		}
		if name := f.text(callee); w.jf.names[name] == "" && w.helper(name).n == nil {
			return ktBuilders[name]
		}
	case "navigation_expression":
		if e.NamedChildCount() == 2 && w.ktBuiltin(e.NamedChild(0)) == builtinString {
			return builtinString
		}
	}
	return ""
}

// javaBuiltin is what a Java expression holds: a string literal, a concatenation with a string, or
// a method called on a string is a string.
func (w *jvmWalk) javaBuiltin(e *sitter.Node) string {
	if e == nil {
		return ""
	}
	switch e.Kind() {
	case "string_literal", "text_block":
		return builtinString
	case "identifier":
		return w.builtins[w.jf.f.text(e)]
	case "binary_expression":
		if w.javaBuiltin(e.ChildByFieldName("left")) == builtinString ||
			w.javaBuiltin(e.ChildByFieldName("right")) == builtinString {
			return builtinString
		}
	case "method_invocation":
		if w.javaBuiltin(e.ChildByFieldName("object")) == builtinString {
			return builtinString
		}
	}
	return ""
}
