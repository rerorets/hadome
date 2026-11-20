// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

package config

import (
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strings"
)

// Diagram is the dependency diagram that rules.deps.diagram names, or the call diagram that
// rules.deps.calls names: the nodes and arrows of the Mermaid flowcharts in a Markdown file.
type Diagram struct {
	Path  string // the file, from the repository root
	Key   string // the setting that names the file
	Nodes []Node
	Edges []Edge
}

// Node is one box of the diagram. Label names a directory, or a directory and everything under
// it when it ends in "/...". A description after a line break (<br/>) is not part of it.
type Node struct {
	ID    string
	Label string
	Line  int
	// Planned marks a box drawn before its directory is created: it has the class planned.
	// PlannedLine is where the class is given, on the node or in a class statement.
	Planned     bool
	PlannedLine int
	// Wiring marks, in the call diagram, the box of the code that makes and wires everything: its
	// calls are not checked. WiringLine is where the class is given.
	Wiring     bool
	WiringLine int
}

// The settings that name the dependency diagram and the call diagram.
const (
	DiagramKey = "rules.deps.diagram"
	CallsKey   = "rules.deps.calls"
)

// loadDiagram reads a diagram from the version's tree: it is part of the repository, like the code
// it describes.
func loadDiagram(product fs.FS, path, key string) (*Diagram, error) {
	data, err := fs.ReadFile(product, path)
	if err != nil {
		example := "diagram: docs/conventions/dependencies.md"
		if key == CallsKey {
			example = "calls: docs/conventions/calls.md"
		}
		return nil, Errors{{File: FileName, Line: 1, Key: key,
			Problem: fmt.Sprintf("cannot read the diagram %s: %v", path, errors.Unwrap(err)),
			Example: example}}
	}
	d, errs := parseDiagram(path, key, data)
	if len(errs) > 0 {
		return nil, errs
	}
	return d, nil
}

// PlannedClass is the Mermaid class that marks a planned box; WiringClass marks a wiring box.
const (
	PlannedClass = "planned"
	WiringClass  = "wiring"
)

// Edge is one arrow: the units of From may import, or call, the units of To. Both are node IDs.
type Edge struct {
	From, To string
	Line     int
}

// Label returns the label of the node with this ID.
func (d *Diagram) Label(id string) string {
	for _, n := range d.Nodes {
		if n.ID == id {
			return n.Label
		}
	}
	return ""
}

var (
	nodeID    = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*`)
	directed  = regexp.MustCompile(`^\s*(?:-->|-\.->|==>|--\s[^>]*?\s-->)(?:\|[^|]*\|)?\s*`)
	undirect  = regexp.MustCompile(`^\s*(?:---|-\.-|===)(?:\|[^|]*\|)?\s*`)
	ampersand = regexp.MustCompile(`^\s*&\s*`)
	dirPath   = regexp.MustCompile(`^(\.|\.\.\.|[A-Za-z0-9_.-]+(/[A-Za-z0-9_.-]+)*(/\.\.\.)?)$`)
	// shapes are Mermaid's node shapes, longest opening first; the label is the first group.
	shapes = []*regexp.Regexp{
		regexp.MustCompile(`^\[\["?(.*?)"?\]\]`), regexp.MustCompile(`^\[\("?(.*?)"?\)\]`),
		regexp.MustCompile(`^\(\("?(.*?)"?\)\)`), regexp.MustCompile(`^\{\{"?(.*?)"?\}\}`),
		regexp.MustCompile(`^\["?(.*?)"?\]`), regexp.MustCompile(`^\("?(.*?)"?\)`),
		regexp.MustCompile(`^\{"?(.*?)"?\}`), regexp.MustCompile(`^>"?(.*?)"?\]`),
	}
	// lineBreak starts a label's description: the first line names the directory, the rest is prose.
	lineBreak = regexp.MustCompile(`(?i)<br\s*/?>`)
	// classSuffix is a node's ":::class"; classStmt is "class id1,id2 class".
	classSuffix = regexp.MustCompile(`^:::([A-Za-z0-9_-]+)`)
	classStmt   = regexp.MustCompile(`^class\s+([A-Za-z0-9_.,-]+)\s+([A-Za-z0-9_-]+)\s*$`)
	ignored     = regexp.MustCompile(`^(%%|subgraph\b|end\b|classDef\b|class\b|style\b|linkStyle\b|click\b|direction\b)`)
)

// parseDiagram reads the flowcharts of a Markdown file that the setting key names. Errors carry the
// line they are on.
func parseDiagram(path, key string, data []byte) (*Diagram, Errors) {
	p := &diagramParser{d: &Diagram{Path: path, Key: key}, index: map[string]int{}, explicit: map[string]bool{}}
	inBlock, header := false, false
	for i, raw := range strings.Split(string(data), "\n") {
		line, n := strings.TrimSpace(raw), i+1
		switch {
		case !inBlock:
			inBlock, header = line == "```mermaid" || line == "~~~mermaid", false
		case line == "```" || line == "~~~":
			inBlock = false
		case !header:
			if line == "" || strings.HasPrefix(line, "%%") {
				continue
			}
			header = true
			if !strings.HasPrefix(line, "flowchart") && !strings.HasPrefix(line, "graph") {
				inBlock = false // another kind of diagram
			}
		default:
			for _, stmt := range strings.Split(line, ";") {
				p.statement(strings.TrimSpace(stmt), n)
			}
		}
	}
	p.markClasses()
	if len(p.errs) == 0 && len(p.d.Nodes) == 0 {
		p.fail(1, "holds no Mermaid flowchart; put the diagram in a ```mermaid block starting with flowchart")
	}
	return p.d, p.errs
}

