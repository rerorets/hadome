package config

import (
	"sort"
	"strings"
	"testing"
	"testing/fstest"
)

func arrows(d *Diagram) string {
	var out []string
	for _, e := range d.Edges {
		out = append(out, d.Label(e.From)+">"+d.Label(e.To))
	}
	sort.Strings(out)
	return strings.Join(out, " ")
}

// viewpoint: behaviour
// target: config.Load with rules.deps.diagram
// The diagram is read the way Mermaid draws it: chains, &, labels given after first use, and
// lines without arrows allow nothing.
func TestTheDiagramIsReadLikeMermaid(t *testing.T) {
	doc := "# Dependencies\n\n```mermaid\nflowchart LR\n  %% comment\n  cmd[\"cmd/...\"] --> core[\"internal/core\"] --> domain\n" +
		"  cmd & core -.-> store\n  store[(\"internal/store\")] ==>|reads| domain[internal/domain]\n  subgraph x\n  end\n" +
		"  core --- store\n  style core fill:#fff\n```\n\n```mermaid\nsequenceDiagram\n  a->>b: hi\n```\n"
	c := mustLoadTree(t, fstest.MapFS{".hadome.yml": {Data: []byte("schema: 1\nrules:\n  deps:\n    diagram: docs/deps.md\n")},
		"docs/deps.md": {Data: []byte(doc)}})
	want := "cmd/...>internal/core cmd/...>internal/store internal/core>internal/domain internal/core>internal/store internal/store>internal/domain"
	if got := arrows(c.Diagram); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

// viewpoint: behaviour
// target: config.Load with rules.deps.diagram
// A box may describe its directory after a line break; only the first line names the directory,
// so editing the description changes nothing the diagram allows.
func TestALabelMayDescribeItsDirectoryAfterALineBreak(t *testing.T) {
	doc := "```mermaid\nflowchart LR\n  core[\"internal/core<br/>the core. Holds the rules\"] --> store[\"internal/store <BR> where data lives\"]\n" +
		"  store --> domain[\"internal/domain<br>\"]\n```\n"
	c := mustLoadTree(t, fstest.MapFS{".hadome.yml": {Data: []byte("schema: 1\nrules:\n  deps:\n    diagram: docs/deps.md\n")},
		"docs/deps.md": {Data: []byte(doc)}})
	want := "internal/core>internal/store internal/store>internal/domain"
	if got := arrows(c.Diagram); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	reworded := mustLoadTree(t, fstest.MapFS{".hadome.yml": {Data: []byte("schema: 1\nrules:\n  deps:\n    diagram: docs/deps.md\n")},
		"docs/deps.md": {Data: []byte(strings.ReplaceAll(doc, "the core. Holds the rules", "rewritten"))}})
	for _, ch := range Diff(c, reworded) {
		if ch.Looser {
			t.Errorf("rewording a description loosens %s: %s → %s", ch.Key, ch.Removed, ch.Added)
		}
	}
}

// viewpoint: behaviour
// target: config.Load with rules.deps.diagram
// A box is planned when it has the class planned, given on the node or by a class statement before
// or after it; any other class leaves it as it is.
func TestABoxIsPlannedByItsClass(t *testing.T) {
	doc := "```mermaid\nflowchart LR\n  class b planned\n  a[\"internal/a\"]:::planned --> b[\"internal/b\"]\n" +
		"  c[\"internal/c\"]:::wide --> d\n  class d,a planned\n```\n"
	c := mustLoadTree(t, fstest.MapFS{".hadome.yml": {Data: []byte("schema: 1\nrules:\n  deps:\n    diagram: d.md\n")},
		"d.md": {Data: []byte(doc)}})
	got := map[string]bool{}
	for _, n := range c.Diagram.Nodes {
		got[n.ID] = n.Planned
	}
	want := map[string]bool{"a": true, "b": true, "c": false, "d": true}
	for id, planned := range want {
		if got[id] != planned {
			t.Errorf("node %s: planned %v, want %v", id, got[id], planned)
		}
	}
	if arrows(c.Diagram) != "internal/a>internal/b internal/c>d" {
		t.Errorf("a class must not change the arrows: %s", arrows(c.Diagram))
	}
}

func mustLoadTree(t *testing.T, tree fstest.MapFS) Config {
	t.Helper()
	c, err := Load(tree, t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return c
}

// viewpoint: failure handling
// target: config.Load with rules.deps.diagram
// The diagram is configuration: one that cannot be read never lets a check run, and says where.
func TestABrokenDiagramIsAConfigurationError(t *testing.T) {
	cases := map[string]struct {
		doc  string
		line int
	}{
		"missing file":         {"", 1},
		"no flowchart":         {"# Nothing here\n", 1},
		"not a path":           {"```mermaid\nflowchart LR\n  a[\"The core\"] --> b\n```\n", 3},
		"two labels":           {"```mermaid\nflowchart LR\n  a[x] --> b\n  a[y] --> b\n```\n", 4},
		"unreadable statement": {"```mermaid\nflowchart LR\n  a --> \n```\n", 3},
		"planned unknown node": {"```mermaid\nflowchart LR\n  a --> b\n  class c planned\n```\n", 4},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			tree := fstest.MapFS{".hadome.yml": {Data: []byte("schema: 1\nrules:\n  deps:\n    diagram: docs/deps.md\n")}}
			if tc.doc != "" {
				tree["docs/deps.md"] = &fstest.MapFile{Data: []byte(tc.doc)}
			}
			_, err := Load(tree, t.TempDir())
			es, ok := err.(Errors)
			if !ok || es[0].Key != "rules.deps.diagram" || es[0].Line != tc.line {
				t.Fatalf("want an error on rules.deps.diagram at line %d, got %v", tc.line, err)
			}
		})
	}
}

