//go:build cgo

package treesitter

import (
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

// The parts of a test's annotations, decorators and attributes that only give it a name to show,
// which its fingerprint leaves out as it leaves out the test's own name. Anything else written on a
// test stays in: an input, a timeout, a tag that selects which runs include it.

// jvmNamedAnnotations are the JUnit annotations whose name element only sets the display name of the
// test or of each of its runs.
var jvmNamedAnnotations = set("ParameterizedTest", "RepeatedTest", "ParameterizedClass")

// jvmNameOnly lists the parts of the Java or Kotlin annotations in roots that only name a test:
// @DisplayName, and the name element of jvmNamedAnnotations.
func jvmNameOnly(f *file, roots []*sitter.Node) []*sitter.Node {
	var out []*sitter.Node
	walkAnnotations(roots, map[string]bool{"block": true, "function_body": true, "class_body": true}, func(n *sitter.Node) {
		if n.Kind() != "annotation" && n.Kind() != "marker_annotation" {
			return
		}
		switch name := jvmAnnotationName(f, n); {
		case name == "DisplayName":
			out = append(out, n)
		case jvmNamedAnnotations[name]:
			list, args := jvmAnnotationArgs(n)
			var drop []*sitter.Node
			for _, arg := range args {
				if key, ok := jvmArgKey(f, arg); ok && key == "name" {
					drop = append(drop, withSeparator(arg)...)
				}
			}
			if len(drop) > 0 && len(args) == 1 { // @ParameterizedTest(name = "x") is @ParameterizedTest
				drop = []*sitter.Node{list}
			}
			out = append(out, drop...)
		}
	})
	return out
}

// jvmAnnotationName is the simple name of an annotation: DisplayName for @DisplayName("x") and for
// @org.junit.jupiter.api.DisplayName("x").
func jvmAnnotationName(f *file, n *sitter.Node) string {
	var name *sitter.Node
	if name = n.ChildByFieldName("name"); name == nil { // Kotlin
		for _, c := range named(n) {
			switch c.Kind() {
			case "user_type":
				name = c
			case "constructor_invocation":
				for _, cc := range named(c) {
					if cc.Kind() == "user_type" {
						name = cc
					}
				}
			}
		}
	}
	if name == nil {
		return ""
	}
	text := f.text(name)
	return text[strings.LastIndex(text, ".")+1:]
}

// jvmAnnotationArgs returns the argument list of an annotation and the arguments in it: the element
// value pairs and values of a Java annotation, the value arguments of a Kotlin one.
func jvmAnnotationArgs(n *sitter.Node) (*sitter.Node, []*sitter.Node) {
	if args := n.ChildByFieldName("arguments"); args != nil { // Java
		return args, named(args)
	}
	for _, c := range named(n) {
		if c.Kind() != "constructor_invocation" {
			continue
		}
		for _, cc := range named(c) {
			if cc.Kind() == "value_arguments" {
				return cc, named(cc)
			}
		}
	}
	return nil, nil
}

// jvmArgKey is the key of a named annotation argument: name for name = "x" in Java or Kotlin. It
// reports false for an argument given by position.
func jvmArgKey(f *file, arg *sitter.Node) (string, bool) {
	if arg.Kind() == "element_value_pair" {
		return f.text(arg.ChildByFieldName("key")), true
	}
	if arg.Kind() != "value_argument" {
		return "", false
	}
	for i := uint(0); i < arg.ChildCount(); i++ {
		if arg.Child(i).Kind() == "=" && arg.NamedChildCount() > 0 {
			return f.text(arg.NamedChild(0)), true
		}
	}
	return "", false
}

// pyNameOnly lists the parts of the decorators in roots that only name a test: the ids= argument of
// pytest.mark.parametrize and the id= argument of pytest.param.
func pyNameOnly(f *file, roots []*sitter.Node) []*sitter.Node {
	var out []*sitter.Node
	walkAnnotations(roots, map[string]bool{"block": true}, func(n *sitter.Node) {
		if n.Kind() != "call" {
			return
		}
		var key string
		switch fn := f.text(n.ChildByFieldName("function")); {
		case fn == "parametrize" || strings.HasSuffix(fn, ".parametrize"):
			key = "ids"
		case fn == "param" || strings.HasSuffix(fn, ".param"):
			key = "id"
		default:
			return
		}
		args := n.ChildByFieldName("arguments")
		if args == nil {
			return
		}
		for _, a := range named(args) {
			if a.Kind() == "keyword_argument" && f.text(a.ChildByFieldName("name")) == key {
				out = append(out, withSeparator(a)...)
			}
		}
	})
	return out
}

// rustNameOnly lists the parts of the attributes in roots that only name a test: the description
// of an rstest case, ::small in #[case::small(1)].
func rustNameOnly(f *file, roots []*sitter.Node) []*sitter.Node {
	var out []*sitter.Node
	walkAnnotations(roots, map[string]bool{"block": true}, func(n *sitter.Node) {
		if n.Kind() != "attribute" || n.NamedChildCount() == 0 {
			return
		}
		path := n.NamedChild(0)
		if path.Kind() != "scoped_identifier" || f.text(path.ChildByFieldName("path")) != "case" {
			return
		}
		for i := uint(0); i < path.ChildCount(); i++ {
			if c := path.Child(i); c.Kind() == "::" {
				out = append(out, c)
			}
		}
		out = append(out, path.ChildByFieldName("name"))
	})
	return out
}

// walkAnnotations calls visit on every node of roots, not descending into the kinds in stop: the
// body of the test, whose tokens are its code and never only its name.
func walkAnnotations(roots []*sitter.Node, stop map[string]bool, visit func(*sitter.Node)) {
	var walk func(n *sitter.Node)
	walk = func(n *sitter.Node) {
		if stop[n.Kind()] {
			return
		}
		visit(n)
		for i := uint(0); i < n.NamedChildCount(); i++ {
			walk(n.NamedChild(i))
		}
	}
	for _, n := range roots {
		if n != nil {
			walk(n)
		}
	}
}

// withSeparator returns an argument and the comma that separates it from its neighbours, so that
// leaving both out leaves the tokens the list would have without the argument.
func withSeparator(n *sitter.Node) []*sitter.Node {
	if p := n.PrevSibling(); p != nil && p.Kind() == "," {
		return []*sitter.Node{n, p}
	}
	if s := n.NextSibling(); s != nil && s.Kind() == "," {
		return []*sitter.Node{n, s}
	}
	return []*sitter.Node{n}
}
