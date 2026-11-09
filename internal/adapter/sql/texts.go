package sql

import (
	"sort"
	"strings"

	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/facts"
)

// statementHeads are the first words of a string that make it SQL to try.
var statementHeads = map[string]bool{
	"SELECT": true, "WITH": true, "INSERT": true, "UPDATE": true, "DELETE": true, "MERGE": true, "CALL": true,
	"EXEC": true, "EXECUTE": true, "VALUES": true, "TABLE": true, "REFRESH": true, "TRUNCATE": true, "BEGIN": true,
	"DECLARE": true, "CREATE": true, "DROP": true, "ALTER": true, "REPLACE": true, "WAITFOR": true,
}

// reading is what one test string says, read in one database's dialect.
type reading struct {
	db      *database
	sql     bool // it read to the end as statements
	refs    []Reference
	defs    []Definition
	hazards []facts.Hazard
	// guessed is true for a string that should be SQL but did not read, and holds a word equal to
	// one of the database's object names.
	guessed bool
}

// effect collects what a test's strings add to the test.
type effect struct {
	calls      map[string]*callCount
	reads      int
	unresolved int
	hazards    []facts.Hazard
}

type callCount struct {
	times  int
	scopes map[string]bool
}

func (e *effect) call(id string, times int, scope string) {
	cc := e.calls[id]
	if cc == nil {
		cc = &callCount{scopes: map[string]bool{}}
		e.calls[id] = cc
	}
	cc.times += times
	if scope != "" {
		cc.scopes[scope] = true
	}
}

// linkTest reads the test's strings and adds the calls, reads, unresolved names and hazards they
// hold to it.
func linkTest(cat *catalog, t *facts.Test) {
	e := &effect{calls: map[string]*callCount{}}
	dbs := databasesFor(cat, t.Path)
	// Temporary objects the test's strings create hide product objects of the same name.
	temps := map[string]bool{}
	var readings [][]reading
	for _, tx := range t.Texts {
		if tx.Dynamic {
			e.unresolved++
			continue
		}
		var rs []reading
		for _, db := range dbs {
			if r, ok := readText(db, tx); ok {
				rs = append(rs, r)
				for _, d := range r.defs {
					if d.Temp && d.Effect == EffectCreate {
						temps[db.dialect.(tableDialect).key(d.Name)] = true
					}
				}
			}
		}
		readings = append(readings, rs)
	}
	i := 0
	for _, tx := range t.Texts {
		if tx.Dynamic {
			continue
		}
		e.apply(readings[i], tx, temps)
		i++
	}
	merge(t, e)
}

// databasesFor returns the databases a test's strings are resolved against: those whose tests
// globs match the test, else every database whose dialect is known.
func databasesFor(cat *catalog, testPath string) []*database {
	var matched, known []*database
	for _, db := range cat.databases {
		if db.dialect == nil {
			continue
		}
		known = append(known, db)
		if len(db.cfg.Tests) > 0 && config.MatchesAny(db.cfg.Tests, testPath) {
			matched = append(matched, db)
		}
	}
	if len(matched) > 0 {
		return matched
	}
	var unscoped []*database
	for _, db := range known {
		if len(db.cfg.Tests) == 0 {
			unscoped = append(unscoped, db)
		}
	}
	return unscoped
}

// apply adds what one string says. A name that resolves in two or more databases is unresolved.
func (e *effect) apply(rs []reading, tx facts.Text, temps map[string]bool) {
	var sqlReadings []reading
	guessed := false
	for _, r := range rs {
		if r.sql {
			sqlReadings = append(sqlReadings, r)
			e.hazards = append(e.hazards, r.hazards...)
		}
		guessed = guessed || r.guessed
	}
	if len(sqlReadings) == 0 {
		if guessed {
			e.unresolved++
		}
		return
	}
	// The readings of one string differ only in the database they resolve against, so their
	// references line up.
	base := sqlReadings[0]
	d := base.db.dialect.(tableDialect)
	for ri, ref := range base.refs {
		if len(ref.Name) == 1 && temps[d.key(ref.Name)] {
			continue
		}
		e.resolveIn(sqlReadings, ri, ref, max(tx.Times, 1))
	}
}

