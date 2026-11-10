// Package golang is the Go language adapter. It reads Go with the standard library's parser,
// so it needs no C compiler and identifies facts exactly.
//
// Design: docs/design/adapters.md
package golang

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path"
	"regexp"
	"strings"
	"unicode"

	"github.com/rerorets/hadome/internal/adapter/reuse"
	"github.com/rerorets/hadome/internal/adapter/walk"
	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
)

// Adapter is the Go adapter.
type Adapter struct{}

type parsedFile struct {
	path    string
	pkgPath string
	file    *ast.File
	src     []byte
	test    bool
	// support is a file of a test support package (testutil, storetest): test code that is not a
	// _test.go file. A test that calls one of its functions is followed into it.
	support bool
	// generated is true for a file with the standard header, or one matching files.generated.
	// files.tests never applies to Go: the compiler decides what a test is.
	generated bool
}

// Name is the language's name in configuration.
func (Adapter) Name() string { return "go" }

// Claims reports whether the file is Go that the go tool builds. Like the go tool, it ignores a
// directory whose name starts with an underscore (_examples): its tests never run, so they guard
// nothing. Such files are still listed, with role other.
func (Adapter) Claims(p string) bool {
	if !strings.HasSuffix(p, ".go") {
		return false
	}
	for _, dir := range strings.Split(path.Dir(p), "/") {
		if strings.HasPrefix(dir, "_") {
			return false
		}
	}
	return true
}

// Analyze reads the Go files of the version. Files that do not parse are listed but yield no other
// facts: a broken file is the compiler's business, not a reason to stop checking everything else.
func (Adapter) Analyze(r *reuse.Run, c config.Analysis, paths []string) (*facts.Analysis, error) {
	v := engine.Version{Label: r.Label(), FS: r.FS()}
	return analyze(v, c, paths, true)
}

// AnalyzeWithoutTexts is Analyze leaving every test's strings (facts.Test.Texts) out, for a version
// no other adapter reads them in.
func (Adapter) AnalyzeWithoutTexts(r *reuse.Run, c config.Analysis, paths []string) (*facts.Analysis, error) {
	v := engine.Version{Label: r.Label(), FS: r.FS()}
	return analyze(v, c, paths, false)
}

func analyze(v engine.Version, c config.Analysis, paths []string, texts bool) (*facts.Analysis, error) {
	modules, err := findModules(v.FS)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	var files []parsedFile
	a := &facts.Analysis{}
	at := map[string]int{} // where each parsed file is in a.Files, whose role waits for markSupport
	for _, p := range paths {
		src, err := fs.ReadFile(v.FS, p)
		if err != nil {
			return nil, err
		}
		pf := parsedFile{path: p, src: src, pkgPath: importPath(modules, path.Dir(p)), test: strings.HasSuffix(p, "_test.go"),
			generated: config.MatchesAny(c.GeneratedFiles, p)}
		lines := facts.CountLines(src)
		f, perr := parser.ParseFile(fset, p, src, parser.ParseComments)
		if perr != nil {
			a.Files = append(a.Files, facts.File{Path: p, Language: "go", Role: facts.RoleOther, Lines: lines, Unread: true})
			continue
		}
		pf.file = f
		pf.generated = pf.generated || ast.IsGenerated(f)
		files = append(files, pf)
		at[p] = len(a.Files)
		a.Files = append(a.Files, facts.File{Path: p, Language: "go", Lines: lines, Unit: pf.pkgPath, UnitPath: path.Dir(p)})
		a.Comments = append(a.Comments, comments(fset, p, f)...)
	}
	markSupport(files, c.ProductFiles)
	for _, pf := range files {
		a.Files[at[pf.path]].Role = role(pf)
	}
	helpers := collectHelpers(files)
	idx := buildIndex(files, modules, helpers)
	tb := newTables(c)
	rd := reading{fset: fset, idx: idx, tb: tb, noTexts: !texts}
	mains := findTestMains(rd, files, helpers)
	for _, pf := range files {
		if pf.test {
			a.Tests = append(a.Tests, extractTests(rd, pf, helpers[helperKey(pf)], mains[path.Dir(pf.path)])...)
			continue
		}
		if pf.support { // test code: neither the product's dependencies nor its documented API
			continue
		}
		a.Imports = append(a.Imports, importsOf(fset, pf, idx, tb.io)...)
		if !pf.generated {
			syms, entries := traceFile(rd, pf, symbolsOf(fset, pf))
			a.Symbols = append(a.Symbols, syms...)
			a.Entries = append(a.Entries, entries...)
		}
	}
	return a, nil
}

func role(pf parsedFile) facts.Role {
	switch {
	case pf.test || pf.support:
		return facts.RoleTest
	case pf.generated:
		return facts.RoleGenerated
	default:
		return facts.RoleSource
	}
}

func comments(fset *token.FileSet, p string, f *ast.File) []facts.Comment {
	var out []facts.Comment
	for _, g := range f.Comments {
		for _, c := range g.List {
			text := strings.TrimPrefix(strings.TrimPrefix(c.Text, "//"), "/*")
			text = strings.TrimSuffix(text, "*/")
			// Line breaks before the text stay, so that its first line is the comment's first line.
			text = strings.TrimRightFunc(strings.TrimLeft(text, " \t"), unicode.IsSpace)
			out = append(out, facts.Comment{Path: p, Line: fset.Position(c.Pos()).Line, EndLine: fset.Position(c.End()).Line, Text: text,
				Directive: directive.MatchString(c.Text)})
		}
	}
	return out
}

// directive matches the comments Go treats as directives: //go:embed, //line, //export and the
// like, with no space after the slashes. The engine still reads exception markers in them.
var directive = regexp.MustCompile(`^//(line |extern |export |[a-z0-9]+:[a-z0-9])`)

// findModules maps each directory holding a go.mod to its module path.
func findModules(fsys fs.FS) (map[string]string, error) {
	mods := map[string]string{}
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && p != "." && walk.Skip(d.Name()) {
			return fs.SkipDir
		}
		if d.IsDir() || d.Name() != "go.mod" {
			return nil
		}
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		for _, line := range strings.Split(string(b), "\n") {
			if f := strings.Fields(line); len(f) == 2 && f[0] == "module" {
				mods[path.Dir(p)] = strings.Trim(f[1], `"`)
			}
		}
		return nil
	})
	return mods, err
}

// importPath returns the import path of a directory, using the nearest enclosing module.
func importPath(mods map[string]string, dir string) string {
	for d := dir; ; d = path.Dir(d) {
		if m, ok := mods[d]; ok {
			rel := strings.TrimPrefix(strings.TrimPrefix(dir, d), "/")
			if d == "." {
				rel = dir
			}
			if rel == "." || rel == "" {
				return m
			}
			return m + "/" + rel
		}
		if d == "." || d == "/" {
			return dir
		}
	}
}
