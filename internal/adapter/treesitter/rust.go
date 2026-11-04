//go:build cgo

package treesitter

import (
	"path"
	"regexp"
	"strings"
	"unicode"

	sitter "github.com/tree-sitter/go-tree-sitter"
	rustgrammar "github.com/tree-sitter/tree-sitter-rust/bindings/go"

	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
)

// rust reads Rust. A unit is a module, named by its location: src/store for src/store.rs or
// src/store/mod.rs, src for lib.rs and main.rs.
type rust struct {
	modules map[string]bool     // module locations
	crates  map[string]bool     // directories holding a Cargo.toml
	defs    map[string][]string // module location -> names of its top-level items, for use ...::*
	scopes  map[string]int
	// automocks are the types mockall's #[automock] generates (MockStore for trait Store). The
	// attribute sits on product code, so a test that uses one imports nothing that says mock.
	automocks map[string]bool
}

func (*rust) grammar() *sitter.Language { return sitter.NewLanguage(rustgrammar.Language()) }

var rustBranches = branches{
	kinds: set("if_expression", "while_expression", "loop_expression", "for_expression", "match_arm"),
	ops:   set("&&", "||"),
}

var rustTables = &tables{
	calls: map[string]facts.HazardCategory{
		"SystemTime::now": facts.HazardClock, "Instant::now": facts.HazardClock, "Utc::now": facts.HazardClock,
		"Local::now": facts.HazardClock, "std::time::SystemTime::now": facts.HazardClock, "std::time::Instant::now": facts.HazardClock,
		"chrono::Utc::now": facts.HazardClock, "thread::sleep": facts.HazardSleep, "std::thread::sleep": facts.HazardSleep,
		"tokio::time::sleep": facts.HazardSleep, "mock!": facts.HazardMock,
	},
	mocks: []string{"mockall", "mockito", "wiremock"},
	io: []string{"std::fs", "std::net", "std::process", "std::io::stdin", "tokio::fs", "tokio::net", "tokio::process",
		"reqwest", "hyper", "sqlx", "diesel", "rusqlite", "redis", "mongodb"},
}

var rustDirective = regexp.MustCompile(`^//\s*(rustfmt::|clippy::|@generated)`)

var rustTestDir = regexp.MustCompile(`(^|/)(tests|benches)/`)

func (r *rust) index(v engine.Version, files []*file) {
	r.modules, r.crates, r.defs, r.scopes = map[string]bool{}, map[string]bool{}, map[string][]string{}, map[string]int{}
	r.automocks = map[string]bool{}
	paths, _ := fsFiles(v)
	for _, p := range paths {
		if path.Base(p) == "Cargo.toml" {
			r.crates[path.Dir(p)] = true
		}
	}
	for _, f := range files {
		f.test = rustTestDir.MatchString(f.path)
		f.unit = rustModule(f.path)
		f.unitPath = f.unit
		r.modules[f.unit] = true
		walk(f.root, func(n *sitter.Node) bool {
			if (n.Kind() == "trait_item" || n.Kind() == "impl_item" || rustTypes[n.Kind()]) && automocked(f, n) {
				name := f.field(n, "name")
				if n.Kind() == "impl_item" {
					name = strings.SplitN(f.field(n, "type"), "<", 2)[0]
				}
				r.automocks["Mock"+name] = true
			}
			return true
		})
	}
}

// automocked reports an item under #[automock], #[mockall::automock] or #[cfg_attr(test, automock)].
func automocked(f *file, n *sitter.Node) bool {
	for _, a := range attributes(f, n) {
		if strings.Contains(a, "automock") {
			return true
		}
	}
	return false
}

// rustModule is the location of the module a file defines.
func rustModule(p string) string {
	switch path.Base(p) {
	case "lib.rs", "main.rs", "mod.rs":
		return path.Dir(p)
	}
	return strings.TrimSuffix(p, ".rs")
}

// crateSrc returns the src directory of the crate holding a file.
func (r *rust) crateSrc(p string) string {
	for d := path.Dir(p); ; d = path.Dir(d) {
		if r.crates[d] {
			return path.Join(d, "src")
		}
		if d == "." || d == "/" {
			return "src"
		}
	}
}

