package sql

import (
	"strings"
)

// outcome is what a name in a test resolved to.
type outcome int

const (
	unresolved outcome = iota
	builtin
	found
)

// resolve finds the object a name in a test names, the way the database would: qualified names
// directly, unqualified ones by the search order, the default schema, or the only object of that
// name. A built-in function or a system schema counts nothing.
func (db *database) resolve(d Dialect, ref Reference) (*object, outcome) {
	if ref.Hole || len(ref.Name) == 0 {
		return nil, unresolved
	}
	parts := make([]string, len(ref.Name))
	for i, p := range ref.Name {
		parts[i] = d.Fold(p)
	}
	b := d.Builtins()
	if len(parts) >= 2 && (b.Schemas[parts[len(parts)-2]] || len(parts) >= 3 && b.Schemas[parts[len(parts)-3]] ||
		hasPrefix(parts[0], b.Prefixes)) {
		return nil, builtin
	}
	switch len(parts) {
	case 1:
		return db.unqualified(d, parts[0], ref.Use == UseCall)
	case 2:
		if o := db.objects[parts[0]+"."+parts[1]]; o != nil {
			return o, found
		}
		// pkg.routine of an Oracle package in the default schema, or in the only schema that has it
		if s := db.schemaFor(d); s != "" {
			if o := db.objects[s+"."+parts[0]+"."+parts[1]]; o != nil && o.pkg {
				return o, found
			}
		}
		return db.only(func(o *object) bool { return o.pkg && strings.HasSuffix(o.key, "."+parts[0]+"."+parts[1]) })
	case 3:
		if o := db.objects[strings.Join(parts, ".")]; o != nil && o.pkg {
			return o, found
		}
		if db.cfg.Catalog != "" && parts[0] != d.Fold(Ident{Text: db.cfg.Catalog}) {
			return nil, unresolved
		}
		if o := db.objects[parts[1]+"."+parts[2]]; o != nil {
			return o, found
		}
	}
	return nil, unresolved
}

func (db *database) unqualified(d Dialect, name string, call bool) (*object, outcome) {
	b := d.Builtins()
	if call && b.Functions[name] {
		return nil, builtin
	}
	if o := db.lookup(d, name); o != nil {
		return o, found
	}
	if hasPrefix(name, b.Prefixes) {
		return nil, builtin
	}
	if len(db.cfg.SearchPath) > 0 {
		return nil, unresolved // the database searches only its search order
	}
	return db.only(func(o *object) bool { return !o.pkg && lastPart(o.key) == name })
}

// lookup finds an unqualified name in the search order, then in the default schema.
func (db *database) lookup(d Dialect, name string) *object {
	for _, s := range db.cfg.SearchPath {
		if o := db.objects[d.Fold(Ident{Text: s})+"."+name]; o != nil {
			return o
		}
	}
	if s := db.schemaFor(d); s != "" {
		return db.objects[s+"."+name]
	}
	return db.objects[name]
}

// only returns the object matching, when exactly one does.
func (db *database) only(match func(*object) bool) (*object, outcome) {
	var hit *object
	for _, o := range db.objects {
		if match(o) {
			if hit != nil {
				return nil, unresolved
			}
			hit = o
		}
	}
	if hit == nil {
		return nil, unresolved
	}
	return hit, found
}

func lastPart(key string) string { return key[strings.LastIndex(key, ".")+1:] }

func hasPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// triggered returns the routines a write to the table runs: its triggers for the event, or the
// routines PostgreSQL triggers execute.
func (db *database) triggered(table, event string) []*object {
	var out []*object
	fires := func(events []string) bool {
		for _, e := range events {
			if e == event || event == "MERGE" && (e == "INSERT" || e == "UPDATE" || e == "DELETE") {
				return true
			}
		}
		return false
	}
	for _, o := range db.objects {
		if o.kind == KindTrigger && o.on == table && fires(o.events) {
			out = append(out, o)
		}
	}
	for _, t := range db.triggers {
		if t.on == table && fires(t.events) {
			if o := db.objects[t.executes]; o != nil {
				out = append(out, o)
			}
		}
	}
	return out
}
