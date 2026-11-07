//go:build cgo

package treesitter

import (
	"path"
	"regexp"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	tsx "github.com/tree-sitter/tree-sitter-typescript/bindings/go"

	fswalk "github.com/rerorets/hadome/internal/adapter/walk"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
)

// typescript reads TypeScript and JavaScript.
type typescript struct {
	project *tsProject
	files   map[string]*file     // every file of the version, by path
	modules map[string]*tsModule // what each file exports, read when first needed
	collide map[string]bool      // "<dir>.<name>" declared by two or more files of the directory
	scopes  map[string]int       // target ID -> position of its scope parameter
	// supports are the test support files read as a test's helpers, by path.
	supports map[string]*tsFile
}

// grammar parses .ts, .mts and .cts with the TypeScript grammar and every other file with the TSX
// grammar. Neither grammar reads all of them: TSX rejects the `<T>x` type assertion, which .ts files
// may use, and the TypeScript grammar rejects JSX, which .tsx and JavaScript files may use.
func (*typescript) grammar(p string) *sitter.Language {
	switch path.Ext(p) {
	case ".ts", ".mts", ".cts":
		return sitter.NewLanguage(tsx.LanguageTypescript())
	}
	return sitter.NewLanguage(tsx.LanguageTSX())
}

var tsBranches = branches{
	kinds: set("if_statement", "for_statement", "for_in_statement", "while_statement", "do_statement",
		"switch_case", "catch_clause", "ternary_expression"),
	ops: set("&&", "||", "??"),
}

var tsTables = &tables{
	calls: map[string]facts.HazardCategory{"Date.now": facts.HazardClock, "performance.now": facts.HazardClock,
		"setTimeout": facts.HazardSleep, "jest.mock": facts.HazardMock, "vi.mock": facts.HazardMock,
		"mock.module": facts.HazardMock, "vi.spyOn": facts.HazardMock, "jest.spyOn": facts.HazardMock,
		"vi.stubGlobal": facts.HazardMock},
	mocks: []string{"sinon", "jest-mock", "ts-mockito", "testdouble"},
	io: []string{"fs", "node:fs", "net", "node:net", "http", "node:http", "https", "node:https", "child_process",
		"node:child_process", "dgram", "node:dgram", "axios", "node-fetch", "undici",
		"ky", "got", "pg", "mysql2", "mongodb", "mongoose", "redis", "ioredis", "@prisma/client", "@aws-sdk", "@google-cloud"},
}

var tsDirective = regexp.MustCompile(`^(//\s*@ts-|//\s*eslint-|/\*\s*eslint|///\s*<reference|//\s*prettier-ignore)`)

var tsTestFile = regexp.MustCompile(`(\.(test|spec)\.[cm]?[jt]sx?$)|(^|/)__tests__/`)

// tsSupportFile matches the test support files besides those under walk.SupportDir: test_helpers.ts
// and the like, and the setup files of the test runners.
var tsSupportFile = regexp.MustCompile(`(?i:test[_-]?helpers?\.[cm]?[jt]sx?$)|(^|/)(setupTests|jest\.setup|vitest\.setup)\.[cm]?[jt]sx?$`)

// supportFile reports whether a file is test support: under a support directory or named like
// one, unless configuration says it is generated or product code.
func supportFile(f *file, named *regexp.Regexp) bool {
	return (fswalk.SupportDir(f.path) || named.MatchString(f.path)) && !f.generatedGlob && !f.productGlob
}

func (r *typescript) index(v engine.Version, files []*file) {
	r.files, r.modules, r.scopes, r.supports = map[string]*file{}, map[string]*tsModule{}, map[string]int{}, map[string]*tsFile{}
	r.project = newTSProject(v.FS, files)
	for _, f := range files {
		r.files[f.path] = f
		f.support = supportFile(f, tsSupportFile)
		f.test = tsTestFile.MatchString(f.path) || f.support || f.testGlob
		f.unit, f.unitPath = path.Dir(f.path), path.Dir(f.path)
	}
	r.collisions(files)
}

