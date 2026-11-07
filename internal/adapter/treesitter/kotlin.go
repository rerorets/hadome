//go:build cgo

package treesitter

import (
	"regexp"
	"strings"

	kotlingrammar "github.com/tree-sitter-grammars/tree-sitter-kotlin/bindings/go"
	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
)

// kotlin reads Kotlin.
type kotlin struct {
	jvm
	functions map[string][]string // package -> top-level functions of product code
	values    map[string][]string // package -> top-level properties of product code
}

func (*kotlin) grammar(string) *sitter.Language { return sitter.NewLanguage(kotlingrammar.Language()) }

var ktBranches = branches{
	kinds: set("if_expression", "for_statement", "while_statement", "do_while_statement", "catch_block", "when_entry"),
	ops:   set("&&", "||"),
}

var ktTables = jvmTables([]string{"io.ktor.client", "kotlinx.coroutines.io"}, []string{"io.mockk"},
	map[string]string{"delay": "sleep", "Clock.System.now": "clock", "mockk": "mock", "spyk": "mock",
		"measureTimeMillis": "clock", "measureNanoTime": "clock", "measureTime": "clock", "measureTimedValue": "clock",
		"TimeSource.Monotonic.markNow": "clock"})

var ktDirective = regexp.MustCompile(`^//\s*(ktlint-disable|noinspection|@formatter)`)

var ktTypes = set("class_declaration", "object_declaration")

func (k *kotlin) index(_ engine.Version, files []*file) {
	k.reset()
	k.functions = map[string][]string{}
	k.values = map[string][]string{}
	for _, f := range files {
		pkg := ""
		var classes, functions, values []string
		var types []*sitter.Node
		for _, n := range named(f.root) {
			switch {
			case n.Kind() == "package_header" && n.NamedChildCount() > 0:
				pkg = f.text(n.NamedChild(0))
			case ktTypes[n.Kind()]:
				classes = append(classes, f.field(n, "name"))
				types = append(types, n)
			case n.Kind() == "function_declaration":
				functions = append(functions, f.field(n, "name"))
			case n.Kind() == "property_declaration":
				for _, c := range named(n) {
					if c.Kind() == "variable_declaration" && c.NamedChildCount() > 0 {
						values = append(values, f.text(c.NamedChild(0)))
					}
				}
			}
		}
		k.place(f, pkg, classes)
		for _, n := range types {
			k.noteType(f, n, f.field(n, "name"), ktEntries(f, n))
		}
		if !f.test {
			k.functions[f.unit] = append(k.functions[f.unit], functions...)
			k.values[f.unit] = append(k.values[f.unit], values...)
		}
	}
}

// ktImports lists the imports as (name, wildcard, node).
func ktImports(f *file, visit func(name string, wildcard bool, n *sitter.Node)) {
	for _, n := range named(f.root) {
		if n.Kind() != "import" || n.NamedChildCount() == 0 {
			continue
		}
		visit(f.text(n.NamedChild(0)), strings.HasSuffix(strings.TrimSpace(f.text(n)), ".*"), n)
	}
}

func (k *kotlin) imports(f *file, t *tables) []facts.Import {
	var out []facts.Import
	ktImports(f, func(name string, wildcard bool, n *sitter.Node) {
		pkg, _, internal := k.resolve(name, wildcard)
		out = append(out, facts.Import{Path: f.path, Line: line(n), FromUnit: f.unit, ToUnit: pkg,
			Internal: internal, IO: !internal && t.isIO(name)})
	})
	for _, c := range dynamicCalls(f, set("call_expression"), jvmDynamic, ktTemplate.MatchString) {
		if c.spec == "" {
			out = append(out, unknownImport(f, c.at))
			continue
		}
		pkg, _, internal := k.resolve(c.spec, false)
		out = append(out, facts.Import{Path: f.path, Line: line(c.at), FromUnit: f.unit, ToUnit: pkg,
			Internal: internal, IO: !internal && t.isIO(c.spec)})
	}
	return out
}

// ktPublic reports whether a declaration is visible outside its module: Kotlin's default is public.
func ktPublic(f *file, n *sitter.Node) bool {
	for _, c := range named(n) {
		if c.Kind() != "modifiers" {
			continue
		}
		for _, m := range named(c) {
			if m.Kind() == "visibility_modifier" && f.text(m) != "public" {
				return false
			}
		}
	}
	return true
}

// ktInterface reports whether a class declaration is an interface.
func ktInterface(f *file, n *sitter.Node) bool {
	for i := uint(0); i < n.ChildCount(); i++ {
		if c := n.Child(i); !c.IsNamed() && f.text(c) == "interface" {
			return true
		}
	}
	return false
}

func (k *kotlin) symbols(f *file) []facts.Symbol {
	var out []facts.Symbol
	for _, n := range named(f.root) {
		switch {
		case ktTypes[n.Kind()]:
			out = append(out, k.typeSymbols(f, n, f.unit, true)...)
		case n.Kind() == "function_declaration":
			out = append(out, k.function(f, n, f.unit, facts.SymbolFunction, true))
		}
	}
	return out
}

