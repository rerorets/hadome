//go:build cgo

package treesitter

import (
	"io/fs"
	"regexp"
	"strings"
	"unicode"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/rerorets/hadome/internal/adapter/reuse"
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
	// support is a test support file (test/helpers.ts, conftest.py): test code too, and a test that
	// calls one of its functions is followed into it.
	support bool
	// testGlob, generatedGlob and productGlob are set from configuration before the language indexes
	// the file. productGlob keeps a file that would be test support product code.
	testGlob, generatedGlob, productGlob bool
	unit                                 string // the unit's ID, as imports name it
	unitPath                             string // the unit's location, as diagrams name it
	// lifted are the nodes a repairer took out of the source before the reading root comes from.
	// They belong to an earlier reading of the same bytes, so their positions and text are src's.
	lifted []*sitter.Node
	// consts are the file's constants that hold a string, by name (see stringConsts); nil
	// until a test first reads a name of the file.
	stringConsts map[string]*sitter.Node
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
	name string
	// r reads grammars and repairs, which keep no state. fresh makes the reader one analysis uses:
	// a reader learns about the version it indexes, and nothing of one version may stay for the next.
	r      reader
	fresh  func() reader
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
func (l *lang) Analyze(r *reuse.Run, c config.Analysis, paths []string) (*facts.Analysis, error) {
	return l.analyze(r, c, paths, true)
}

// AnalyzeWithoutTexts is Analyze leaving every test's strings (facts.Test.Texts) out, for a version
// no other adapter reads them in.
func (l *lang) AnalyzeWithoutTexts(r *reuse.Run, c config.Analysis, paths []string) (*facts.Analysis, error) {
	return l.analyze(r, c, paths, false)
}

func (l *lang) analyze(r *reuse.Run, c config.Analysis, paths []string, texts bool) (*facts.Analysis, error) {
	v := engine.Version{Label: r.Label(), FS: r.FS()}
	rd := l.fresh()
	var files []*file
	for _, p := range paths {
		src, err := fs.ReadFile(v.FS, p)
		if err != nil {
			return nil, err
		}
		// The session lets go of the file's trees when the language's analysis ends.
		pr, err := parseStage.Do(r, parseInput{Lang: l.name, Path: p, Src: src})
		if err != nil {
			return nil, err
		}
		f := pr.file(p)
		gen := config.MatchesAny(c.GeneratedFiles, p)
		f.testGlob, f.generatedGlob = !gen && config.MatchesAny(c.TestFiles, p), gen
		f.productGlob = config.MatchesAny(c.ProductFiles, p)
		files = append(files, f)
	}
	rd.index(v, files)
	t := l.tables.with(c)
	t.lang, t.sqlSinks, t.texts = l.name, c.SQLSinks[l.name], texts
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
			Lines: facts.CountLines(f.src), Unit: f.unit, UnitPath: f.unitPath})
		a.Comments = append(a.Comments, cs...)
		if !f.test {
			a.Imports = append(a.Imports, rd.imports(f, t)...)
		}
		if role == facts.RoleSource {
			a.Symbols = append(a.Symbols, rd.symbols(f)...)
		}
	}
	if tr, ok := rd.(tracer); ok {
		tr.trace(files, t, a)
	}
	for _, f := range files {
		a.Tests = append(a.Tests, rd.tests(f, t)...)
	}
	return a, nil
}

// repairer is a reader whose grammar misreads some correct code. repair returns a copy of src to
// read in its place, with those parts of the code blanked out, and the nodes of root it blanked; or
// nil when root is read right. The copy keeps every byte's position and line.
type repairer interface {
	repair(src []byte, root *sitter.Node) ([]byte, []*sitter.Node)
}

// parse parses one file with the grammar its path calls for. done frees the file's trees, after
// which its nodes must not be used.
func (l *lang) parse(parser *sitter.Parser, p string, src []byte) (f *file, done func(), err error) {
	pr, err := parseWith(l.r, parser, p, src)
	if err != nil {
		return nil, nil, err
	}
	return pr.file(p), pr.Release, nil
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
		out = append(out, facts.Comment{Path: f.path, Line: line(n), EndLine: endLine(n), Text: stripDelimiters(raw), Directive: directive(raw)})
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
	return trimComment(strings.TrimSuffix(s, "*/"))
}

// trimComment trims the spaces around a comment's text, but not the line breaks before it: the
// text's first line must stay the comment's first line, so that line numbers counted from it hold.
func trimComment(s string) string {
	return strings.TrimRightFunc(strings.TrimLeft(s, " \t"), unicode.IsSpace)
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
