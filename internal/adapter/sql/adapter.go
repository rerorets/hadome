// Package sql is the SQL adapter: database objects defined in .sql files become symbols, and the
// SQL strings of tests in other languages become calls to them.
//
// Design: docs/design/adapters-sql.md
package sql

import (
	"bytes"
	"io/fs"
	"path"
	"regexp"
	"strings"

	"github.com/rerorets/hadome/internal/adapter/reuse"
	"github.com/rerorets/hadome/internal/adapter/walk"
	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
)

// Adapter reads .sql files, and after every language has been read, the SQL strings of tests. It
// keeps nothing between calls: what one version's Analyze builds for its Link is a stage's result.
type Adapter struct{}

// Name is the adapter's name. SQL is not in config.Languages: it is read whenever .sql files are
// there, whichever languages are configured.
func (Adapter) Name() string { return "sql" }

// Claims reports whether the file is SQL: its path ends in .sql. Its content is never looked at.
func (Adapter) Claims(p string) bool { return strings.EqualFold(path.Ext(p), ".sql") }

var (
	undoFile  = regexp.MustCompile(`(?i)(\.down\.sql$|(^|/)down/|(^|/)U\d[^/]*__[^/]*\.sql$)`)
	sqitchDir = regexp.MustCompile(`(^|/)(revert|verify)/`)
)

// role decides what a .sql file is for, by its path and the files around it.
func role(fsys fs.FS, c config.Analysis, p string) facts.Role {
	switch {
	case undoFile.MatchString(p), sqitchDir.MatchString(p) && ancestorHas(fsys, p, "sqitch.plan"),
		ancestorHas(fsys, p, "dbt_project.yml"):
		return facts.RoleOther
	case config.MatchesAny(c.GeneratedFiles, p):
		return facts.RoleGenerated
	case walk.SupportDir(p) || config.MatchesAny(c.TestFiles, p):
		return facts.RoleTest
	}
	return facts.RoleSource
}

// ancestorHas reports whether a directory holding p, or one above it, has a file named name.
func ancestorHas(fsys fs.FS, p, name string) bool {
	for dir := path.Dir(p); ; dir = path.Dir(dir) {
		if _, err := fs.Stat(fsys, path.Join(dir, name)); err == nil {
			return true
		}
		if dir == "." || dir == "/" {
			return false
		}
	}
}

// Analyze reads the .sql files of a version: the objects that remain at head become symbols, and
// tests written in SQL become tests.
func (a Adapter) Analyze(r *reuse.Run, c config.Analysis, paths []string) (*facts.Analysis, error) {
	b, err := buildStage.Do(r, buildInput{Paths: paths, Config: c})
	if err != nil {
		return nil, err
	}
	return b.facts.Clone(), nil
}

// buildInput is the .sql files of a version, in the order they were claimed, and the configuration
// they are read with.
type buildInput struct {
	Paths  []string
	Config config.Analysis
}

// built is what reading a version's .sql files gives: the catalog Link resolves strings against,
// and the facts Analyze reports. The catalog keeps only the databases' objects, not the files: no
// text, definition or tree stays while the other languages are read and linked.
type built struct {
	cat   *catalog
	facts *facts.Analysis
}

// buildStage reads a version's .sql files once for Analyze and Link, and for head too when every
// file, every file around them that decides a role, and the configuration are the same.
var buildStage = reuse.NewReading("sql/build", reuse.ScopePair, buildVersion)

