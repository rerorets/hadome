package sql

import (
	"path"
	"regexp"
	"slices"
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
	for _, e := range dialects {
		if len(e.Dialect.Markers(src)) > 0 {
			out = append(out, e.Dialect.Name())
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

// apply runs the definitions of the database's files in order, leaving the objects that remain.
func (db *database) apply() {
	files := db.applyOrder()
	created := map[string]string{} // key -> file that created it
	for _, f := range files {
		d := f.dialect
		for _, def := range f.defs {
			if def.Temp {
				continue
			}
			key, ok := db.defKey(d, def.Name)
			if def.Package {
				key, ok = db.pkgKey(d, def.Name)
			}
			if !ok {
				continue
			}
			switch def.Effect {
			case EffectCreate:
				db.create(f, d, key, def)
				created[key] = f.path
			case EffectDoc:
				if o := db.objects[key]; o != nil {
					o.doc = true
				}
			case EffectDrop:
				if from, ok := created[key]; ok && !db.decided(from, f.path) {
					continue // the order of the create and the drop is not known: the object remains
				}
				db.drop(key, def.Kind)
			case EffectRename:
				db.rename(d, key, def)
			}
		}
	}
}

func (db *database) create(f *sqlFile, d Dialect, key string, def Definition) {
	if def.Kind == KindPackage {
		db.dropPackage(key)
	}
	if def.Kind == KindTrigger && d.Name() == "postgresql" {
		on, _ := db.defKey(d, def.On)
		exec, _ := db.defKey(d, def.Executes)
		db.triggers = slices.DeleteFunc(db.triggers, func(t trigger) bool { return t.on == on && t.executes == exec })
		db.triggers = append(db.triggers, trigger{on: on, events: def.Events, executes: exec})
		return
	}
	if objectKinds[def.Kind].Symbol == "" {
		return
	}
	o := &object{key: key, kind: def.Kind, path: f.path, line: def.Line, endLine: def.EndLine, params: def.Params,
		events: def.Events, pkg: def.Package, doc: precededByComment(f.src, def.Start) || describes(f, def)}
	if def.On != nil {
		o.on, _ = db.defKey(d, def.On)
	}
	db.objects[key] = o
}

// sameKind reports whether a drop of kind k applies to the object: DROP VIEW never drops a table.
func sameKind(k, objKind ObjectKind) bool {
	family := func(x ObjectKind) ObjectKind {
		if x == KindMatView {
			return KindView
		}
		if x == KindPackageBody {
			return KindPackage
		}
		return x
	}
	return family(k) == family(objKind)
}

func (db *database) drop(key string, kind ObjectKind) {
	if kind == KindPackage {
		db.dropPackage(key)
		return
	}
	if o := db.objects[key]; o != nil && sameKind(kind, o.kind) {
		delete(db.objects, key)
		if o.kind == KindTable {
			db.dropTriggersOn(key)
		}
	}
}

func (db *database) dropPackage(key string) {
	for k, o := range db.objects {
		if o.pkg && strings.HasPrefix(k, key+".") {
			delete(db.objects, k)
		}
	}
}

func (db *database) dropTriggersOn(table string) {
	for k, o := range db.objects {
		if o.kind == KindTrigger && o.on == table {
			delete(db.objects, k)
		}
	}
	db.triggers = slices.DeleteFunc(db.triggers, func(t trigger) bool { return t.on == table })
}

// rename moves an object to its new name. A part the new name leaves out keeps the old one:
// ALTER TABLE billing.a RENAME TO b is billing.b, SET SCHEMA other keeps the name.
func (db *database) rename(d Dialect, key string, def Definition) {
	o := db.objects[key]
	if o == nil || def.Kind != KindOther && !sameKind(def.Kind, o.kind) {
		return
	}
	oldSchema, oldName := "", key
	if i := strings.LastIndex(key, "."); i >= 0 {
		oldSchema, oldName = key[:i], key[i+1:]
	}
	var newKey string
	switch {
	case len(def.NewName) == 2 && def.NewName[1].Text == "":
		newKey = d.Fold(def.NewName[0]) + "." + oldName
	case len(def.NewName) == 1 && oldSchema != "":
		newKey = oldSchema + "." + d.Fold(def.NewName[0])
	default:
		k, ok := db.defKey(d, def.NewName)
		if !ok {
			return
		}
		newKey = k
	}
	delete(db.objects, key)
	o.key = newKey
	db.objects[newKey] = o
	for _, t := range db.objects {
		if t.on == key {
			t.on = newKey
		}
	}
}

// precededByComment reports whether a comment ends on the line right before offset.
func precededByComment(src []byte, offset int) bool {
	before := strings.TrimRight(string(src[:min(offset, len(src))]), " \t")
	before = strings.TrimSuffix(before, "\n")
	before = strings.TrimSuffix(before, "\r")
	last := before[strings.LastIndex(before, "\n")+1:]
	t := strings.TrimSpace(last)
	return strings.HasPrefix(t, "--") || strings.HasSuffix(t, "*/") || strings.HasPrefix(t, "#")
}

var describedBy = regexp.MustCompile(`(?i)\bCOMMENT\s*=?\s*['"]|\bdescription\s*=\s*['"r]|\bMS_Description\b`)

// describes reports whether the definition's statement gives the object a description: MySQL's
// COMMENT = '…', BigQuery's OPTIONS(description = …).
func describes(f *sqlFile, def Definition) bool {
	lines := strings.Split(string(f.src), "\n")
	if def.Line < 1 || def.EndLine > len(lines) || def.EndLine < def.Line {
		return false
	}
	return describedBy.MatchString(strings.Join(lines[def.Line-1:def.EndLine], "\n"))
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
