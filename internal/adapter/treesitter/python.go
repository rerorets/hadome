//go:build cgo

package treesitter

import (
	"path"
	"regexp"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	py "github.com/tree-sitter/tree-sitter-python/bindings/go"

	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
)

// python reads Python. A unit is a package, named by its directory; a symbol's ID is its module's
// path and its name ("src/app/store.append"), so that configured targets match it like Go's.
type python struct {
	exists map[string]bool // files
	dirs   map[string]bool // directories holding files
	scopes map[string]int
}

func (*python) grammar() *sitter.Language { return sitter.NewLanguage(py.Language()) }

var pyBranches = branches{
	kinds: set("if_statement", "elif_clause", "for_statement", "while_statement", "except_clause",
		"conditional_expression", "case_clause", "for_in_clause", "if_clause"),
	ops: set("and", "or"),
}

var pyTables = &tables{
	calls: map[string]facts.HazardCategory{
		"datetime.now": facts.HazardClock, "datetime.utcnow": facts.HazardClock, "datetime.datetime.now": facts.HazardClock,
		"datetime.datetime.utcnow": facts.HazardClock, "date.today": facts.HazardClock, "datetime.date.today": facts.HazardClock,
		"time.time": facts.HazardClock, "time.monotonic": facts.HazardClock,
		"time.sleep": facts.HazardSleep, "asyncio.sleep": facts.HazardSleep,
	},
	mocks: []string{"unittest.mock", "mock", "pytest_mock"},
	io: []string{"socket", "os", "subprocess", "shutil", "sqlite3", "requests", "httpx", "aiohttp", "urllib.request",
		"http.client", "psycopg", "psycopg2", "pymysql", "pymongo", "redis", "boto3"},
}

var pyDirective = regexp.MustCompile(`^#\s*(type:|noqa|pylint:|fmt:|pragma|mypy:|isort:)|^#!`)

var pyTestFile = regexp.MustCompile(`(^|/)(test_[^/]*|[^/]*_test)\.py$`)

func (r *python) index(_ engine.Version, files []*file) {
	r.exists, r.dirs, r.scopes = map[string]bool{}, map[string]bool{}, map[string]int{}
	for _, f := range files {
		r.exists[f.path] = true
		for d := path.Dir(f.path); d != "." && !r.dirs[d]; d = path.Dir(d) {
			r.dirs[d] = true
		}
		f.test = pyTestFile.MatchString(f.path) || f.testGlob
		f.unit, f.unitPath = path.Dir(f.path), path.Dir(f.path)
	}
}

// module returns the module path of a file: its path without .py, or its package for __init__.py.
func module(p string) string {
	if path.Base(p) == "__init__.py" {
		return path.Dir(p)
	}
	return strings.TrimSuffix(p, ".py")
}

// resolve finds a dotted module in the repository, from the root or from src/. It returns the
// module's path ("app/store"), its unit (the package directory), and whether it was found.
func (r *python) resolve(dotted string) (mod, unit string, ok bool) {
	rel := strings.ReplaceAll(dotted, ".", "/")
	for _, root := range []string{"", "src/"} {
		p := root + rel
		switch {
		case r.exists[p+".py"]:
			return p, path.Dir(p), true
		case r.dirs[p]:
			return p, p, true
		}
	}
	return "", "", false
}

// relative resolves "from ..x import y": dots climb from the importing file's package.
func (r *python) relative(from, prefix, dotted string) (mod, unit string, ok bool) {
	dir := path.Dir(from)
	for i := 1; i < len(prefix); i++ {
		dir = path.Dir(dir)
	}
	p := path.Join(dir, strings.ReplaceAll(dotted, ".", "/"))
	if r.exists[p+".py"] {
		return p, path.Dir(p), true
	}
	return p, p, r.dirs[p] || dotted == ""
}

// pyImport is one module a file imports, and what it binds.
type pyImport struct {
	at       *sitter.Node
	dotted   string // as written, for external modules
	mod      string // the module's path when internal
	unit     string
	internal bool
	names    map[string]string // local name -> name in the module ("" binds the module itself)
}

func (r *python) parseImports(f *file) []pyImport {
	var out []pyImport
	walk(f.root, func(n *sitter.Node) bool {
		switch n.Kind() {
		case "import_statement":
			for _, c := range named(n) {
				name, local := f.text(c), f.text(c)
				if c.Kind() == "aliased_import" {
					name, local = f.field(c, "name"), f.field(c, "alias")
				}
				im := pyImport{at: n, dotted: name, names: map[string]string{local: ""}}
				im.mod, im.unit, im.internal = r.resolve(name)
				out = append(out, im)
			}
			return false
		case "import_from_statement":
			out = append(out, r.fromImport(f, n))
			return false
		}
		return true
	})
	return out
}

