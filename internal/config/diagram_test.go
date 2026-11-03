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
