// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

//go:build cgo

package treesitter

import (
	"strings"

	"github.com/rerorets/hadome/internal/facts"
)

// How the Rust reader finds the calls through a value's declared type (Symbol.Through) and the
// traits a type implements or a trait requires (Symbol.Supertypes), for the call diagram
// (docs/design/rules-deps.md, "Call facts").

// rustPointers are the wrappers whose methods a call reaches through to the value inside:
// Box<dyn Finder>.find() calls Finder's find.
var rustPointers = set("Box", "Arc", "Rc")

// rustSupertypes records the trait an impl implements for its type, and the traits a trait
// requires (trait Store: Finder + Saver).
func rustSupertypes(f *file, c *node, sup supers, resolve func(string) tbind) {
	switch c.Kind() {
	case "impl_item":
		if trait := f.field(c, "trait"); trait != "" {
			if t := resolve(f.field(c, "type")); t.id != "" {
				sup.add(t.id, []string{trait}, resolve)
			}
		}
	case "trait_item":
		if b := c.ChildByFieldName("bounds"); b != nil {
			var written []string
			for _, t := range named(b) {
				written = append(written, f.text(t))
			}
			sup.add(resolve(f.field(c, "name")).id, written, resolve)
		}
	}
}

// rustThrough returns the methods a function's body calls on values whose type, through &, dyn,
// impl, Box, Arc or Rc, or a type parameter's trait bound, is one of the repository's types: its
// parameters and locals, and the fields of its type (self.finder.find()).
func rustThrough(tr *tracing, fn tfunc, repo map[string]bool) callLines {
	f := tr.f
	bounds := rustBounds(f, fn.decl)
	typed := func(written string) string {
		t := rustUnwrap(written)
		if b, ok := bounds[t]; ok {
			t = b
		}
		if id := tr.types(t).id; repo[id] {
			return id
		}
		return ""
	}
	fields, vars := map[string]string{}, map[string]string{}
	if fn.scope != nil {
		walk(fn.scope, func(n *node) bool {
			if n.Kind() == "field_declaration" {
				fields[f.field(n, "name")] = f.field(n, "type")
			}
			return true
		})
	}
	walk(fn.decl, func(n *node) bool {
		if n.Kind() == "parameter" || n.Kind() == "let_declaration" {
			if t := f.field(n, "type"); t != "" {
				vars[f.field(n, "pattern")] = t
			}
		}
		return true
	})
	out := callLines{}
	walk(fn.body, func(n *node) bool {
		if n.Kind() != "call_expression" {
			return true
		}
		callee := n.ChildByFieldName("function")
		if callee == nil || callee.Kind() != "field_expression" {
			return true
		}
		obj, method := callee.ChildByFieldName("value"), f.field(callee, "field")
		written := ""
		switch {
		case obj.Kind() == "identifier":
			written = vars[f.text(obj)]
		case obj.Kind() == "field_expression" && f.field(obj, "value") == "self":
			written = fields[f.field(obj, "field")]
		}
		if id := typed(written); id != "" {
			out.add(id+"."+method, line(n))
		}
		return true
	})
	return out
}

// rustUnwrap reduces a written type to the name of the type a method call reaches: &mut dyn Finder,
// Box<dyn Finder + Send>, std::sync::Arc<impl Finder> and Finder are all Finder.
func rustUnwrap(written string) string {
	t := strings.TrimSpace(written)
	for {
		before := t
		for _, p := range []string{"&", "mut ", "dyn ", "impl "} {
			t = strings.TrimSpace(strings.TrimPrefix(t, p))
		}
		if i := strings.Index(t, "<"); i > 0 && rustPointers[t[strings.LastIndex(t[:i], ":")+1:i]] && strings.HasSuffix(t, ">") {
			t = t[i+1 : len(t)-1]
		}
		if t == before {
			break
		}
	}
	if i := strings.IndexAny(t, "+<"); i >= 0 {
		t = t[:i]
	}
	return strings.TrimSpace(t)
}

// rustBounds maps a function's type parameters to the first trait each is bound by, in its type
// parameters (T: Finder) or its where clause (where T: Finder).
func rustBounds(f *file, decl *node) map[string]string {
	out := map[string]string{}
	add := func(param string, b *node) {
		if _, ok := out[param]; ok || b == nil {
			return
		}
		if ts := named(b); len(ts) > 0 {
			out[param] = rustUnwrap(f.text(ts[0]))
		}
	}
	for _, c := range named(decl) {
		switch c.Kind() {
		case "type_parameters":
			for _, p := range named(c) {
				if p.Kind() == "constrained_type_parameter" || p.Kind() == "type_parameter" {
					add(f.field(p, "left")+f.field(p, "name"), p.ChildByFieldName("bounds"))
				}
			}
		case "where_clause":
			for _, p := range named(c) {
				if p.Kind() == "where_predicate" {
					add(f.field(p, "left"), p.ChildByFieldName("bounds"))
				}
			}
		}
	}
	return out
}

// rustRepoTypes returns the IDs of the version's types and traits.
func rustRepoTypes(a *facts.Analysis) map[string]bool { return tsRepoTypes(a) }