type diagramParser struct {
	d        *Diagram
	index    map[string]int  // node ID -> position in d.Nodes
	explicit map[string]bool // whether the node's label was written, not taken from its ID
	// classes are the uses of the classes planned and wiring, by line. Mermaid lets a class statement
	// come before or after the node, so they are applied once every node is known.
	classes []classUse
	errs    Errors
}

type classUse struct {
	id, class string
	line      int
}

// markClasses applies the classes planned and wiring to the nodes they name.
func (p *diagramParser) markClasses() {
	for _, u := range p.classes {
		i, ok := p.index[u.id]
		if !ok {
			p.fail(u.line, fmt.Sprintf("class %s %s names node %s, which the diagram does not draw", u.id, u.class, u.id))
			continue
		}
		n := &p.d.Nodes[i]
		switch {
		case u.class == PlannedClass && !n.Planned:
			n.Planned, n.PlannedLine = true, u.line
		case u.class == WiringClass && !n.Wiring:
			n.Wiring, n.WiringLine = true, u.line
		}
	}
}

// marks reports whether a class means something to the diagram: planned in both, wiring in the call
// diagram only, so that a dependency diagram styling a box wiring reads as it did.
func (p *diagramParser) marks(class string) bool {
	return class == PlannedClass || class == WiringClass && p.d.Key == CallsKey
}

func (p *diagramParser) fail(line int, problem string) {
	p.errs = append(p.errs, Error{File: p.d.Path, Line: line, Key: p.d.Key, Problem: problem,
		Example: `a["internal/store"] --> b["internal/domain"]`})
}

// statement reads "group (arrow group)*", where a group is "node (& node)*".
func (p *diagramParser) statement(s string, line int) {
	if m := classStmt.FindStringSubmatch(s); m != nil {
		if p.marks(m[2]) {
			for _, id := range strings.Split(m[1], ",") {
				p.classes = append(p.classes, classUse{id: id, class: m[2], line: line})
			}
		}
		return
	}
	if s == "" || ignored.MatchString(s) {
		return
	}
	var prev []string
	arrow := false
	for {
		group, rest, ok := p.group(s, line)
		if !ok {
			p.fail(line, fmt.Sprintf("cannot read %q as nodes and arrows", s))
			return
		}
		if arrow {
			for _, from := range prev {
				for _, to := range group {
					p.d.Edges = append(p.d.Edges, Edge{From: from, To: to, Line: line})
				}
			}
		}
		if rest == "" {
			return
		}
		m := directed.FindString(rest)
		arrow = m != ""
		if !arrow {
			m = undirect.FindString(rest)
		}
		if m == "" {
			p.fail(line, fmt.Sprintf("cannot read %q as nodes and arrows", s))
			return
		}
		prev, s = group, rest[len(m):]
	}
}

func (p *diagramParser) group(s string, line int) ([]string, string, bool) {
	var ids []string
	for {
		id, rest, ok := p.node(s, line)
		if !ok {
			return nil, "", false
		}
		ids = append(ids, id)
		m := ampersand.FindString(rest)
		if m == "" {
			return ids, strings.TrimSpace(rest), true
		}
		s = rest[len(m):]
	}
}

func (p *diagramParser) node(s string, line int) (string, string, bool) {
	id := nodeID.FindString(s)
	if id == "" {
		return "", "", false
	}
	rest, label := s[len(id):], ""
	for _, shape := range shapes {
		if m := shape.FindStringSubmatch(rest); m != nil {
			label, rest = m[1], rest[len(m[0]):]
			if at := lineBreak.FindStringIndex(label); at != nil {
				label = strings.TrimSpace(label[:at[0]])
			}
			break
		}
	}
	p.define(id, label, line)
	if m := classSuffix.FindStringSubmatch(rest); m != nil {
		rest = rest[len(m[0]):]
		if p.marks(m[1]) {
			p.classes = append(p.classes, classUse{id: id, class: m[1], line: line})
		}
	}
	return id, rest, true
}

// define records a node. Mermaid allows a node to be used before its label is given, so a node first
// seen without a label takes the label it is given later.
func (p *diagramParser) define(id, label string, line int) {
	i, known := p.index[id]
	switch {
	case !known:
		p.index[id], p.explicit[id] = len(p.d.Nodes), label != ""
		if label == "" {
			label = id
		}
		p.d.Nodes = append(p.d.Nodes, Node{ID: id, Label: label, Line: line})
		p.check(p.d.Nodes[len(p.d.Nodes)-1], line)
	case label == "":
	case p.explicit[id] && p.d.Nodes[i].Label != label:
		p.fail(line, fmt.Sprintf("node %s is labelled both %q and %q", id, p.d.Nodes[i].Label, label))
	case !p.explicit[id]:
		p.d.Nodes[i] = Node{ID: id, Label: label, Line: line}
		p.explicit[id] = true
		p.check(p.d.Nodes[i], line)
	}
}

func (p *diagramParser) check(n Node, line int) {
	if !dirPath.MatchString(n.Label) {
		p.fail(line, fmt.Sprintf("the label %q of node %s is not a directory path such as internal/store or internal/store/...", n.Label, n.ID))
	}
}
