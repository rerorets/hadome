// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

//go:build cgo

package treesitter

import (
	"slices"
	"strings"
)

// What a name in a TypeScript test holds: an argument a helper receives, or a row of the table of
// describe.each and it.each.

// tsParam is one parameter of a function: its name, or the pattern it destructures.
type tsParam struct {
	name    string
	pattern *node
	typ     *node // the written type, nil for none
}

// tsParamList reads a parameter list in order. A parameter with a default value keeps its name: the
// grammar gives it as a parameter with a value, also in JavaScript files.
func tsParamList(f *file, params *node) []tsParam {
	if params == nil {
		return nil
	}
	list := []*node{params}
	if params.Kind() == "formal_parameters" {
		list = arguments(params)
	}
	var out []tsParam
	for _, p := range list {
		var typ *node
		if k := p.Kind(); k == "required_parameter" || k == "optional_parameter" {
			typ = p.ChildByFieldName("type")
			p = p.ChildByFieldName("pattern")
		}
		if p != nil && p.Kind() == "identifier" {
			out = append(out, tsParam{name: f.text(p), typ: typ})
		} else {
			out = append(out, tsParam{pattern: p, typ: typ})
		}
	}
	return out
}

// tsBinding is what a name holds in a walker: a module, a function or class of a module, a
// class of the repository, a value of a kind, or the functions of a table's rows.
type tsBinding struct {
	module, loaded, class string
	kind                  vkind
	rows                  []string
}

// binding is what an argument passed by from holds.
func (from *tsWalk) binding(arg *node) tsBinding {
	arg = tsUnwrap(arg)
	if arg == nil {
		return tsBinding{}
	}
	if arg.Kind() != "identifier" {
		k := from.kindOf(arg)
		return tsBinding{class: k.repo, kind: k}
	}
	name := from.tf.f.text(arg)
	b := tsBinding{module: from.modules[name], loaded: from.loaded[name], class: from.vars[name],
		kind: from.nameKind(name, nil), rows: from.rows[name]}
	if b.module == "" {
		b.module = from.tf.spaces[name]
	}
	if b.loaded == "" && b.class == "" {
		b.loaded = from.tf.names[name] // a function of the repository passed by name: check(append)
	}
	return b
}

// bindArgs binds the parameters of a helper to the arguments from passes, by position: a helper
// called as call(route, 'a') that calls r.GET() calls the module route holds, whatever the
// parameter is named. A parameter that gets nothing it can name, or destructures, holds nothing
// known, and hides an outer name it shares. It returns what puts the walker's names back.
func (w *tsWalk) bindArgs(from *tsWalk, params *node, args []*node) func() {
	type saved struct {
		name string
		b    tsBinding
	}
	var names []string
	var binds []tsBinding
	for i, p := range tsParamList(w.tf.f, params) {
		if p.name == "" {
			for _, n := range tsPatternNames(w.tf.f, p.pattern) {
				names, binds = append(names, n), append(binds, tsBinding{})
			}
			continue
		}
		var b tsBinding
		if i < len(args) {
			b = from.binding(args[i])
		}
		if k := w.typeKind(p.typ); k.known() {
			b.kind, b.class = k, k.repo
		}
		names, binds = append(names, p.name), append(binds, b)
	}
	var old []saved
	for i, n := range names {
		old = append(old, saved{n, w.current(n)})
		w.set(n, binds[i])
	}
	return func() {
		for _, s := range slices.Backward(old) {
			w.set(s.name, s.b)
		}
	}
}

// current is what a name holds in the walker.
func (w *tsWalk) current(name string) tsBinding {
	return tsBinding{module: w.modules[name], loaded: w.loaded[name], class: w.vars[name], kind: w.kinds[name],
		rows: w.rows[name]}
}

// set makes a name hold b, and nothing else.
func (w *tsWalk) set(name string, b tsBinding) {
	put := func(m map[string]string, v string) {
		if v == "" {
			delete(m, name)
		} else {
			m[name] = v
		}
	}
	put(w.modules, b.module)
	put(w.loaded, b.loaded)
	put(w.vars, b.class)
	if b.kind.known() {
		w.kinds[name] = b.kind
	} else {
		delete(w.kinds, name)
	}
	if b.rows != nil {
		w.rows[name] = b.rows
	} else {
		delete(w.rows, name)
	}
}

// rowBindings binds the parameters of a describe.each or it.each callback to the functions and
// components of the repository its table holds, by position: describe.each([[append], [remove]])
// with (fn) => binds fn to both. A row that is not an array is the one argument, and a row that is
// an object binds the names a destructuring parameter takes by key. A tagged template table binds
// nothing.
func (tf *tsFile) rowBindings(table, params *node) map[string][]string {
	args := arguments(table)
	if table == nil || table.Kind() != "arguments" || len(args) != 1 || args[0].Kind() != "array" {
		return nil
	}
	list := tsParamList(tf.f, params)
	out := map[string][]string{}
	for _, row := range arguments(args[0]) {
		cells := []*node{row}
		if row.Kind() == "array" {
			cells = arguments(row)
		}
		for i := 0; i < len(list) && i < len(cells); i++ {
			tf.bindCell(out, list[i], cells[i])
		}
	}
	return out
}

// bindCell binds one parameter to the cell of a row in its position.
func (tf *tsFile) bindCell(out map[string][]string, p tsParam, cell *node) {
	add := func(name string, cell *node) {
		if cell = tsUnwrap(cell); cell != nil && (cell.Kind() == "identifier" || cell.Kind() == "shorthand_property_identifier") {
			if t := tf.names[tf.f.text(cell)]; t != "" && !slices.Contains(out[name], t) {
				out[name] = append(out[name], t)
			}
		}
	}
	if p.name != "" {
		add(p.name, cell)
	} else if p.pattern != nil && p.pattern.Kind() == "object_pattern" && cell.Kind() == "object" {
		tf.rowKeys(p.pattern, cell, add)
	}
}

// rowKeys binds the names an object pattern takes to the values of an object row with those keys.
func (tf *tsFile) rowKeys(pattern, row *node, add func(string, *node)) {
	values := map[string]*node{}
	for _, m := range arguments(row) {
		switch m.Kind() {
		case "pair":
			values[unquote(tf.f.field(m, "key"))] = m.ChildByFieldName("value")
		case "shorthand_property_identifier": // { append } holds the name it spells
			values[tf.f.text(m)] = m
		}
	}
	for _, p := range arguments(pattern) {
		switch p.Kind() {
		case "shorthand_property_identifier_pattern":
			add(tf.f.text(p), values[tf.f.text(p)])
		case "pair_pattern":
			if v := p.ChildByFieldName("value"); v != nil && v.Kind() == "identifier" {
				add(tf.f.text(v), values[unquote(tf.f.field(p, "key"))])
			}
		}
	}
}

// tsImportedNames are the local names an import statement, or the pattern a require() is
// assigned to, binds.
func tsImportedNames(f *file, n *node) []string {
	if k := n.Kind(); k == "identifier" || k == "object_pattern" {
		return tsPatternNames(f, n)
	}
	var out []string
	walk(n, func(x *node) bool {
		switch x.Kind() {
		case "import_specifier":
			local := f.field(x, "alias")
			if local == "" {
				local = f.field(x, "name")
			}
			out = append(out, strings.TrimSpace(local))
		case "namespace_import":
			for _, id := range named(x) {
				out = append(out, f.text(id))
			}
		case "import_clause":
			if first := x.NamedChild(0); first != nil && first.Kind() == "identifier" {
				out = append(out, f.text(first))
			}
		}
		return true
	})
	return out
}
