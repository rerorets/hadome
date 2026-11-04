package config

import (
	"reflect"
	"sort"
	"strconv"
)

// decode turns validated plain values into a Config. Validation has already guaranteed every type.
func decode(raw map[string]any, layers []string) Config {
	c := Config{raw: raw, Layers: layers, Hazards: map[string][]string{}}
	c.Schema, _ = raw["schema"].(int)
	c.Languages = textList(raw["languages"])
	for _, v := range list(raw["invariants"]) {
		m := v.(map[string]any)
		c.Invariants = append(c.Invariants, Invariant{
			Type: str(m["type"]), Target: str(m["target"]), Targets: textList(m["targets"]),
			ID: str(m["id"]), Says: str(m["says"]),
		})
	}
	for _, v := range list(raw["gates"]) {
		m := v.(map[string]any)
		c.Gates = append(c.Gates, Gate{ID: str(m["id"]), Kind: str(m["kind"]), Run: textList(m["run"]),
			Paths: textList(m["paths"]), Timeout: str(m["timeout"]), Options: m["options"]})
	}
	tests := object(object(raw["rules"])["tests"])
	for category, v := range object(tests["hazards"]) {
		c.Hazards[category] = textList(v)
	}
	c.IO = textList(tests["io"])
	c.Assertions = textList(tests["assertions"])
	size := object(object(raw["rules"])["size"])
	c.Size = Limits{}
	for _, k := range sizeKey.Fields {
		c.Size[k.Name] = intOr(size[k.Name], k.Default.(int))
	}
	c.TodoForm = DefaultTodo
	if form, ok := object(object(raw["rules"])["comments"])["todo"].(string); ok {
		c.TodoForm = form
	}
	return c
}

func orDefault(v, def any) any {
	if v == nil {
		return def
	}
	return v
}

func intOr(v any, def int) int {
	if i, ok := v.(int); ok {
		return i
	}
	return def
}

func list(v any) []any { l, _ := v.([]any); return l }
func str(v any) string { s, _ := v.(string); return s }

func object(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func textList(v any) []string {
	var out []string
	for _, e := range list(v) {
		out = append(out, str(e))
	}
	return out
}

// Change is one difference between the configurations of base and head.
type Change struct {
	Key     string `json:"key"`
	Removed string `json:"removed,omitempty"`
	Added   string `json:"added,omitempty"`
	// Looser means the change makes the rules more permissive than in base.
	Looser bool `json:"looser"`
}

// Diff lists what changed from base to head. Which changes are looser comes from the
// specification, so a new key needs no code here.
func Diff(base, head Config) []Change {
	var out []Change
	diffObject(&out, base.raw, head.raw, Spec, "")
	out = append(out, diffArrows(base.Diagram, head.Diagram)...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Key != out[j].Key {
			return out[i].Key < out[j].Key
		}
		return out[i].Removed+out[i].Added < out[j].Removed+out[j].Added
	})
	return out
}

func diffObject(out *[]Change, base, head map[string]any, k Key, path string) {
	for _, f := range k.Fields {
		p := joinPath(path, f.Name)
		b, h := base[f.Name], head[f.Name]
		switch {
		case f.Kind == KindObject:
			diffObject(out, object(b), object(h), f, p)
		case f.Kind == KindList:
			if h == nil && f.OmittedIsWidest {
				continue
			}
			diffList(out, list(b), list(h), f, p)
		case f.Kind == KindInt:
			def, _ := f.Default.(int)
			bi, hi := intOr(b, def), intOr(h, def)
			if bi != hi {
				*out = append(*out, Change{Key: p, Removed: strconv.Itoa(bi), Added: strconv.Itoa(hi),
					Looser: f.Looser == LooserWhenHigher && hi > bi})
			}
		case f.Default != nil:
			if bs, hs := fmtValue(orDefault(b, f.Default)), fmtValue(orDefault(h, f.Default)); bs != hs {
				*out = append(*out, Change{Key: p, Removed: bs, Added: hs, Looser: f.Looser == LooserWhenChanged})
			}
		case !reflect.DeepEqual(b, h):
			*out = append(*out, Change{Key: p, Removed: fmtValue(b), Added: fmtValue(h)})
		}
	}
}

func diffList(out *[]Change, base, head []any, k Key, path string) {
	ids := func(l []any) map[string]bool {
		m := map[string]bool{}
		for _, v := range l {
			m[identityOf(v, *k.Elem, k.Identity)] = true
		}
		return m
	}
	b, h := ids(base), ids(head)
	for id := range b {
		if !h[id] {
			*out = append(*out, Change{Key: path, Removed: id, Looser: k.Looser != LooserNever && k.Looser != LooserWhenAdded})
		}
	}
	if k.Looser == LooserWhenChanged {
		diffChanged(out, base, head, k, path)
	}
	for id := range h {
		if !b[id] {
			*out = append(*out, Change{Key: path, Added: id, Looser: k.Looser == LooserWhenAdded})
		}
	}
}

// diffChanged reports entries that keep their identity but change their content.
func diffChanged(out *[]Change, base, head []any, k Key, path string) {
	byID := map[string]any{}
	for _, v := range base {
		byID[identityOf(v, *k.Elem, k.Identity)] = v
	}
	for _, v := range head {
		id := identityOf(v, *k.Elem, k.Identity)
		if old, ok := byID[id]; ok && !reflect.DeepEqual(old, v) {
			*out = append(*out, Change{Key: path, Removed: fmtValue(old), Added: fmtValue(v), Looser: true})
		}
	}
}

// diffArrows compares what the diagrams allow, arrow by arrow, by the labels at their ends: a new
// arrow, or a label that now names more, loosens the rules. Without a diagram nothing is checked, so
// adding the first one only tightens, and removing it loosens everything.
func diffArrows(base, head *Diagram) []Change {
	switch {
	case base == nil:
		return nil
	case head == nil:
		return []Change{{Key: "rules.deps.diagram", Removed: base.Path, Looser: true}}
	}
	arrows := func(d *Diagram) map[string]bool {
		m := map[string]bool{}
		if d != nil {
			for _, e := range d.Edges {
				m[d.Label(e.From)+" --> "+d.Label(e.To)] = true
			}
		}
		return m
	}
	b, h := arrows(base), arrows(head)
	var out []Change
	for a := range h {
		if !b[a] {
			out = append(out, Change{Key: "rules.deps.diagram", Added: a, Looser: true})
		}
	}
	for a := range b {
		if !h[a] {
			out = append(out, Change{Key: "rules.deps.diagram", Removed: a})
		}
	}
	return out
}

func fmtValue(v any) string {
	if v == nil {
		return ""
	}
	return identityOf(v, Key{}, nil)
}
