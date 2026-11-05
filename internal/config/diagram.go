package config

import (
	"fmt"
	"regexp"
	"strings"
)

// Diagram is the dependency diagram that rules.deps.diagram names: the nodes and arrows of the
// Mermaid flowcharts in a Markdown file.
type Diagram struct {
	Path  string // the file, from the repository root
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
}

// PlannedClass is the Mermaid class that marks a planned box.
const PlannedClass = "planned"

// Edge is one arrow: the units of From may import the units of To. Both are node IDs.
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

// parseDiagram reads the flowcharts of a Markdown file. Errors carry the line they are on.
func parseDiagram(path string, data []byte) (*Diagram, Errors) {
	p := &diagramParser{d: &Diagram{Path: path}, index: map[string]int{}, explicit: map[string]bool{}}
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
	p.markPlanned()
	if len(p.errs) == 0 && len(p.d.Nodes) == 0 {
		p.fail(1, "holds no Mermaid flowchart; put the diagram in a ```mermaid block starting with flowchart")
	}
	return p.d, p.errs
}

type diagramParser struct {
	d        *Diagram
	index    map[string]int  // node ID -> position in d.Nodes
	explicit map[string]bool // whether the node's label was written, not taken from its ID
	// planned are the IDs a class statement marks planned, by line. Mermaid lets the statement come
	// before or after the node, so they are applied once every node is known.
	planned []classUse
	errs    Errors
}

type classUse struct {
	id   string
	line int
}

// markPlanned applies the class statements that mark nodes planned.
func (p *diagramParser) markPlanned() {
	for _, u := range p.planned {
		i, ok := p.index[u.id]
		if !ok {
			p.fail(u.line, fmt.Sprintf("class %s %s names node %s, which the diagram does not draw", u.id, PlannedClass, u.id))
			continue
		}
		if !p.d.Nodes[i].Planned {
			p.d.Nodes[i].Planned, p.d.Nodes[i].PlannedLine = true, u.line
		}
	}
}

func (p *diagramParser) fail(line int, problem string) {
	p.errs = append(p.errs, Error{File: p.d.Path, Line: line, Key: "rules.deps.diagram", Problem: problem,
		Example: `a["internal/store"] --> b["internal/domain"]`})
}

// statement reads "group (arrow group)*", where a group is "node (& node)*".
func (p *diagramParser) statement(s string, line int) {
	if m := classStmt.FindStringSubmatch(s); m != nil {
		if m[2] == PlannedClass {
			for _, id := range strings.Split(m[1], ",") {
				p.planned = append(p.planned, classUse{id: id, line: line})
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
		if m[1] == PlannedClass {
			p.planned = append(p.planned, classUse{id: id, line: line})
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