// Invariant 5 of docs/design/configuration.md, for the diagram: drawing an arrow, widening a label or
// dropping the diagram needs review; erasing an arrow, or adding the first diagram, does not.
func TestDrawingAnArrowNeedsReview(t *testing.T) {
	load := func(flow string) Config {
		return mustLoadTree(t, fstest.MapFS{".hadome.yml": {Data: []byte("schema: 1\nrules:\n  deps:\n    diagram: d.md\n")},
			"d.md": {Data: []byte("```mermaid\nflowchart LR\n" + flow + "\n```\n")}})
	}
	one, two := load("a --> b"), load("a --> b\n  a --> c")
	none := mustLoadTree(t, fstest.MapFS{".hadome.yml": {Data: []byte("schema: 1\n")}})
	wide := load("a[\"a/...\"] --> b")
	for name, tc := range map[string]struct {
		base, head Config
		want       int
	}{"adding an arrow": {one, two, 1}, "removing one": {two, one, 0}, "widening a label": {one, wide, 1},
		"adding the first diagram": {none, two, 0}, "removing the diagram": {two, none, 1}} {
		if got := looser(Diff(tc.base, tc.head)); len(got) != tc.want {
			t.Errorf("%s: want %d looser changes, got %v", name, tc.want, got)
		}
	}
}

// viewpoint: state integrity
// target: config.Diff
// Marking a box base already draws as planned is looser, and the change points at the mark, so a
// reviewer lands on it. Drawing a new box planned, or removing the mark, is not looser.
func TestMarkingAnExistingBoxPlannedIsLooser(t *testing.T) {
	load := func(flow string) Config {
		return mustLoadTree(t, fstest.MapFS{".hadome.yml": {Data: []byte("schema: 1\nrules:\n  deps:\n    diagram: d.md\n")},
			"d.md": {Data: []byte("```mermaid\nflowchart LR\n" + flow + "\n```\n")}})
	}
	looser := func(base, head Config) []Change {
		var out []Change
		for _, ch := range Diff(base, head) {
			if ch.Looser {
				out = append(out, ch)
			}
		}
		return out
	}
	plain, marked := load("  a --> b"), load("  a --> b\n  class b planned")
	got := looser(plain, marked)
	if len(got) != 1 || got[0].Added != "b planned" || got[0].File != "d.md" || got[0].Line != 4 {
		t.Fatalf("want b planned at d.md:4 as looser, got %+v", got)
	}
	if got := looser(marked, plain); len(got) != 0 {
		t.Errorf("removing the mark tightens, got %+v", got)
	}
	if got := looser(plain, load("  a --> b\n  c[\"internal/c\"]:::planned")); len(got) != 0 {
		t.Errorf("a new box drawn planned is not looser, got %+v", got)
	}
}
