package config

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// decode turns validated plain values into a Config. Validation has already guaranteed every type.
func decode(raw map[string]any, layers []string) Config {
	c := Config{raw: raw, Layers: layers, Hazards: map[string][]string{}}
	c.Schema, _ = raw["schema"].(int)
	c.Languages = textList(raw["languages"])
	c.Messages = str(orDefault(raw["messages"], "en"))
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
	if m, ok := raw["mutation"].(map[string]any); ok {
		c.Mutation = &Mutation{Test: textList(m["test"]), Timeout: str(m["timeout"]),
			MaxMutants: intOr(m["max-mutants"], 100), TestsSeparator: str(orDefault(m["tests-separator"], "|"))}
	}
	files := object(raw["files"])
	c.TestFiles = textList(files["tests"])
	c.GeneratedFiles = textList(files["generated"])
	c.AnsibleFiles = textList(files["ansible"])
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
	Key string `json:"key"`
	// File and Line are where in head the change is: the key in .hadome.yml, or the arrow in the
	// diagram. A removal points at what holds the removed entry; Line is 0 when nothing does.
	File    string `json:"file"`
	Line    int    `json:"line,omitempty"`
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
	for i := range out {
		out[i].File, out[i].Line = FileName, head.lineOf(out[i])
	}
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
		case f.Kind == KindObject && f.OmittedIsOff && b == nil:
			diffTurnedOn(out, object(h), f, p)
		case f.Kind == KindObject:
			diffObject(out, object(b), object(h), f, p)
		case f.Kind == KindList:
			if h == nil && f.OmittedIsWidest {
				continue
			}
			diffList(out, list(b), list(h), f, p)
		case f.Kind == KindInt:
			diffInt(out, b, h, f, p)
		case f.Default != nil:
			if bs, hs := fmtValue(orDefault(b, f.Default)), fmtValue(orDefault(h, f.Default)); bs != hs {
				*out = append(*out, Change{Key: p, Removed: bs, Added: hs, Looser: f.Looser == LooserWhenChanged})
			}
		case !reflect.DeepEqual(b, h):
			*out = append(*out, Change{Key: p, Removed: fmtValue(b), Added: fmtValue(h)})
		}
	}
}

// diffTurnedOn lists what an OmittedIsOff object holds in head when base has none. Base did not
// run the feature at all, so nothing in it is looser.
func diffTurnedOn(out *[]Change, head map[string]any, k Key, path string) {
	var added []Change
	diffObject(&added, nil, head, k, path)
	for _, c := range added {
		c.Looser = false
		*out = append(*out, c)
	}
}

func diffInt(out *[]Change, b, h any, k Key, path string) {
	def, _ := k.Default.(int)
	bi, hi := intOr(b, def), intOr(h, def)
	if bi != hi {
		*out = append(*out, Change{Key: path, Removed: strconv.Itoa(bi), Added: strconv.Itoa(hi),
			Looser: k.Looser == LooserWhenHigher && hi > bi || k.Looser == LooserWhenLower && hi < bi})
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
			*out = append(*out, Change{Key: path, Added: id, Looser: k.Looser == LooserWhenAdded || k.Looser == LooserAlways})
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
		return []Change{{Key: "rules.deps.diagram", File: FileName, Removed: base.Path, Looser: true}}
	}
	arrows := func(d *Diagram) map[string]int {
		m := map[string]int{}
		for _, e := range d.Edges {
			if a := d.Label(e.From) + " --> " + d.Label(e.To); m[a] == 0 {
				m[a] = e.Line
			}
		}
		return m
	}
	b, h := arrows(base), arrows(head)
	var out []Change
	for a, line := range h {
		if _, ok := b[a]; !ok {
			out = append(out, Change{Key: "rules.deps.diagram", File: head.Path, Line: line, Added: a, Looser: true})
		}
	}
	for a := range b {
		if _, ok := h[a]; !ok {
			out = append(out, Change{Key: "rules.deps.diagram", File: head.Path, Removed: a})
		}
	}
	return append(out, diffPlanned(base, head)...)
}

// diffPlanned lists boxes base draws unplanned and head marks planned. That is looser: one mark
// would take a box that stopped naming anything out of deps/diagram-stale. A new box drawn planned
// is the intended use, and removing the mark tightens, so neither is listed.
func diffPlanned(base, head *Diagram) []Change {
	was := map[string]bool{}
	for _, n := range base.Nodes {
		was[n.ID] = n.Planned
	}
	var out []Change
	for _, n := range head.Nodes {
		if planned, ok := was[n.ID]; ok && !planned && n.Planned {
			out = append(out, Change{Key: "rules.deps.diagram", File: head.Path, Line: n.PlannedLine,
				Added: n.ID + " " + PlannedClass, Looser: true})
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

// lineOf finds the line of a change in head's .hadome.yml: the added or changed list entry when
// there is one, or else the key itself, or else the nearest enclosing key that head has.
func (c Config) lineOf(ch Change) int {
	if ch.Added != "" {
		if k, ok := lookup(Spec, strings.Split(ch.Key, ".")); ok && k.Kind == KindList {
			for i, v := range list(valueAt(c.raw, ch.Key)) {
				if identityOf(v, *k.Elem, k.Identity) == ch.Added || fmtValue(v) == ch.Added {
					if line := c.lines[fmt.Sprintf("%s[%d]", ch.Key, i)]; line > 0 {
						return line
					}
				}
			}
		}
	}
	for p := ch.Key; p != ""; p = parentPath(p) {
		if line := c.lines[p]; line > 0 {
			return line
		}
	}
	return 0
}

// valueAt returns the raw value at a dotted key path.
func valueAt(raw map[string]any, path string) any {
	var v any = raw
	for _, name := range strings.Split(path, ".") {
		v = object(v)[name]
	}
	return v
}

func parentPath(p string) string {
	if i := strings.LastIndex(p, "."); i >= 0 {
		return p[:i]
	}
	return ""
}