// resolve turns a use path ("crate::store::append") into the deepest module it names and whether
// it is the repository's. External paths keep their own text as the unit.
func (r *rust) resolve(from string, segs []string) (string, bool) {
	loc := ""
	switch segs[0] {
	case "crate":
		loc = r.crateSrc(from)
	case "self":
		loc = rustModule(from)
	case "super":
		loc = path.Dir(rustModule(from))
		for len(segs) > 1 && segs[1] == "super" {
			loc, segs = path.Dir(loc), segs[1:]
		}
	default:
		last := segs[len(segs)-1]
		if len(segs) > 1 && (unicode.IsUpper(rune(last[0])) || last == "*") {
			segs = segs[:len(segs)-1]
		}
		return strings.Join(segs, "::"), false
	}
	for _, s := range segs[1:] {
		if !r.modules[loc+"/"+s] {
			break
		}
		loc += "/" + s
	}
	return loc, true
}

// useLeaf is one path a use declaration brings in, with the name it binds ("" for a wildcard).
type useLeaf struct {
	segs  []string
	local string
	at    *sitter.Node
}

// uses flattens the use declarations directly under n.
func uses(f *file, n *sitter.Node) []useLeaf {
	var out []useLeaf
	for _, c := range named(n) {
		if c.Kind() == "use_declaration" {
			flatten(f, c.ChildByFieldName("argument"), nil, c, &out)
		}
	}
	return out
}

func flatten(f *file, n *sitter.Node, prefix []string, at *sitter.Node, out *[]useLeaf) {
	segs := func(x *sitter.Node) []string {
		return append(append([]string{}, prefix...), strings.Split(f.text(x), "::")...)
	}
	switch n.Kind() {
	case "scoped_use_list":
		p := prefix
		if pn := n.ChildByFieldName("path"); pn != nil {
			p = segs(pn)
		}
		for _, item := range named(n.ChildByFieldName("list")) {
			flatten(f, item, p, at, out)
		}
	case "use_list":
		for _, item := range named(n) {
			flatten(f, item, prefix, at, out)
		}
	case "use_wildcard":
		var s []string
		if n.NamedChildCount() > 0 {
			s = segs(n.NamedChild(0))
		} else {
			s = prefix
		}
		*out = append(*out, useLeaf{segs: append(s, "*"), at: at})
	case "use_as_clause":
		s := segs(n.ChildByFieldName("path"))
		*out = append(*out, useLeaf{segs: s, local: f.field(n, "alias"), at: at})
	default:
		s := segs(n)
		*out = append(*out, useLeaf{segs: s, local: s[len(s)-1], at: at})
	}
}

func (r *rust) imports(f *file, t *tables) []facts.Import {
	var out []facts.Import
	walk(f.root, func(n *sitter.Node) bool {
		if n.Kind() == "mod_item" && testModule(f, n) {
			return false
		}
		for _, u := range uses(f, n) {
			unit, internal := r.resolve(f.path, u.segs)
			out = append(out, facts.Import{Path: f.path, Line: line(u.at), FromUnit: f.unit, ToUnit: unit,
				Internal: internal, IO: !internal && t.isIO(strings.Join(u.segs, "::"))})
		}
		return n.Kind() == "source_file" || n.Kind() == "mod_item" || n.Kind() == "declaration_list"
	})
	return out
}

// attributes returns the attributes written right before n, innermost first.
func attributes(f *file, n *sitter.Node) []string {
	var out []string
	for p := n.PrevSibling(); p != nil && (p.Kind() == "attribute_item" || strings.Contains(p.Kind(), "comment")); p = p.PrevSibling() {
		if p.Kind() == "attribute_item" {
			out = append(out, strings.TrimSuffix(strings.TrimPrefix(f.text(p), "#["), "]"))
		}
	}
	return out
}

// testModule reports a module compiled only for tests: #[cfg(test)].
func testModule(f *file, n *sitter.Node) bool {
	for _, a := range attributes(f, n) {
		if strings.ReplaceAll(a, " ", "") == "cfg(test)" {
			return true
		}
	}
	return false
}

// rustDoc reports a /// or /** doc comment before n, above its attributes.
func rustDoc(f *file, n *sitter.Node) bool {
	for p := n.PrevSibling(); p != nil; p = p.PrevSibling() {
		switch {
		case p.Kind() == "attribute_item":
		case strings.Contains(p.Kind(), "comment"):
			t := f.text(p)
			return strings.HasPrefix(t, "///") || strings.HasPrefix(t, "/**")
		default:
			return false
		}
	}
	return false
}
