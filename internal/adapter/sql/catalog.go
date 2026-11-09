package sql

import (
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/facts"
)

// database is one set of objects whose names share a namespace, with the files that define them.
type database struct {
	cfg  config.Database
	unit string
	// dialect reads the database's test strings: the configured one, or the only one its files'
	// markers point to. Nil when it is not decided.
	dialect Dialect
	files   []*sqlFile
	objects map[string]*object
	// triggers are PostgreSQL's triggers, which are not symbols: a write to their table calls the
	// routine they execute.
	triggers []trigger
}

// sqlFile is one .sql file of a database.
type sqlFile struct {
	path    string
	src     []byte
	role    facts.Role
	db      *database
	dialect Dialect
	unread  bool
	defs    []Definition
	trees   []*Node
}

// object is one database object that remains at head.
type object struct {
	key     string
	kind    ObjectKind
	path    string
	line    int
	endLine int
	params  []string
	events  []string
	on      string // a trigger's table, as a key
	doc     bool
	pkg     bool // a routine of an Oracle package specification
}

type trigger struct {
	on       string
	events   []string
	executes string
}

// catalog is every database of a version.
type catalog struct {
	databases []*database
}

// defaultSchemas are the schemas unqualified names are in when the configuration names none.
var defaultSchemas = map[string]string{"postgresql": "public", "sqlite": "main", "tsql": "dbo", "snowflake": "PUBLIC"}

// newDatabases returns the configured databases, or one database holding every file.
func newDatabases(c config.Config) []*database {
	if len(c.Databases) == 0 {
		return []*database{{unit: ".sql", objects: map[string]*object{}}}
	}
	var out []*database
	for _, cfg := range c.Databases {
		unit := ".sql"
		if len(c.Databases) > 1 {
			unit = ".sql/" + cfg.Name
		}
		db := &database{cfg: cfg, unit: unit, objects: map[string]*object{}}
		if cfg.Dialect != "" {
			db.dialect, _ = dialectNamed(cfg.Dialect)
		}
		out = append(out, db)
	}
	return out
}

// databaseOf returns the first database whose paths hold the file, or nil.
func databaseOf(dbs []*database, p string) *database {
	for _, db := range dbs {
		if len(db.cfg.Paths) == 0 || config.MatchesAny(db.cfg.Paths, p) {
			return db
		}
	}
	return nil
}

// markerDialects returns the names of the dialects whose markers src has.
func markerDialects(src []byte) []string {
	var out []string
	for _, d := range dialects {
		if len(d.Markers(src)) > 0 {
			out = append(out, d.Name())
		}
	}
	return out
}

// decideDialects gives every file of the database its dialect: the configured one, else the one its
// markers point to, else the one the markers of every other file of the database point to. A file
// whose markers point to two dialects, or to one that is not enabled, has none and is unread.
func (db *database) decideDialects() {
	if db.dialect != nil && db.cfg.Dialect != "" {
		for _, f := range db.files {
			f.dialect = db.dialect
		}
		return
	}
	marks := map[*sqlFile][]string{}
	seen := map[string]bool{}
	for _, f := range db.files {
		marks[f] = markerDialects(f.src)
		if len(marks[f]) == 1 {
			seen[marks[f][0]] = true
		}
	}
	var only string
	if len(seen) == 1 {
		for name := range seen {
			only = name
		}
	}
	for _, f := range db.files {
		name := only
		switch len(marks[f]) {
		case 0:
		case 1:
			name = marks[f][0]
		default:
			name = ""
		}
		if d, ok := dialectNamed(name); ok {
			f.dialect = d
		}
	}
	if d, ok := dialectNamed(only); ok {
		db.dialect = d
	}
}

// read parses the file. A file with a statement the grammar cannot read is unread as a whole.
func (f *sqlFile) read() {
	if f.dialect == nil {
		f.unread = true
		return
	}
	for _, s := range f.dialect.Split(f.src) {
		t, err := f.dialect.Parse(s)
		if err != nil {
			f.unread, f.defs, f.trees = true, nil, nil
			return
		}
		f.trees = append(f.trees, t)
		f.defs = append(f.defs, f.dialect.Definitions(t)...)
	}
}

var (
	digits     = regexp.MustCompile(`\d+`)
	versioned  = regexp.MustCompile(`^[Vv]?\d`)
	repeatable = regexp.MustCompile(`^R__`)
)

