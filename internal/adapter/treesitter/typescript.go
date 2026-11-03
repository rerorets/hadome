//go:build cgo

package treesitter

import (
	"encoding/json"
	"io/fs"
	"path"
	"regexp"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	tsx "github.com/tree-sitter/tree-sitter-typescript/bindings/go"

	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
)

// typescript reads TypeScript and JavaScript with the TSX grammar, which accepts both.
type typescript struct {
	exists  map[string]bool
	aliases map[string]string // "@/" -> "src/", from tsconfig.json compilerOptions.paths
	scopes  map[string]int    // target ID -> position of its scope parameter
}

func (*typescript) grammar() *sitter.Language { return sitter.NewLanguage(tsx.LanguageTSX()) }

var tsBranches = branches{
	kinds: set("if_statement", "for_statement", "for_in_statement", "while_statement", "do_statement",
		"switch_case", "catch_clause", "ternary_expression"),
	ops: set("&&", "||", "??"),
}

var tsTables = &tables{
	calls: map[string]facts.HazardCategory{"Date.now": facts.HazardClock, "performance.now": facts.HazardClock,
		"setTimeout": facts.HazardSleep, "jest.mock": facts.HazardMock, "vi.mock": facts.HazardMock},
	mocks: []string{"sinon", "jest-mock", "ts-mockito", "testdouble"},
	io: []string{"fs", "node:fs", "net", "node:net", "http", "node:http", "https", "node:https", "child_process",
		"node:child_process", "dgram", "node:dgram", "axios", "node-fetch", "pg", "mysql2", "mongodb", "redis", "ioredis"},
}

var tsDirective = regexp.MustCompile(`^(//\s*@ts-|//\s*eslint-|/\*\s*eslint|///\s*<reference|//\s*prettier-ignore)`)

var tsTestFile = regexp.MustCompile(`(\.(test|spec)\.[cm]?[jt]sx?$)|(^|/)__tests__/`)

func (r *typescript) index(v engine.Version, files []*file) {
	r.exists, r.aliases, r.scopes = map[string]bool{}, tsAliases(v.FS), map[string]int{}
	for _, f := range files {
		r.exists[f.path] = true
		f.test = tsTestFile.MatchString(f.path)
		f.unit, f.unitPath = path.Dir(f.path), path.Dir(f.path)
	}
}

// tsAliases reads the path aliases of the root tsconfig.json: {"@/*": ["src/*"]} maps "@/" to "src/".
func tsAliases(fsys fs.FS) map[string]string {
	out := map[string]string{}
	data, err := fs.ReadFile(fsys, "tsconfig.json")
	if err != nil {
		return out
	}
	var cfg struct {
		CompilerOptions struct {
			BaseURL string              `json:"baseUrl"`
			Paths   map[string][]string `json:"paths"`
		} `json:"compilerOptions"`
	}
	if json.Unmarshal(jsonComments.ReplaceAll(data, []byte("$1")), &cfg) != nil {
		return out
	}
	for alias, targets := range cfg.CompilerOptions.Paths {
		if len(targets) > 0 {
			to := path.Join(cfg.CompilerOptions.BaseURL, strings.TrimSuffix(targets[0], "*"))
			out[strings.TrimSuffix(alias, "*")] = strings.TrimPrefix(to+"/", "./")
		}
	}
	return out
}

// jsonComments matches a // comment after anything that is not inside a string; tsconfig.json
// allows them, encoding/json does not.
var jsonComments = regexp.MustCompile(`(?m)^((?:[^"/\n]|"(?:[^"\\\n]|\\.)*")*)//.*$`)

// resolve turns an import specifier into the imported unit, and whether it is the repository's.
func (r *typescript) resolve(from, spec string) (string, bool) {
	p, internal := "", false
	switch {
	case strings.HasPrefix(spec, "."):
		p, internal = path.Join(path.Dir(from), spec), true
	case strings.HasPrefix(spec, "/"):
		p, internal = strings.TrimPrefix(spec, "/"), true
	default:
		for alias, to := range r.aliases {
			if rest, ok := strings.CutPrefix(spec, alias); ok {
				p, internal = path.Join(to, rest), true
			}
		}
	}
	if !internal {
		parts := strings.SplitN(spec, "/", 3)
		if strings.HasPrefix(spec, "@") && len(parts) > 1 {
			return parts[0] + "/" + parts[1], false
		}
		return parts[0], false
	}
	for _, ext := range append([]string{""}, extensions["typescript"]...) {
		if r.exists[p+ext] {
			return path.Dir(p + ext), true
		}
		if r.exists[p+"/index"+ext] {
			return p, true
		}
	}
	return path.Dir(p), true
}

func (r *typescript) imports(f *file, t *tables) []facts.Import {
	var out []facts.Import
	walk(f.root, func(n *sitter.Node) bool {
		spec := ""
		switch n.Kind() {
		case "import_statement", "export_statement":
			spec = unquote(f.field(n, "source"))
		case "call_expression":
			if fn := f.field(n, "function"); fn == "require" || fn == "import" {
				if args := named(n.ChildByFieldName("arguments")); len(args) == 1 && args[0].Kind() == "string" {
					spec = unquote(f.text(args[0]))
				}
			}
		}
		if spec == "" {
			return true
		}
		unit, internal := r.resolve(f.path, spec)
		out = append(out, facts.Import{Path: f.path, Line: line(n), FromUnit: f.unit, ToUnit: unit,
			Internal: internal, IO: !internal && t.isIO(spec)})
		return false
	})
	return out
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
	id := f.unit + "." + f.field(n, "name")
	typ := facts.Symbol{ID: id, Kind: facts.SymbolType, Path: f.path, Line: line(n), EndLine: endLine(n), Exported: exported, Doc: doc}
	switch n.Kind() {
	case "function_declaration", "generator_function_declaration":
		return []facts.Symbol{r.function(f, n, n, facts.Symbol{ID: id, Kind: facts.SymbolFunction, Exported: exported, Doc: doc})}
	case "lexical_declaration", "variable_declaration":
		var out []facts.Symbol
		for _, d := range named(n) {
			v := d.ChildByFieldName("value")
			if d.Kind() == "variable_declarator" && v != nil && (v.Kind() == "arrow_function" || v.Kind() == "function_expression") {
				s := facts.Symbol{ID: f.unit + "." + f.field(d, "name"), Kind: facts.SymbolFunction, Exported: exported, Doc: doc}
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
		s := facts.Symbol{ID: id + "." + name, Kind: facts.SymbolMethod, Exported: exported && !private, Doc: docBefore(f, m, "/**")}
		out = append(out, r.function(f, m, m, s))
	}
	return out
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