func (r *typescript) imports(f *file, t *tables) []facts.Import {
	var out []facts.Import
	globals := map[string]bool{}
	walk(f.root, func(n *sitter.Node) bool {
		if name := tsGlobalIO(f, n); name != "" && !globals[name] {
			globals[name] = true
			out = append(out, facts.Import{Path: f.path, Line: line(n), FromUnit: f.unit, ToUnit: name, IO: true, Global: true})
		}
		spec := ""
		switch n.Kind() {
		case "import_statement", "export_statement":
			if typeOnly(n) {
				return false
			}
			spec = unquote(f.field(n, "source"))
		case "call_expression":
			if fn := f.field(n, "function"); fn == "require" || fn == "import" {
				args := named(n.ChildByFieldName("arguments"))
				switch {
				case len(args) == 1 && literal(args[0]):
					spec = unquote(f.text(args[0]))
				case len(args) > 0:
					out = append(out, unknownImport(f, n))
					return false
				}
			}
		}
		if spec == "" {
			return true
		}
		// A specifier that looks like the repository's own but names no file is internal too, and its
		// unit is the directory of the path it names.
		p, kind := r.project.resolve(f.path, spec)
		switch kind {
		case tsExternal:
			out = append(out, facts.Import{Path: f.path, Line: line(n), FromUnit: f.unit, ToUnit: p, IO: t.isIO(spec)})
		case tsUnread:
			out = append(out, unreadImport(f, n, spec))
		default:
			out = append(out, facts.Import{Path: f.path, Line: line(n), FromUnit: f.unit, ToUnit: path.Dir(p), Internal: true})
		}
		return false
	})
	return out
}

// tsGlobals are the APIs a browser or Node.js provides without an import that perform input or
// output, by the name a call, a new or a member access uses.
var tsGlobals = set("fetch", "navigator.sendBeacon", "WebSocket", "XMLHttpRequest", "EventSource",
	"localStorage", "sessionStorage", "indexedDB")

// tsGlobalIO returns the global API that performs input or output this node uses, or "": a call of
// fetch, a new WebSocket, a member of localStorage, with or without window., globalThis. or self.
// in front. Only the name is read: a local variable of the same name counts too.
func tsGlobalIO(f *file, n *sitter.Node) string {
	name := ""
	switch n.Kind() {
	case "call_expression":
		name = f.field(n, "function")
	case "new_expression":
		name = f.field(n, "constructor")
	case "member_expression":
		name = f.field(n, "object")
	default:
		return ""
	}
	for _, p := range []string{"window.", "globalThis.", "self."} {
		name = strings.TrimPrefix(name, p)
	}
	if tsGlobals[name] {
		return name
	}
	return ""
}

// typeOnly reports an import or export that brings in only types: `import type`, `export type
// {...} from`, or named imports that are all marked `type`. TypeScript erases them, so they are no
// dependency of the running code.
func typeOnly(n *sitter.Node) bool {
	if hasToken(n, "type") {
		return true
	}
	clause := (*sitter.Node)(nil)
	for _, c := range named(n) {
		if c.Kind() == "import_clause" {
			clause = c
		}
	}
	if clause == nil {
		return false
	}
	specs := 0
	for _, c := range named(clause) {
		if c.Kind() != "named_imports" {
			return false // a default or namespace import brings in a value
		}
		for _, s := range named(c) {
			if s.Kind() == "import_specifier" {
				if !hasToken(s, "type") {
					return false
				}
				specs++
			}
		}
	}
	return specs > 0
}

// hasToken reports whether n has an unnamed child of this kind, such as the keyword `type`.
func hasToken(n *sitter.Node, kind string) bool {
	for i := uint(0); i < n.ChildCount(); i++ {
		if c := n.Child(i); !c.IsNamed() && c.Kind() == kind {
			return true
		}
	}
	return false
}

