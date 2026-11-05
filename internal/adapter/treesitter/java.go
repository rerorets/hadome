//go:build cgo

package treesitter

import (
	"regexp"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	javagrammar "github.com/tree-sitter/tree-sitter-java/bindings/go"

	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
)

// java reads Java.
type java struct{ jvm }

func (*java) grammar(string) *sitter.Language { return sitter.NewLanguage(javagrammar.Language()) }

var javaBranches = branches{
	kinds: set("if_statement", "for_statement", "enhanced_for_statement", "while_statement", "do_statement",
		"catch_clause", "ternary_expression", "switch_label"),
	ops: set("&&", "||"),
}

var javaTables = jvmTables(nil, nil, nil)

var javaDirective = regexp.MustCompile(`^//\s*(CHECKSTYLE|NOSONAR|noinspection|@formatter)`)

var javaTypes = set("class_declaration", "interface_declaration", "enum_declaration", "record_declaration", "annotation_type_declaration")

func (j *java) index(_ engine.Version, files []*file) {
	j.reset()
	for _, f := range files {
		pkg := ""
		var classes []string
		for _, n := range named(f.root) {
			switch {
			case n.Kind() == "package_declaration" && n.NamedChildCount() > 0:
				pkg = f.text(n.NamedChild(0))
			case javaTypes[n.Kind()]:
				classes = append(classes, f.field(n, "name"))
			}
		}
		j.place(f, pkg, classes)
	}
}

// javaImports lists the import declarations as (name, wildcard, node).
func javaImports(f *file, visit func(name string, wildcard bool, n *sitter.Node)) {
	for _, n := range named(f.root) {
		if n.Kind() != "import_declaration" || n.NamedChildCount() == 0 {
			continue
		}
		wildcard := false
		for _, c := range named(n) {
			wildcard = wildcard || c.Kind() == "asterisk"
		}
		visit(f.text(n.NamedChild(0)), wildcard, n)
	}
}

func (j *java) imports(f *file, t *tables) []facts.Import {
	var out []facts.Import
	javaImports(f, func(name string, wildcard bool, n *sitter.Node) {
		pkg, _, internal := j.resolve(name, wildcard)
		out = append(out, facts.Import{Path: f.path, Line: line(n), FromUnit: f.unit, ToUnit: pkg,
			Internal: internal, IO: !internal && t.isIO(name)})
	})
	for _, c := range dynamicCalls(f, set("method_invocation"), jvmDynamic, nil) {
		if c.spec == "" {
			out = append(out, unknownImport(f, c.at))
			continue
		}
		pkg, _, internal := j.resolve(c.spec, false)
		out = append(out, facts.Import{Path: f.path, Line: line(c.at), FromUnit: f.unit, ToUnit: pkg,
			Internal: internal, IO: !internal && t.isIO(c.spec)})
	}
	return out
}

func (j *java) symbols(f *file) []facts.Symbol {
	var out []facts.Symbol
	for _, n := range named(f.root) {
		if javaTypes[n.Kind()] {
			out = append(out, j.typeSymbols(f, n, f.unit, true)...)
		}
	}
	return out
}

// public reports whether a declaration's modifiers include the keyword.
func modifier(f *file, n *sitter.Node, word string) bool {
	for _, c := range named(n) {
		if c.Kind() == "modifiers" {
			for _, w := range strings.Fields(f.text(c)) {
				if w == word {
					return true
				}
			}
		}
	}
	return false
}

// typeSymbols lists a type and its members; visible says whether the enclosing type is public.
func (j *java) typeSymbols(f *file, n *sitter.Node, prefix string, visible bool) []facts.Symbol {
	id := prefix + "." + f.field(n, "name")
	iface := n.Kind() == "interface_declaration"
	public := visible && modifier(f, n, "public")
	typ := facts.Symbol{ID: id, Kind: facts.SymbolType, Path: f.path, Line: line(n), EndLine: endLine(n),
		Exported: public, Doc: docBefore(f, n, "/**")}
	if iface {
		typ.Kind = facts.SymbolInterface
	}
	out := []facts.Symbol{typ}
	for _, m := range named(n.ChildByFieldName("body")) {
		switch {
		case javaTypes[m.Kind()]:
			out = append(out, j.typeSymbols(f, m, id, public)...)
		case m.Kind() == "method_declaration" || m.Kind() == "constructor_declaration":
			if iface {
				out[0].Methods++
			}
			s := facts.Symbol{ID: id + "." + f.field(m, "name"), Kind: facts.SymbolMethod,
				Exported: public && (iface || modifier(f, m, "public")), Doc: docBefore(f, m, "/**")}
			params := javaParams(f, m)
			j.scopes[s.ID] = scopeOf(params)
			out = append(out, funcSymbol(f, m, s, params, javaBranches.complexity(f, m.ChildByFieldName("body"))))
		}
	}
	return out
}

func javaParams(f *file, m *sitter.Node) []string {
	var out []string
	for _, p := range named(m.ChildByFieldName("parameters")) {
		if name := f.field(p, "name"); name != "" {
			out = append(out, name)
		} else if p.Kind() == "spread_parameter" {
			out = append(out, "...")
		}
	}
	return out
}