func (r *python) fromImport(f *file, n *sitter.Node) pyImport {
	src := n.ChildByFieldName("module_name")
	im := pyImport{at: n, dotted: f.text(src), names: map[string]string{}}
	if src.Kind() == "relative_import" {
		prefix, dotted := "", ""
		for _, c := range named(src) {
			if c.Kind() == "import_prefix" {
				prefix = f.text(c)
			} else {
				dotted = f.text(c)
			}
		}
		im.mod, im.unit, im.internal = r.relative(f.path, prefix, dotted)
	} else {
		im.mod, im.unit, im.internal = r.resolve(im.dotted)
	}
	for i := uint(0); i < n.ChildCount(); i++ {
		c := n.Child(i)
		if n.FieldNameForChild(uint32(i)) != "name" {
			continue
		}
		name, local := f.text(c), f.text(c)
		if c.Kind() == "aliased_import" {
			name, local = f.field(c, "name"), f.field(c, "alias")
		}
		im.names[local] = name
	}
	return im
}

func (r *python) imports(f *file, t *tables) []facts.Import {
	var out []facts.Import
	for _, im := range r.parseImports(f) {
		to := im.dotted
		if im.internal {
			to = im.unit
		}
		out = append(out, facts.Import{Path: f.path, Line: line(im.at), FromUnit: f.unit, ToUnit: to,
			Internal: im.internal, IO: !im.internal && t.isIO(im.dotted)})
	}
	for _, c := range dynamicCalls(f, set("call"), pyDynamic, nil) {
		if c.spec == "" {
			out = append(out, unknownImport(f, c.at))
			continue
		}
		_, unit, internal := r.resolve(c.spec)
		to := c.spec
		if internal {
			to = unit
		}
		out = append(out, facts.Import{Path: f.path, Line: line(c.at), FromUnit: f.unit, ToUnit: to,
			Internal: internal, IO: !internal && t.isIO(c.spec)})
	}
	return out
}

// pyDynamic are the calls that import the module their argument names.
var pyDynamic = set("importlib.import_module", "import_module", "__import__")

func (r *python) symbols(f *file) []facts.Symbol {
	mod := module(f.path)
	var out []facts.Symbol
	for _, n := range named(f.root) {
		def := definition(n)
		switch def.Kind() {
		case "function_definition":
			out = append(out, r.function(f, def, mod+"."+f.field(def, "name"), facts.SymbolFunction, true))
		case "class_definition":
			name := f.field(def, "name")
			id := mod + "." + name
			out = append(out, facts.Symbol{ID: id, Kind: facts.SymbolType, Path: f.path, Line: line(def), EndLine: endLine(def),
				Exported: !strings.HasPrefix(name, "_"), Doc: docstring(def)})
			for _, m := range named(def.ChildByFieldName("body")) {
				if m = definition(m); m.Kind() == "function_definition" {
					out = append(out, r.function(f, m, id+"."+f.field(m, "name"), facts.SymbolMethod, !strings.HasPrefix(name, "_")))
				}
			}
		}
	}
	return out
}

// definition unwraps a decorated definition.
func definition(n *sitter.Node) *sitter.Node {
	if n.Kind() == "decorated_definition" {
		if d := n.ChildByFieldName("definition"); d != nil {
			return d
		}
	}
	return n
}

// docstring reports whether a function or class body starts with a string.
func docstring(def *sitter.Node) bool {
	body := def.ChildByFieldName("body")
	if body == nil || body.NamedChildCount() == 0 {
		return false
	}
	first := body.NamedChild(0)
	return first.Kind() == "expression_statement" && first.NamedChildCount() > 0 && first.NamedChild(0).Kind() == "string"
}

// function completes a def's symbol; visible says whether its class (if any) is public.
func (r *python) function(f *file, def *sitter.Node, id string, kind facts.SymbolKind, visible bool) facts.Symbol {
	params := pyParams(f, def)
	r.scopes[id] = scopeOf(params)
	name := f.field(def, "name")
	s := facts.Symbol{ID: id, Kind: kind, Exported: visible && !strings.HasPrefix(name, "_"), Doc: docstring(def)}
	return funcSymbol(f, def, s, params, pyBranches.complexity(f, def.ChildByFieldName("body")))
}

// pyParams lists a def's parameter names, without self and cls.
func pyParams(f *file, def *sitter.Node) []string {
	var out []string
	for _, p := range named(def.ChildByFieldName("parameters")) {
		name := f.text(p)
		if p.Kind() != "identifier" {
			name = f.field(p, "name")
			if name == "" && p.NamedChildCount() > 0 {
				name = f.text(p.NamedChild(0))
			}
		}
		if name != "self" && name != "cls" && name != "" {
			out = append(out, name)
		}
	}
	return out
}
