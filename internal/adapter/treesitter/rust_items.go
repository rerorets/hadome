//go:build cgo

package treesitter

import (
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"

	fswalk "github.com/rerorets/hadome/internal/adapter/walk"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
)

// fsFiles lists every file of the version that hadome reads.
func fsFiles(v engine.Version) ([]string, error) { return fswalk.Files(v.FS) }

func rustPub(f *file, n *sitter.Node) bool {
	for _, c := range named(n) {
		if c.Kind() == "visibility_modifier" {
			return f.text(c) == "pub"
		}
	}
	return false
}

var rustTypes = set("struct_item", "enum_item", "type_item", "union_item")

func (r *rust) symbols(f *file) []facts.Symbol { return r.items(f, f.root, f.unit) }

// items lists the symbols declared in a module body, leaving out modules compiled only for tests.
func (r *rust) items(f *file, body *sitter.Node, prefix string) []facts.Symbol {
	var out []facts.Symbol
	for _, c := range named(body) {
		name := f.field(c, "name")
		typ := facts.Symbol{ID: prefix + "." + name, Kind: facts.SymbolType, Path: f.path, Line: line(c), EndLine: endLine(c),
			Exported: rustPub(f, c), Doc: rustDoc(f, c)}
		if prefix == f.unit && (c.Kind() == "function_item" || rustTypes[c.Kind()] || c.Kind() == "trait_item" || c.Kind() == "const_item" || c.Kind() == "static_item") {
			r.defs[prefix] = append(r.defs[prefix], name)
		}
		switch {
		case c.Kind() == "function_item":
			out = append(out, r.function(f, c, prefix, facts.SymbolFunction, rustPub(f, c)))
		case rustTypes[c.Kind()]:
			out = append(out, typ)
		case c.Kind() == "trait_item":
			typ.Kind = facts.SymbolInterface
			methods := r.methods(f, c.ChildByFieldName("body"), typ.ID, typ.Exported)
			typ.Methods = len(methods)
			out = append(append(out, typ), methods...)
		case c.Kind() == "impl_item":
			self := strings.SplitN(f.field(c, "type"), "<", 2)[0]
			out = append(out, r.methods(f, c.ChildByFieldName("body"), prefix+"."+self, c.ChildByFieldName("trait") != nil)...)
		case c.Kind() == "mod_item" && c.ChildByFieldName("body") != nil && !testModule(f, c):
			out = append(out, r.items(f, c.ChildByFieldName("body"), prefix+"/"+name)...)
		}
	}
	return out
}

// methods lists the functions of an impl or trait body; all are exported when public says so.
func (r *rust) methods(f *file, body *sitter.Node, prefix string, public bool) []facts.Symbol {
	var out []facts.Symbol
	for _, m := range named(body) {
		if m.Kind() == "function_item" || m.Kind() == "function_signature_item" {
			out = append(out, r.function(f, m, prefix, facts.SymbolMethod, public || rustPub(f, m)))
		}
	}
	return out
}

func (r *rust) function(f *file, n *sitter.Node, prefix string, kind facts.SymbolKind, exported bool) facts.Symbol {
	s := facts.Symbol{ID: prefix + "." + f.field(n, "name"), Kind: kind, Exported: exported, Doc: rustDoc(f, n)}
	var params []string
	for _, p := range named(n.ChildByFieldName("parameters")) {
		if p.Kind() == "parameter" {
			params = append(params, f.field(p, "pattern"))
		}
	}
	r.scopes[s.ID] = scopeOf(params)
	return funcSymbol(f, n, s, params, rustBranches.complexity(f, n.ChildByFieldName("body")))
}
