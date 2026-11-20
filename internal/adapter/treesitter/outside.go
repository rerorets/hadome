// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

//go:build cgo

package treesitter

import "github.com/rerorets/hadome/internal/facts"

// outsideSkipped are the kinds of the declarations reading the code outside functions leaves out:
// they name what they bring in or declare, and call nothing.
var outsideSkipped = set("import_statement", "import_from_statement", "future_import_statement", "use_declaration",
	"import_declaration", "package_declaration", "import_header", "import_list", "package_header", "export_clause")

// declarations are the kinds of declarations whose name reading the code outside functions leaves
// out: a name declared is not a call of it.
var declarations = set("function_definition", "class_definition", "function_declaration", "generator_function_declaration",
	"class_declaration", "abstract_class_declaration", "interface_declaration", "method_definition", "method_declaration",
	"constructor_declaration", "function_item", "struct_item", "enum_item", "trait_item", "variable_declarator")

// outsideSkip returns the nodes reading the code outside functions leaves out: the bodies of the
// functions, the imports, the names of declarations, and Rust's test modules.
func outsideSkip(f *file, bodies []*node) map[uintptr]bool {
	skip := map[uintptr]bool{}
	for _, b := range bodies {
		if b != nil {
			skip[b.Id()] = true
		}
	}
	walk(f.root, func(n *node) bool {
		if outsideSkipped[n.Kind()] || n.Kind() == "mod_item" && testModule(f, n) {
			skip[n.Id()] = true
			return false
		}
		if name := n.ChildByFieldName("name"); name != nil && declarations[n.Kind()] {
			skip[name.Id()] = true
		}
		return true
	})
	return skip
}

// outside sets a file's facts for its code outside the bodies of the functions fns lists.
func (tr *tracing) outside(file *facts.File, fns []tfunc) {
	bodies := make([]*node, len(fns))
	for i, fn := range fns {
		bodies[i] = fn.body
	}
	tr.skip = outsideSkip(tr.f, bodies)
	defer func() { tr.skip = nil }()
	got := tr.reach(tfunc{own: tr.f.unit}, tr.f.root)
	file.Calls, file.Asserted, file.CallAt = got.ids(), got.askedIDs(), got.lines()
	if tr.madeOutside != nil {
		addFileThrough(file, tr.madeOutside(tr.skip))
	}
}

// addFileThrough adds to a file the calls of through that are not among its calls.
func addFileThrough(file *facts.File, through callLines) {
	s := facts.Symbol{Calls: file.Calls, CallAt: file.CallAt}
	addThrough(&s, through)
	file.Through, file.CallAt = s.Through, s.CallAt
}

// outside sets a TypeScript file's facts for its code outside the bodies of its functions.
func (ty *tsTypes) outside(file *facts.File, bodies []*node) {
	tr := ty.tr
	tr.skip = outsideSkip(tr.f, bodies)
	defer func() { tr.skip = nil }()
	got := tr.reach(tr.f.root)
	file.Calls, file.CallAt = got.ids(), got.lines()
	addFileThrough(file, madeIn(tr.f, tr.f.root, tr.skip, tsMade, ty.resolve))
}