// applyOrder sorts the database's product files in the order they are applied: the configured
// order's globs, then the natural order of paths, with Flyway's repeatable R__ files last.
func (db *database) applyOrder() []*sqlFile {
	var files []*sqlFile
	for _, f := range db.files {
		if f.role == facts.RoleSource && !f.unread {
			files = append(files, f)
		}
	}
	sort.SliceStable(files, func(i, j int) bool {
		a, b := files[i].path, files[j].path
		if oa, ob := db.orderIndex(a), db.orderIndex(b); oa != ob {
			return oa < ob
		}
		if ra, rb := repeatable.MatchString(path.Base(a)), repeatable.MatchString(path.Base(b)); ra != rb {
			return rb
		}
		return naturalLess(a, b)
	})
	return files
}

// orderIndex is the index of the first configured order glob the path matches; paths matching none
// come after all.
func (db *database) orderIndex(p string) int {
	for i, g := range db.cfg.Order {
		if config.MatchesAny([]string{g}, p) {
			return i
		}
	}
	return len(db.cfg.Order)
}

// decided reports whether the order of two files is known: the configuration orders them, they
// are in one directory, or both names start with a version.
func (db *database) decided(a, b string) bool {
	if db.orderIndex(a) != db.orderIndex(b) || path.Dir(a) == path.Dir(b) {
		return true
	}
	return versioned.MatchString(path.Base(a)) && versioned.MatchString(path.Base(b))
}

// naturalLess compares paths with runs of digits compared as numbers: V2__ before V10__.
func naturalLess(a, b string) bool {
	for a != "" && b != "" {
		da, db := digits.FindStringIndex(a), digits.FindStringIndex(b)
		if da == nil || db == nil || da[0] != db[0] || a[:da[0]] != b[:db[0]] {
			return a < b
		}
		na, nb := strings.TrimLeft(a[da[0]:da[1]], "0"), strings.TrimLeft(b[db[0]:db[1]], "0")
		if len(na) != len(nb) {
			return len(na) < len(nb)
		}
		if na != nb {
			return na < nb
		}
		a, b = a[da[1]:], b[db[1]:]
	}
	return len(a) < len(b)
}

// schemaFor returns the schema of an unqualified definition.
func (db *database) schemaFor(d Dialect) string {
	if db.cfg.DefaultSchema != "" {
		return d.Fold(Ident{Text: db.cfg.DefaultSchema})
	}
	if len(db.cfg.SearchPath) > 0 && d.Name() == "postgresql" {
		return d.Fold(Ident{Text: db.cfg.SearchPath[0]})
	}
	if s, ok := defaultSchemas[d.Name()]; ok {
		return s
	}
	return ""
}

// defKey returns the key of a defined name: schema.name folded, with the database part dropped
// (or checked, when the configuration names the catalog). ok is false for a name in another
// database or on a linked server.
func (db *database) defKey(d Dialect, name QualifiedName) (string, bool) {
	parts := make([]string, 0, len(name))
	for _, p := range name {
		parts = append(parts, d.Fold(p))
	}
	switch len(parts) {
	case 0:
		return "", false
	case 1:
		if s := db.schemaFor(d); s != "" {
			return s + "." + parts[0], true
		}
		return parts[0], true
	case 2:
		return parts[0] + "." + parts[1], true
	case 3:
		if db.cfg.Catalog != "" && parts[0] != d.Fold(Ident{Text: db.cfg.Catalog}) {
			return "", false
		}
		return parts[1] + "." + parts[2], true
	}
	return "", false
}

// pkgKey returns the key of an Oracle package routine: the package's key, then the routine.
func (db *database) pkgKey(d Dialect, name QualifiedName) (string, bool) {
	pkg, ok := db.defKey(d, name[:len(name)-1])
	return pkg + "." + d.Fold(name[len(name)-1]), ok
}

// symbols returns the database's objects that are symbols.
func (db *database) symbols() []facts.Symbol {
	var out []facts.Symbol
	for _, o := range db.objects {
		row := objectKinds[o.kind]
		if row.Symbol == "" {
			continue
		}
		s := facts.Symbol{ID: db.unit + "/" + o.key, Kind: row.Symbol, Path: o.path, Line: o.line, EndLine: o.endLine,
			Exported: true, Doc: o.doc, Params: len(o.params)}
		for _, p := range o.params {
			s.Scoped = s.Scoped || facts.IsScopeName(p)
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