func buildVersion(rd reuse.Reads, in buildInput) (*built, error) {
	cat, files, err := build(engine.Version{FS: rd.FS()}, in.Config, in.Paths)
	if err != nil {
		return nil, err
	}
	out := &facts.Analysis{}
	for _, f := range files {
		ff := facts.File{Path: f.path, Language: "sql", Role: f.role, Lines: facts.CountLines(f.src), Unread: f.unread}
		if f.db != nil && f.role != facts.RoleOther {
			ff.Unit = f.db.unit
		}
		out.Files = append(out.Files, ff)
		if f.role == facts.RoleOther || f.role == facts.RoleGenerated {
			continue
		}
		out.Comments = append(out.Comments, comments(f)...)
		if f.role == facts.RoleSource && f.db != nil {
			out.Imports = append(out.Imports, facts.Import{Path: f.path, Line: 1, FromUnit: f.db.unit, ToUnit: "database",
				IO: true, Global: true})
		}
	}
	for _, db := range cat.databases {
		out.Symbols = append(out.Symbols, db.symbols()...)
	}
	for _, f := range files {
		if f.role == facts.RoleTest && !f.unread && f.db != nil {
			out.Tests = append(out.Tests, sqlTests(f, cat)...)
		}
		f.trees = nil
	}
	return &built{cat: cat, facts: out}, nil
}

// build reads the files into databases and applies their definitions. The databases it returns
// keep only their objects, not the files: the catalog outlives the files' text and trees.
func build(v engine.Version, c config.Analysis, paths []string) (*catalog, []*sqlFile, error) {
	cat := &catalog{databases: newDatabases(c)}
	var files []*sqlFile
	for _, p := range paths {
		src, err := fs.ReadFile(v.FS, p)
		if err != nil {
			return nil, nil, err
		}
		f := &sqlFile{path: p, src: withoutBOM(src), role: role(v.FS, c, p)}
		files = append(files, f)
		if f.role == facts.RoleOther {
			continue
		}
		if f.db = databaseOf(cat.databases, p); f.db != nil {
			f.db.files = append(f.db.files, f)
		}
	}
	for _, db := range cat.databases {
		db.decideDialects()
		for _, f := range db.files {
			if f.role != facts.RoleGenerated {
				f.read()
			}
		}
		db.apply()
		for _, f := range db.files {
			f.defs = nil
		}
		db.files = nil
	}
	return cat, files, nil
}

// withoutBOM blanks the byte order mark editors on Windows start UTF-8 files with. It is blanked,
// not removed, so that offsets stay the file's.
func withoutBOM(src []byte) []byte {
	if bytes.HasPrefix(src, utf8BOM) {
		out := bytes.Clone(src)
		copy(out, "   ")
		return out
	}
	return src
}

var utf8BOM = []byte{0xef, 0xbb, 0xbf}

// directive matches the comments that are instructions to a tool, not prose.
var directive = regexp.MustCompile(`^\s*(noqa\b|sqlfluff:|migrate:|\+goose\b|changeset\b|rollback\b|name:|%)`)

func comments(f *sqlFile) []facts.Comment {
	lx := lexical{nestedBlocks: true, dollarQuotes: true}
	if f.dialect != nil {
		if td, ok := f.dialect.(tableDialect); ok {
			lx = td.lex
		}
	}
	var out []facts.Comment
	for _, c := range scan(string(f.src), lx).Comments {
		out = append(out, facts.Comment{Path: f.path, Line: c.Line, Text: c.Text, Directive: directive.MatchString(c.Text)})
	}
	return out
}

// Link reads the strings of every test, handed over by the other languages, as SQL resolved against
// the version's databases. Without a database (no .sql file and no configured database) it reads
// nothing: there is no dialect and nothing to resolve against.
func (a Adapter) Link(r *reuse.Run, c config.Analysis, an *facts.Analysis) error {
	var paths []string
	for _, f := range an.Files {
		if f.Language == "sql" {
			paths = append(paths, f.Path)
		}
	}
	if !a.ReadsTexts(c, paths) {
		return nil
	}
	b, err := buildStage.Do(r, buildInput{Paths: paths, Config: c})
	if err != nil {
		return err
	}
	cat := b.cat
	for i := range an.Tests {
		t := &an.Tests[i]
		if len(t.Texts) == 0 || strings.EqualFold(path.Ext(t.Path), ".sql") {
			continue
		}
		linkTest(cat, t)
	}
	return nil
}

// ReadsTexts reports whether Link reads the tests' strings: only when the version has a database,
// a .sql file or a configured one.
func (Adapter) ReadsTexts(c config.Analysis, paths []string) bool {
	return len(paths) > 0 || len(c.Databases) > 0
}