func (r *typescript) symbols(f *file) []facts.Symbol {
	var out []facts.Symbol
	for _, top := range named(f.root) {
		decl, exported := top, false
		if top.Kind() == "export_statement" {
			if d := top.ChildByFieldName("declaration"); d != nil {
				decl, exported = d, true
			}
		}
		doc := docBefore(f, top, "/**")
		out = append(out, r.declaration(f, decl, exported, doc)...)
	}
	return out
}

func (r *typescript) declaration(f *file, n *sitter.Node, exported, doc bool) []facts.Symbol {
	id := r.symbolID(f.path, f.field(n, "name"))
	typ := facts.Symbol{ID: id, Kind: facts.SymbolType, Path: f.path, Line: line(n), EndLine: endLine(n), Exported: exported, Doc: doc}
	switch n.Kind() {
	case "function_declaration", "generator_function_declaration":
		return []facts.Symbol{r.function(f, n, n, facts.Symbol{ID: id, Kind: facts.SymbolFunction, Exported: exported, Doc: doc})}
	case "lexical_declaration", "variable_declaration":
		var out []facts.Symbol
		for _, d := range named(n) {
			v := d.ChildByFieldName("value")
			if d.Kind() == "variable_declarator" && v != nil && (v.Kind() == "arrow_function" || v.Kind() == "function_expression") {
				s := facts.Symbol{ID: r.symbolID(f.path, f.field(d, "name")), Kind: facts.SymbolFunction, Exported: exported, Doc: doc}
				out = append(out, r.function(f, d, v, s))
			}
		}
		return out
	case "class_declaration", "abstract_class_declaration":
		return append([]facts.Symbol{typ}, r.methods(f, n, id, exported)...)
	case "interface_declaration":
		walk(n.ChildByFieldName("body"), func(x *sitter.Node) bool {
			if x.Kind() == "method_signature" {
				typ.Methods++
			}
			return true
		})
		typ.Kind = facts.SymbolInterface
		return []facts.Symbol{typ}
	case "type_alias_declaration", "enum_declaration":
		return []facts.Symbol{typ}
	}
	return nil
}

// methods lists a class's methods; private ones (#name, or the private modifier) are not exported.
func (r *typescript) methods(f *file, class *sitter.Node, id string, exported bool) []facts.Symbol {
	var out []facts.Symbol
	for _, m := range named(class.ChildByFieldName("body")) {
		if m.Kind() != "method_definition" {
			continue
		}
		name := f.field(m, "name")
		private := strings.HasPrefix(name, "#") || strings.HasPrefix(f.text(m), "private ")
		s := facts.Symbol{ID: id + "." + name, Kind: facts.SymbolMethod, Exported: exported && !private, Doc: docBefore(f, m, "/**"),
			DocElsewhere: name == "constructor" || tsOverride(m)}
		out = append(out, r.function(f, m, m, s))
	}
	return out
}

// tsOverride reports whether a method is marked override.
func tsOverride(m *sitter.Node) bool {
	for _, c := range named(m) {
		if c.Kind() == "override_modifier" {
			return true
		}
	}
	return false
}

// function completes a function's symbol: at is where it is declared, fn holds its parameters and body.
func (r *typescript) function(f *file, at, fn *sitter.Node, s facts.Symbol) facts.Symbol {
	var params []string
	if ps := fn.ChildByFieldName("parameters"); ps != nil {
		for _, p := range named(ps) {
			if name := f.field(p, "pattern"); name != "" {
				params = append(params, name)
			} else if p.Kind() == "identifier" {
				params = append(params, f.text(p))
			}
		}
	} else if p := fn.ChildByFieldName("parameter"); p != nil {
		params = []string{f.text(p)}
	}
	r.scopes[s.ID] = scopeOf(params)
	return funcSymbol(f, at, s, params, tsBranches.complexity(f, fn.ChildByFieldName("body")))
}
