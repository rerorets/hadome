//go:build cgo

package treesitter

import (
	"strings"
)

// Python's reading of the names a file declares, for the zero-constant skip in zeroconst.go.

func pyConstants(f *file) consts {
	out := consts{}
	walk(f.root, func(n *node) bool {
		switch n.Kind() {
		case "function_definition", "class_definition":
			out.add(f.field(n, "name"), "")
			return false
		case "lambda":
			return false
		case "import_statement", "import_from_statement":
			for _, name := range pyImportBinds(f, n) {
				out.add(name, "")
			}
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
	walk(f.root, func(n *node) bool {
		if n.Kind() == "global_statement" {
			out.unread(f, n)
		}
		return true
	})
	return out
}

// pyImportBinds lists the names a Python import binds: the alias of each aliased name, the name
// itself otherwise, and the first part of a dotted module (import a.b binds a). The module it
// imports from and the original names behind aliases are not bound.
func pyImportBinds(f *file, n *node) []string {
	var out []string
	for i := uint(0); i < n.ChildCount(); i++ {
		if n.FieldNameForChild(uint32(i)) != "name" {
			continue
		}
		c := n.Child(i)
		local := f.text(c)
		if c.Kind() == "aliased_import" {
			local = f.field(c, "alias")
		} else if first, _, ok := strings.Cut(local, "."); ok {
			local = first
		}
		out = append(out, local)
	}
	return out
}
