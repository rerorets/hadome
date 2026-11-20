// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

//go:build cgo

package treesitter

// castKinds are the expressions that take a value out of another by asking whether it holds a type:
// Java's (T) e, Kotlin's e as T and e as? T, TypeScript's e as T and <T>e.
var castKinds = set("cast_expression", "as_expression", "type_assertion")

// isCast reports whether a value, parentheses aside, is a cast.
func isCast(n *node) bool {
	for n != nil && n.Kind() == "parenthesized_expression" && n.NamedChildCount() == 1 {
		n = n.NamedChild(0)
	}
	return n != nil && castKinds[n.Kind()]
}

// castLocals returns the locals a declaration sets from a cast, by name: Java's T x = (T) e and
// Kotlin's val x: T = e as T. Which implementation such a value holds is decided by the call that
// returned it.
func castLocals(f *file, n *node) map[string]bool {
	out := map[string]bool{}
	switch n.Kind() {
	case "local_variable_declaration":
		for _, d := range named(n) {
			if d.Kind() == "variable_declarator" && isCast(d.ChildByFieldName("value")) {
				out[f.field(d, "name")] = true
			}
		}
	case "variable_declaration":
		if p := n.Parent(); p != nil && p.Kind() == "property_declaration" {
			kids := named(p)
			for i, c := range kids {
				if c.Id() == n.Id() && i+1 < len(kids) && isCast(kids[i+1]) {
					for _, id := range named(n) {
						if id.Kind() == "identifier" {
							out[f.text(id)] = true
							break
						}
					}
				}
			}
		}
	}
	return out
}

// declare types the locals a declaration of the function being read sets, and notes those set from
// a cast.
func (tr *tracing) declare(n *node) {
	cast := castLocals(tr.f, n)
	for k, v := range tr.syn.vars(tr.f, n) {
		tr.locals[k] = v
		if cast[k] {
			tr.asked[k] = true
		} else {
			delete(tr.asked, k)
		}
	}
}
