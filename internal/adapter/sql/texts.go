package sql

import (
	"regexp"
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
	times := max(tx.Times, 1)
	var sqlReadings []reading
	guessed := false
	for _, r := range rs {
		if r.sql {
			sqlReadings = append(sqlReadings, r)
		}
		guessed = guessed || r.guessed
	}
	if len(sqlReadings) == 0 {
		if guessed {
			e.unresolved++
		}
		return
	}
	for _, r := range sqlReadings {
		e.hazards = append(e.hazards, r.hazards...)
	}
	// Each reference is resolved in every database that read the string.
	base := sqlReadings[0]
	for ri, ref := range base.refs {
		d := base.db.dialect.(tableDialect)
		if len(ref.Name) == 1 && temps[d.key(ref.Name)] {
			continue
		}
		var hits []*object
		var hitDB *database
		isBuiltin := false
		for _, r := range sqlReadings {
			if ri >= len(r.refs) {
				continue
			}
			o, out := r.db.resolve(r.db.dialect, r.refs[ri])
			switch out {
			case found:
				hits = append(hits, o)
				hitDB = r.db
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
	head := firstWord(text, d.lex)
	if !statementHeads[strings.ToUpper(head)] && tx.Sink != facts.TextSinkSQL {
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
		// A string with a hole that does not read most likely has the hole where a name goes.
		r.guessed = len(tx.Holes) > 0 || mentions(db, d, text)
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

// firstWord returns the first word of text outside comments.
func firstWord(text string, lx lexical) string {
	masked := scan(text, lx).Masked
	s := strings.TrimLeft(masked, " \t\r\n(")
	end := strings.IndexFunc(s, func(r rune) bool {
		return !(r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z')
	})
	if end < 0 {
		return s
	}
	return s[:end]
}

var jdbcCall = regexp.MustCompile(`(?is)^\s*\{\s*(\?\s*=\s*)?call\s+(.*?)\s*\}\s*$`)

// callEscape turns JDBC's {call p(?)} into the dialect's call statement.
func callEscape(text, dialect string) string {
	m := jdbcCall.FindStringSubmatch(text)
	if m == nil {
		return text
	}
	if dialect == "tsql" {
		return "EXEC " + m[2]
	}
	return "CALL " + m[2]
}

var driverParam = regexp.MustCompile(`\$\d+|\?|%\(\w+\)s|%s|@\w+|:[A-Za-z_]\w*`)

// prepare turns the holes of the host language and the drivers' parameters ($1, ?, :name, @p,
// %s) into a value, 0, so that the grammar reads them where a value goes. A hole where a name goes
// then does not read, which is what makes the string unresolved.
func prepare(text string, holes []int, d tableDialect) string {
	masked := scan(text, d.lex).Masked
	var sb strings.Builder
	last := 0
	insert := map[int]bool{}
	for _, h := range holes {
		if h >= 0 && h <= len(text) {
			insert[h] = true
		}
	}
	for i := 0; i <= len(text); i++ {
		if insert[i] && !insideString(text, masked, i) {
			sb.WriteString(text[last:i])
			sb.WriteString(" 0 ")
			last = i
		}
	}
	sb.WriteString(text[last:])
	out := sb.String()
	masked = scan(out, d.lex).Masked
	var res strings.Builder
	last = 0
	for _, m := range driverParam.FindAllStringIndex(masked, -1) {
		p := masked[m[0]:m[1]]
		keep := false
		switch {
		case p == "?" && m[1] < len(masked) && (masked[m[1]] == '|' || masked[m[1]] == '&'):
			keep = true // PostgreSQL's jsonb ?| and ?& operators
		case p[0] == '@' && (d.name == "tsql" || d.name == "mysql" || d.name == "bigquery"):
			keep = d.name != "bigquery" // T-SQL and MySQL variables; BigQuery's @name is a parameter
		case p[0] == ':' && m[0] > 0 && (masked[m[0]-1] == ':' || isWordByte(masked[m[0]-1])):
			keep = true // a :: cast, or a:b
		case p[0] == '$' && d.name != "postgresql" && d.name != "snowflake":
			keep = false
		}
		if keep {
			continue
		}
		res.WriteString(out[last:m[0]])
		res.WriteString("0")
		last = m[1]
	}
	res.WriteString(out[last:])
	return res.String()
}

func isWordByte(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// insideString reports whether offset i of text is inside a string literal: the masked text has
// blanks there while the text does not, or it sits between quotes.
func insideString(text, masked string, i int) bool {
	quotes := strings.Count(masked[:min(i, len(masked))], "'")
	return quotes%2 == 1
}

var wordRe = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_$]*`)

// mentions reports whether text holds a word equal to the last part of one of the database's object
// names.
func mentions(db *database, d tableDialect, text string) bool {
	names := map[string]bool{}
	for k := range db.objects {
		names[lastPart(k)] = true
	}
	for _, w := range wordRe.FindAllString(text, -1) {
		if names[d.fold(Ident{Text: w})] {
			return true
		}
	}
	return false
}

// hazardsIn finds the dialect's hazards among the calls and keywords of a tree.
func hazardsIn(d tableDialect, t *Node, tx facts.Text) []facts.Hazard {
	var out []facts.Hazard
	add := func(cat, evidence string) {
		h := facts.Hazard{Category: facts.HazardCategory(cat), Path: tx.Path, Line: tx.Line, Evidence: evidence}
		for _, have := range out {
			if have == h {
				return
			}
		}
		out = append(out, h)
	}
	calls := map[string]bool{}
	for _, r := range d.References(t) {
		if r.Use == UseCall {
			calls[d.key(r.Name)] = true
		}
	}
	toks := words(t.Tokens())
	for _, h := range d.hazards {
		if h.Call != "" {
			parts := splitName(h.Call)
			if calls[d.key(parts)] {
				add(h.Category, h.Call)
			}
			continue
		}
		for i := range toks {
			if i+len(h.Keywords) <= len(toks) && equalWords(toks[i:i+len(h.Keywords)], h.Keywords) {
				add(h.Category, strings.Join(h.Keywords, " "))
				break
			}
		}
	}
	return out
}

func equalWords(a, b []string) bool {
	for i := range b {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
