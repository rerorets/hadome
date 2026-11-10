// Package sql is the SQL adapter: database objects defined in .sql files become symbols, and the
// SQL strings of tests in other languages become calls to them.
//
// Design: docs/design/adapters-sql.md
package sql

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"strings"
	"sync"

	"github.com/rerorets/hadome/internal/adapter/walk"
	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
)

// Adapter reads .sql files, and after every language has been read, the SQL strings of tests.
type Adapter struct {
	// cache keeps the catalog Analyze built, for Link to use on the same files. It is keyed by the
	// files' paths and contents, so a catalog is never used for files it was not built from.
	mu    sync.Mutex
	cache map[[32]byte]*catalog
}

// New returns the SQL adapter.
func New() *Adapter { return &Adapter{cache: map[[32]byte]*catalog{}} }

// Name is the adapter's name. SQL is not in config.Languages: it is read whenever .sql files are
// there, whichever languages are configured.
func (*Adapter) Name() string { return "sql" }

// Claims reports whether the file is SQL: its path ends in .sql. Its content is never looked at.
func (*Adapter) Claims(p string) bool { return strings.EqualFold(path.Ext(p), ".sql") }

var (
	undoFile  = regexp.MustCompile(`(?i)(\.down\.sql$|(^|/)down/|(^|/)U\d[^/]*__[^/]*\.sql$)`)
	sqitchDir = regexp.MustCompile(`(^|/)(revert|verify)/`)
)

// role decides what a .sql file is for, by its path and the files around it.
func role(fsys fs.FS, c config.Config, p string) facts.Role {
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
func (a *Adapter) Analyze(v engine.Version, c config.Config, paths []string) (*facts.Analysis, error) {
	cat, files, err := build(v, c, paths)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	a.cache[fingerprint(c, files)] = cat
	a.mu.Unlock()
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
	}
	return out, nil
}

// build reads the files into databases and applies their definitions.
func build(v engine.Version, c config.Config, paths []string) (*catalog, []*sqlFile, error) {
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

// fingerprint identifies a set of files by their paths and contents, read with the configured
// databases.
func fingerprint(c config.Config, files []*sqlFile) [32]byte {
	h := sha256.New()
	fmt.Fprintf(h, "%q\x00", c.Databases)
	for _, f := range files {
		h.Write([]byte(f.path))
		h.Write([]byte{0})
		h.Write(f.src)
		h.Write([]byte{0})
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

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
func (a *Adapter) Link(v engine.Version, c config.Config, an *facts.Analysis) error {
	var paths []string
	for _, f := range an.Files {
		if f.Language == "sql" {
			paths = append(paths, f.Path)
		}
	}
	if !a.ReadsTexts(c, paths) {
		return nil
	}
	cat, err := a.catalogFor(v, c, paths)
	if err != nil {
		return err
	}
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
func (*Adapter) ReadsTexts(c config.Config, paths []string) bool {
	return len(paths) > 0 || len(c.Databases) > 0
}

func (a *Adapter) catalogFor(v engine.Version, c config.Config, paths []string) (*catalog, error) {
	var files []*sqlFile
	for _, p := range paths {
		src, err := fs.ReadFile(v.FS, p)
		if err != nil {
			return nil, err
		}
		files = append(files, &sqlFile{path: p, src: src})
	}
	key := fingerprint(c, files)
	a.mu.Lock()
	cat, ok := a.cache[key]
	delete(a.cache, key)
	a.mu.Unlock()
	if ok {
		return cat, nil
	}
	cat, _, err := build(v, c, paths)
	return cat, err
}