// resolveIn resolves the ri-th reference in every database that read the string. It is a fact only
// when exactly one database has the object.
func (e *effect) resolveIn(readings []reading, ri int, ref Reference, times int) {
	var hits []*object
	var hitDB *database
	isBuiltin := false
	for _, r := range readings {
		if ri >= len(r.refs) {
			continue
		}
		switch o, out := r.db.resolve(r.db.dialect, r.refs[ri]); out {
		case found:
			hits, hitDB = append(hits, o), r.db
		case builtin:
			isBuiltin = true
		}
	}
	switch {
	case len(hits) == 1:
		e.use(hitDB, hits[0], ref, times)
	case len(hits) == 0 && isBuiltin:
	default:
		e.unresolved++
	}
}

// use adds the facts of naming an object: a call of a routine or a view, a read of a relation, and a
// call of the triggers a write runs.
func (e *effect) use(db *database, o *object, ref Reference, times int) {
	id := db.unit + "/" + o.key
	if objectKinds[o.kind].Calls {
		e.call(id, times, scopeArg(o, ref))
	} else {
		e.reads++
	}
	if ref.Use == UseWrite && o.kind == KindTable {
		for _, trg := range db.triggered(o.key, ref.Event) {
			e.call(db.unit+"/"+trg.key, times, "")
		}
	}
}

// scopeArg returns the normalized argument passed at the routine's scope parameter: the first
// parameter whose name says whose data it works on. Named arguments (@account = 1, account => 1)
// are matched by name.
func scopeArg(o *object, ref Reference) string {
	idx := -1
	for i, p := range o.params {
		if facts.IsScopeName(p) {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ""
	}
	name := strings.ToLower(o.params[idx])
	for _, a := range ref.Args {
		for _, sep := range []string{"=>", "="} {
			if k, v, ok := strings.Cut(a, sep); ok && strings.ToLower(strings.TrimLeft(strings.TrimSpace(k), "@")) == name {
				return strings.ToLower(strings.TrimSpace(v))
			}
		}
	}
	if idx < len(ref.Args) && !strings.Contains(ref.Args[idx], "=") {
		return strings.ToLower(strings.TrimSpace(ref.Args[idx]))
	}
	return ""
}

// merge adds the effect to the test: calls merged with the ones its language found, sorted by
// target.
func merge(t *facts.Test, e *effect) {
	for id, cc := range e.calls {
		merged := false
		for i := range t.Calls {
			if t.Calls[i].Target == id {
				t.Calls[i].Times += cc.times
				t.Calls[i].Scopes = max(t.Calls[i].Scopes, len(cc.scopes))
				merged = true
			}
		}
		if !merged {
			t.Calls = append(t.Calls, facts.Call{Target: id, Times: cc.times, Scopes: len(cc.scopes)})
		}
	}
	sort.Slice(t.Calls, func(i, j int) bool { return t.Calls[i].Target < t.Calls[j].Target })
	t.Reads += e.reads
	t.Unresolved += e.unresolved
	for _, h := range e.hazards {
		dup := false
		for _, have := range t.Hazards {
			dup = dup || have == h
		}
		if !dup {
			t.Hazards = append(t.Hazards, h)
		}
	}
}

// readText reads one string in the database's dialect. ok is false when the string is not SQL to
// try: it has no statement head and was not passed to a call that takes SQL.
func readText(db *database, tx facts.Text) (reading, bool) {
	d := db.dialect.(tableDialect)
	text := callEscape(tx.Value, d.name)
	hasHead := statementHeads[strings.ToUpper(firstWord(text, d.lex))]
	if !hasHead && tx.Sink != facts.TextSinkSQL {
		return reading{}, false
	}
	r := reading{db: db}
	prepared := prepare(text, tx.Holes, d)
	var trees []*Node
	for _, s := range d.Split([]byte(prepared)) {
		t, err := d.Parse(s)
		if err != nil {
			trees = nil
			break
		}
		trees = append(trees, t)
	}
	if trees == nil {
		// A statement with a hole that does not read most likely has the hole where a name goes. A
		// string without a statement head that does not read may not be SQL at all: only a word
		// naming an object makes it count.
		r.guessed = hasHead && len(tx.Holes) > 0 || mentions(db, d, text)
		return r, true
	}
	r.sql = true
	for _, t := range trees {
		r.refs = append(r.refs, d.References(t)...)
		r.defs = append(r.defs, d.Definitions(t)...)
		r.hazards = append(r.hazards, hazardsIn(d, t, tx)...)
	}
	for _, def := range r.defs {
		if def.Effect != EffectCreate || def.Temp {
			continue
		}
		if key, ok := db.defKey(d, def.Name); ok && db.objects[key] != nil {
			r.hazards = append(r.hazards, facts.Hazard{Category: facts.HazardMock, Path: tx.Path, Line: tx.Line,
				Evidence: "CREATE " + key})
		}
	}
	return r, true
}
