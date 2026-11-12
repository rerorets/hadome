//go:build cgo

package treesitter

import (
	"bytes"
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
	// testFunctions are the top-level functions of test code: package -> name -> declaration.
	testFunctions map[string]map[string]jvmDecl
}

var kotlinGrammar = sitter.NewLanguage(kotlingrammar.Language())

func (*kotlin) grammar(string) *sitter.Language { return kotlinGrammar }

var ktBranches = branches{
	kinds:  set("if_expression", "for_statement", "while_statement", "do_while_statement", "catch_block", "when_entry"),
	ops:    set("&&", "||"),
	nested: set("lambda_literal", "anonymous_function", "function_declaration"),
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
	k.testFunctions = map[string]map[string]jvmDecl{}
	for _, f := range files {
		pkg := ""
		var classes, functions, values []string
		var types, fns []*node
		for _, n := range named(f.root) {
			switch {
			case n.Kind() == "package_header" && n.NamedChildCount() > 0:
				pkg = f.text(n.NamedChild(0))
			case ktTypes[n.Kind()]:
				classes = append(classes, f.field(n, "name"))
				types = append(types, n)
			case n.Kind() == "function_declaration":
				functions = append(functions, f.field(n, "name"))
				fns = append(fns, n)
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
			continue
		}
		for _, n := range fns {
			if k.testFunctions[f.unit] == nil {
				k.testFunctions[f.unit] = map[string]jvmDecl{}
			}
			k.testFunctions[f.unit][f.field(n, "name")] = jvmDecl{f, n}
		}
	}
}

// ktImports lists the imports as (name, wildcard, node).
func ktImports(f *file, visit func(name string, wildcard bool, n *node)) {
	for _, n := range named(f.root) {
		if n.Kind() != "import" || n.NamedChildCount() == 0 {
			continue
		}
		visit(f.text(n.NamedChild(0)), strings.HasSuffix(strings.TrimSpace(f.text(n)), ".*"), n)
	}
}

func (k *kotlin) imports(f *file, t *tables) []facts.Import {
	var out []facts.Import
	ktImports(f, func(name string, wildcard bool, n *node) {
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
func ktPublic(f *file, n *node) bool {
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

// ktOverride reports whether a declaration has the override modifier.
func ktOverride(f *file, n *node) bool {
	for _, c := range named(n) {
		if c.Kind() != "modifiers" {
			continue
		}
		for _, m := range named(c) {
			if f.text(m) == "override" {
				return true
			}
		}
	}
	return false
}

// ktInterface reports whether a class declaration is an interface.
func ktInterface(f *file, n *node) bool {
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

func (k *kotlin) typeSymbols(f *file, n *node, prefix string, visible bool) []facts.Symbol {
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
// annotations repair lifted off it.
func ktDoc(f *file, n *node) bool {
	_, doc := ktLeading(f, n)
	return doc || docBefore(f, n, "/**")
}

// repair blanks out the annotations this grammar reads as an expression of their own
// (annotated_expression) at the top level of a file. An annotation with arguments on a line of its
// own (@Suppress("x")) before a declaration may be read so, and with two of them the declaration
// after them is read as part of that expression and lost. Without them, the declaration reads as
// it should, and ktLeading finds them again.
func (*kotlin) repair(src []byte, root *node) ([]byte, []*node) {
	var lifted []*node
	var lift func(x *node)
	lift = func(x *node) {
		for i, c := range named(x) {
			switch c.Kind() {
			case "annotation":
				lifted = append(lifted, c)
				if next := x.NamedChild(uint(i) + 1); next != nil && next.Kind() == "parenthesized_expression" {
					lifted = append(lifted, next) // the annotation's arguments, read apart from it
				}
			case "annotated_expression":
				lift(c)
			}
		}
	}
	for _, n := range named(root) {
		if n.Kind() == "annotated_expression" {
			lift(n)
		}
	}
	if len(lifted) == 0 {
		return nil, nil
	}
	out := bytes.Clone(src)
	for _, n := range lifted {
		for i := n.StartByte(); i < n.EndByte(); i++ {
			if out[i] != '\n' && out[i] != '\r' {
				out[i] = ' '
			}
		}
	}
	return out, lifted
}

// ktLeading returns the annotations repair lifted off a declaration, by name, each as the node
// holding its arguments, and whether a /** */ comment ends on the line before the first of them.
// They are the ones between the declaration and the code or comment before it.
func ktLeading(f *file, n *node) (map[string]*node, bool) {
	anns := map[string]*node{}
	prev, from := n.PrevNamedSibling(), uint(0)
	switch {
	case prev != nil:
		from = prev.EndByte()
	case n.Parent() != nil:
		from = n.Parent().StartByte()
	}
	var first *node
	for _, a := range f.lifted {
		if a.Kind() != "annotation" || a.StartByte() < from || a.EndByte() > n.StartByte() {
			continue
		}
		anns[ktAnnotationName(f, a)] = ktAnnotationHolder(a)
		if first == nil || a.StartByte() < first.StartByte() {
			first = a
		}
	}
	doc := first != nil && prev != nil && strings.Contains(prev.Kind(), "comment") &&
		strings.HasPrefix(strings.TrimSpace(f.text(prev)), "/**") && endLine(prev) >= line(first)-1
	return anns, doc
}

// ktAnnotationName is the simple name of an annotation's type.
func ktAnnotationName(f *file, a *node) string {
	name := ""
	walk(a, func(t *node) bool {
		if name == "" && t.Kind() == "user_type" {
			name = lastElem(f.text(t))
		}
		return name == ""
	})
	return name
}

// ktEntries are the constants of an enum class.
func ktEntries(f *file, n *node) []string {
	var out []string
	for _, m := range ktMembers(n) {
		if m.Kind() == "enum_entry" && m.NamedChildCount() > 0 {
			out = append(out, f.text(m.NamedChild(0)))
		}
	}
	return out
}

// ktMembers returns the declarations in a class or object body.
func ktMembers(n *node) []*node {
	for _, c := range named(n) {
		if c.Kind() == "class_body" || c.Kind() == "enum_class_body" {
			return named(c)
		}
	}
	return nil
}

func (k *kotlin) function(f *file, n *node, prefix string, kind facts.SymbolKind, visible bool) facts.Symbol {
	s := facts.Symbol{ID: prefix + "." + f.field(n, "name"), Kind: kind, Exported: visible && ktPublic(f, n), Doc: ktDoc(f, n),
		DocElsewhere: ktOverride(f, n)}
	params := ktParams(f, n)
	k.scopes[s.ID] = scopeOf(params)
	var body *node
	for _, c := range named(n) {
		if c.Kind() == "function_body" {
			body = c
		}
	}
	return funcSymbol(f, n, s, params, ktBranches.complexity(f, body))
}

func ktParams(f *file, n *node) []string {
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

// ktAnnotationHolder is the node holding a lifted annotation's arguments: the annotation, or the
// parenthesized expression this grammar may read them as after it.
func ktAnnotationHolder(a *node) *node {
	if next := a.NextNamedSibling(); next != nil && next.Kind() == "parenthesized_expression" {
		return next
	}
	return a
}
