package sql

import (
	"regexp"
	"slices"
	"strings"
)

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
// COMMENT = '…', BigQuery's OPTIONS(description = …). The definition's lines are those of a
// statement parsed from f, so they are always within it.
func describes(f *sqlFile, def Definition) bool {
	lines := strings.Split(string(f.src), "\n")
	return describedBy.MatchString(strings.Join(lines[def.Line-1:def.EndLine], "\n"))
}