func (k *kotlin) typeSymbols(f *file, n *sitter.Node, prefix string, visible bool) []facts.Symbol {
	id := prefix + "." + f.field(n, "name")
	public := visible && ktPublic(f, n)
	typ := facts.Symbol{ID: id, Kind: facts.SymbolType, Path: f.path, Line: line(n), EndLine: endLine(n),
		Exported: public, Doc: ktDoc(f, n)}
	if ktInterface(f, n) {
		typ.Kind = facts.SymbolInterface
	}
	out := []facts.Symbol{typ}
	for _, m := range ktMembers(n) {
		switch {
		case ktTypes[m.Kind()]:
			out = append(out, k.typeSymbols(f, m, id, public)...)
		case m.Kind() == "function_declaration":
			if typ.Kind == facts.SymbolInterface {
				out[0].Methods++
			}
			out = append(out, k.function(f, m, id, facts.SymbolMethod, public))
		}
	}
	return out
}

// ktDoc reports whether a declaration has a /** */ comment right before it, or before the
// annotations ktLeading finds split off it.
func ktDoc(f *file, n *sitter.Node) bool {
	_, doc := ktLeading(f, n)
	return doc || docBefore(f, n, "/**")
}

// ktLeading reads what this grammar splits off a top-level declaration. An annotation with
// arguments on a line of its own (@Suppress("x")) is read as an expression of its own
// (annotated_expression) before the declaration, and the declaration's doc comment sits before or
// inside that expression. It returns those annotations by name, each as the node holding its
// arguments, and whether a /** */ comment ends on the line before the first of them.
func ktLeading(f *file, n *sitter.Node) (map[string]*sitter.Node, bool) {
	prev := n.PrevSibling()
	if prev == nil || prev.Kind() != "annotated_expression" {
		return map[string]*sitter.Node{}, false
	}
	r := &ktLead{f: f}
	r.restart(nil, false)
	r.read(prev)
	if r.comment == nil && !r.other {
		if c := prev.PrevSibling(); c != nil && strings.Contains(c.Kind(), "comment") {
			r.comment = c
		}
	}
	doc := r.comment != nil && r.first != nil && strings.HasPrefix(strings.TrimSpace(f.text(r.comment)), "/**") &&
		endLine(r.comment) >= line(r.first)-1
	return r.anns, doc
}

// ktLead reads the annotations at the end of an annotated_expression. A comment before them may be
// the doc; any other code before them belongs to something else, and so do annotations before it.
type ktLead struct {
	f              *file
	anns           map[string]*sitter.Node
	comment, first *sitter.Node
	other          bool
}

// restart forgets what came before a comment or other code.
func (r *ktLead) restart(comment *sitter.Node, other bool) {
	r.anns, r.first, r.comment, r.other = map[string]*sitter.Node{}, nil, comment, other
}

func (r *ktLead) read(x *sitter.Node) {
	for i, c := range named(x) {
		switch {
		case c.Kind() == "annotation":
			r.annotation(c, x.NamedChild(uint(i)+1))
		case c.Kind() == "parenthesized_expression" && i > 0 && x.NamedChild(uint(i)-1).Kind() == "annotation":
			// the arguments of the annotation before it
		case strings.Contains(c.Kind(), "comment"):
			r.restart(c, false)
		case c.Kind() == "annotated_expression":
			r.read(c)
		default:
			r.restart(nil, true)
			r.read(c)
		}
	}
}

// annotation records an annotation by name, as the node holding its arguments when they follow it.
func (r *ktLead) annotation(a, next *sitter.Node) {
	name := ""
	walk(a, func(t *sitter.Node) bool {
		if name == "" && t.Kind() == "user_type" {
			name = lastElem(r.f.text(t))
		}
		return name == ""
	})
	holder := a
	if next != nil && next.Kind() == "parenthesized_expression" {
		holder = next
	}
	r.anns[name] = holder
	if r.first == nil {
		r.first = a
	}
}

// ktEntries are the constants of an enum class.
func ktEntries(f *file, n *sitter.Node) []string {
	var out []string
	for _, m := range ktMembers(n) {
		if m.Kind() == "enum_entry" && m.NamedChildCount() > 0 {
			out = append(out, f.text(m.NamedChild(0)))
		}
	}
	return out
}

// ktMembers returns the declarations in a class or object body.
func ktMembers(n *sitter.Node) []*sitter.Node {
	for _, c := range named(n) {
		if c.Kind() == "class_body" || c.Kind() == "enum_class_body" {
			return named(c)
		}
	}
	return nil
}

func (k *kotlin) function(f *file, n *sitter.Node, prefix string, kind facts.SymbolKind, visible bool) facts.Symbol {
	s := facts.Symbol{ID: prefix + "." + f.field(n, "name"), Kind: kind, Exported: visible && ktPublic(f, n), Doc: ktDoc(f, n)}
	params := ktParams(f, n)
	k.scopes[s.ID] = scopeOf(params)
	var body *sitter.Node
	for _, c := range named(n) {
		if c.Kind() == "function_body" {
			body = c
		}
	}
	return funcSymbol(f, n, s, params, ktBranches.complexity(f, body))
}

func ktParams(f *file, n *sitter.Node) []string {
	var out []string
	for _, c := range named(n) {
		if c.Kind() != "function_value_parameters" {
			continue
		}
		for _, p := range named(c) {
			if p.Kind() == "parameter" && p.NamedChildCount() > 0 {
				out = append(out, f.text(p.NamedChild(0)))
			}
		}
	}
	return out
}

// ktTemplate finds a value substituted into a Kotlin string ($name or ${…}); this grammar keeps it
// in the string's text. An escaped \$ is a dollar sign.
var ktTemplate = regexp.MustCompile(`(^|[^\\])\$[A-Za-z_{]`)
