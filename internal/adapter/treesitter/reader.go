//go:build cgo

package treesitter

import (
	"io/fs"
	"regexp"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
)

// file is one parsed file.
type file struct {
	path string
	src  []byte
	root *sitter.Node
	test bool // a test file: every function in it is test code
	// testGlob and generatedGlob are set from configuration before the language indexes the file.
	testGlob, generatedGlob bool
	unit                    string // the unit's ID, as imports name it
	unitPath                string // the unit's location, as diagrams name it
}

func (f *file) text(n *sitter.Node) string { return n.Utf8Text(f.src) }

func line(n *sitter.Node) int    { return int(n.StartPosition().Row) + 1 }
func endLine(n *sitter.Node) int { return int(n.EndPosition().Row) + 1 }

// reader is what one language adds to the shared walk.
type reader interface {
	// grammar is the grammar a file at path is parsed with.
	grammar(path string) *sitter.Language
	// index learns what the language needs about the whole version (packages, modules, aliases)
	// before any file is read. It sets each file's test, unit and unitPath.
	index(v engine.Version, files []*file)
	imports(f *file, t *tables) []facts.Import
	symbols(f *file) []facts.Symbol
	// tests reads the tests of a file; a source file may hold some too (Rust's #[cfg(test)]).
	tests(f *file, t *tables) []facts.Test
}

// lang is one tree-sitter language as the analyzer sees it.
type lang struct {
	name   string
	r      reader
	tables *tables // the built-in hazard and io tables
	breaks *breaks // where hadome mutate breaks the language's code
	// directive reports whether a comment is an instruction to a tool rather than prose.
	directive func(text string) bool
}

// Name is the language's name in configuration.
func (l *lang) Name() string { return l.name }

// Claims reports whether the file is in the language.
func (l *lang) Claims(p string) bool { return claims(l.name, p) }

// Analyze reads the language's files of the version.
func (l *lang) Analyze(v engine.Version, c config.Config, paths []string) (*facts.Analysis, error) {
	parser := sitter.NewParser()
	defer parser.Close()
	var files []*file
	for _, p := range paths {
		src, err := fs.ReadFile(v.FS, p)
		if err != nil {
			return nil, err
		}
		tree, err := l.parse(parser, p, src)
		if err != nil {
			return nil, err
		}
		defer tree.Close()
		gen := config.MatchesAny(c.GeneratedFiles, p)
		files = append(files, &file{path: p, src: src, root: tree.RootNode(),
			testGlob: !gen && config.MatchesAny(c.TestFiles, p), generatedGlob: gen})
	}
	l.r.index(v, files)
	t := l.tables.with(c)
	a := &facts.Analysis{}
	// Symbols come first: tests need to know each target's parameters.
	for _, f := range files {
		cs := comments(f, l.directive)
		role := facts.RoleSource
		switch {
		case f.test:
			role = facts.RoleTest
		case f.generatedGlob || generated(cs):
			role = facts.RoleGenerated
		}
		a.Files = append(a.Files, facts.File{Path: f.path, Language: l.name, Role: role,
			Lines: strings.Count(string(f.src), "\n") + 1, Unit: f.unit, UnitPath: f.unitPath})
		a.Comments = append(a.Comments, cs...)
		if !f.test {
			a.Imports = append(a.Imports, l.r.imports(f, t)...)
		}
		if role == facts.RoleSource {
			a.Symbols = append(a.Symbols, l.r.symbols(f)...)
		}
	}
	for _, f := range files {
		a.Tests = append(a.Tests, l.r.tests(f, t)...)
	}
	return a, nil
}

// parse parses one file with the grammar its path calls for.
func (l *lang) parse(parser *sitter.Parser, p string, src []byte) (*sitter.Tree, error) {
	if err := parser.SetLanguage(l.r.grammar(p)); err != nil {
		return nil, err
	}
	return parser.Parse(src, nil), nil
}

var generatedMark = regexp.MustCompile(`(?i)(@generated|code generated .* do not edit|auto-generated)`)

func generated(cs []facts.Comment) bool {
	for i, c := range cs {
		if i > 2 {
			break
		}
		if generatedMark.MatchString(c.Text) {
			return true
		}
	}
	return false
}

// comments lists the comments of a file, without their delimiters.
func comments(f *file, directive func(string) bool) []facts.Comment {
	var out []facts.Comment
	walk(f.root, func(n *sitter.Node) bool {
		if !strings.Contains(n.Kind(), "comment") {
			return true
		}
		raw := f.text(n)
		out = append(out, facts.Comment{Path: f.path, Line: line(n), Text: stripDelimiters(raw), Directive: directive(raw)})
		return false
	})
	return out
}

func stripDelimiters(s string) string {
	s = strings.TrimSpace(s)
	for _, p := range []string{"///", "//!", "//", "/**", "/*", "#"} {
		if strings.HasPrefix(s, p) {
			s = s[len(p):]
			break
		}
	}
	return strings.TrimSpace(strings.TrimSuffix(s, "*/"))
}

// walk visits n and its descendants depth first; visit returns false to skip a node's children.
func walk(n *sitter.Node, visit func(*sitter.Node) bool) {
	if n == nil || !visit(n) {
		return
	}
	for i := uint(0); i < n.ChildCount(); i++ {
		walk(n.Child(i), visit)
	}
}

// named returns the named children of n.
func named(n *sitter.Node) []*sitter.Node {
	var out []*sitter.Node
	for i := uint(0); i < n.NamedChildCount(); i++ {
		out = append(out, n.NamedChild(i))
	}
	return out
}

// callKinds are the nodes that call something, in every grammar.
var callKinds = set("call_expression", "call", "method_invocation", "object_creation_expression", "new_expression",
	"macro_invocation", "await_expression")

// sameValue reports whether two expressions are one value compared with itself: spelled the same,
// and calling nothing. Two calls spelled the same may return different values, so comparing them is
// a check that can fail.
func sameValue(f *file, a, b *sitter.Node) bool {
	if f.text(a) != f.text(b) {
		return false
	}
	calls := false
	walk(a, func(n *sitter.Node) bool {
		calls = calls || callKinds[n.Kind()]
		return !calls
	})
	return !calls
}

// arguments returns the named children of an argument list without its comments, which the
// grammars keep as named children: a comment between arguments is not one.
func arguments(list *sitter.Node) []*sitter.Node {
	if list == nil {
		return nil
	}
	var out []*sitter.Node
	for _, c := range named(list) {
		if !strings.Contains(c.Kind(), "comment") {
			out = append(out, c)
		}
	}
	return out
}

// field returns the text of a field of n, or "".
func (f *file) field(n *sitter.Node, name string) string {
	if c := n.ChildByFieldName(name); c != nil {
		return f.text(c)
	}
	return ""
}

// unquote strips one pair of matching quotes.
func unquote(s string) string {
	if len(s) >= 2 && strings.ContainsAny(s[:1], `"'`+"`") && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}
