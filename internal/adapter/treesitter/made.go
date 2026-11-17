//go:build cgo

package treesitter

import (
	"unicode"
)

// Objects made in place (docs/design/rules-deps.md, "Objects made in place"): making one of the
// repository's classes is a call reaching its unit, since it runs the constructor, and a local
// variable declared without a type that holds one has that class's type.

// madeSyntax is how a language writes making an object, keeping it in a local variable and calling
// a method on it. made is also asked about a missing node (nil), and then makes nothing.
type madeSyntax struct {
	made  func(f *file, n *node) string          // the class an expression makes, as written, or ""
	local func(f *file, n *node) (string, *node) // a variable declared without a type, and its value
	call  func(f *file, n *node) (*node, string) // a method call's object and the method's name
}

// madeInPlace returns the IDs of the repository's classes code makes, and <class>.<method> for the
// methods it calls on what was just made or on local variables holding it. resolve returns the ID of the
// repository's class a written name stands for, or "".
func madeInPlace(f *file, code *node, syn madeSyntax, resolve func(string) string) callLines {
	return madeIn(f, code, nil, syn, resolve)
}

// madeIn is madeInPlace in code without the nodes skip holds.
func madeIn(f *file, code *node, skip map[uintptr]bool, syn madeSyntax, resolve func(string) string) callLines {
	out, locals := callLines{}, map[string]string{}
	if code == nil { // a method without a body, in an interface
		return nil
	}
	walk(code, func(n *node) bool {
		if skip[n.Id()] {
			return false
		}
		if w := syn.made(f, n); w != "" {
			if id := resolve(w); id != "" {
				out.add(id, line(n))
			}
		}
		if name, value := syn.local(f, n); name != "" && value != nil {
			if id := resolve(syn.made(f, value)); id != "" && syn.made(f, value) != "" {
				locals[name] = id
			}
		}
		obj, method := syn.call(f, n)
		switch {
		case obj == nil:
		case obj.Kind() == "identifier" && locals[f.text(obj)] != "":
			out.add(locals[f.text(obj)]+"."+method, line(n))
		case resolve(syn.made(f, obj)) != "": // m.Repo().find(), on what was just made
			out.add(resolve(syn.made(f, obj))+"."+method, line(n))
		}
		return true
	})
	return out
}

var javaMade = madeSyntax{
	made: func(f *file, n *node) string {
		if n != nil && n.Kind() == "object_creation_expression" {
			return f.field(n, "type")
		}
		return ""
	},
	local: func(f *file, n *node) (string, *node) {
		if n.Kind() != "variable_declarator" || n.Parent() == nil || n.Parent().Kind() != "local_variable_declaration" ||
			f.field(n.Parent(), "type") != "var" {
			return "", nil
		}
		return f.field(n, "name"), n.ChildByFieldName("value")
	},
	call: func(f *file, n *node) (*node, string) {
		if n.Kind() == "method_invocation" {
			return n.ChildByFieldName("object"), f.field(n, "name")
		}
		return nil, ""
	},
}

var kotlinMade = madeSyntax{
	made: func(f *file, n *node) string {
		if n == nil || n.Kind() != "call_expression" || n.NamedChildCount() == 0 {
			return ""
		}
		if c := n.NamedChild(0); c.Kind() == "identifier" && unicode.IsUpper([]rune(f.text(c))[0]) {
			return f.text(c)
		}
		return ""
	},
	local: func(f *file, n *node) (string, *node) {
		if n.Kind() != "property_declaration" || n.NamedChildCount() < 2 {
			return "", nil
		}
		for _, c := range named(n) {
			if c.Kind() == "variable_declaration" && c.NamedChildCount() == 1 && c.NamedChild(0).Kind() == "identifier" {
				return f.text(c.NamedChild(0)), n.NamedChild(n.NamedChildCount() - 1)
			}
		}
		return "", nil
	},
	call: func(f *file, n *node) (*node, string) {
		if n.Kind() != "call_expression" || n.NamedChildCount() == 0 {
			return nil, ""
		}
		if nav := n.NamedChild(0); nav.Kind() == "navigation_expression" && nav.NamedChildCount() == 2 {
			return nav.NamedChild(0), f.text(nav.NamedChild(1))
		}
		return nil, ""
	},
}

var tsMade = madeSyntax{
	made: func(f *file, n *node) string {
		if n != nil && n.Kind() == "new_expression" {
			return f.field(n, "constructor")
		}
		return ""
	},
	local: func(f *file, n *node) (string, *node) {
		if n.Kind() != "variable_declarator" || n.ChildByFieldName("type") != nil {
			return "", nil
		}
		return f.field(n, "name"), n.ChildByFieldName("value")
	},
	call: func(f *file, n *node) (*node, string) {
		if fn := n.ChildByFieldName("function"); n.Kind() == "call_expression" && fn != nil && fn.Kind() == "member_expression" {
			return fn.ChildByFieldName("object"), f.field(fn, "property")
		}
		return nil, ""
	},
}

var pyMade = madeSyntax{
	made: func(f *file, n *node) string {
		if n == nil {
			return ""
		}
		if fn := n.ChildByFieldName("function"); n.Kind() == "call" && fn != nil && (fn.Kind() == "identifier" || fn.Kind() == "attribute") {
			return f.text(fn)
		}
		return ""
	},
	local: func(f *file, n *node) (string, *node) {
		if left := n.ChildByFieldName("left"); n.Kind() == "assignment" && left != nil && left.Kind() == "identifier" &&
			n.ChildByFieldName("type") == nil {
			return f.text(left), n.ChildByFieldName("right")
		}
		return "", nil
	},
	call: func(f *file, n *node) (*node, string) {
		if fn := n.ChildByFieldName("function"); n.Kind() == "call" && fn != nil && fn.Kind() == "attribute" {
			return fn.ChildByFieldName("object"), f.field(fn, "attribute")
		}
		return nil, ""
	},
}
