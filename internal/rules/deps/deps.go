// Package deps holds the deps/ rules: the dependency diagram is the source of truth for which of the
// repository's units may import which.
//
// Design: docs/design/rules-deps.md
package deps

import (
	"strings"

	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
)

// dirs maps each unit of product code to its location, the name a diagram uses.
func dirs(a *facts.Analysis) map[string]string {
	out := map[string]string{}
	for _, f := range a.Files {
		if f.Unit != "" && (f.Role == facts.RoleSource || f.Role == facts.RoleGenerated) {
			out[f.Unit] = f.UnitPath
		}
	}
	return out
}

// covers reports how specifically a node's label covers a directory: 0 not at all, then larger for
// a closer fit. An exact directory beats any "/..."; a longer "/..." beats a shorter one.
func covers(label, dir string) int {
	if label == dir {
		return 1 << 20
	}
	base, ok := strings.CutSuffix(label, "/...")
	switch {
	case label == "...":
		return 1
	case !ok:
		return 0
	case dir == base || strings.HasPrefix(dir, base+"/"):
		return 2 + len(base)
	}
	return 0
}

// nodeOf returns the ID of the node that covers the directory most specifically, or "".
func nodeOf(d *config.Diagram, dir string) string {
	best, score := "", 0
	for _, n := range d.Nodes {
		if s := covers(n.Label, dir); s > score {
			best, score = n.ID, s
		}
	}
	return best
}

// notInDiagram is deps/not-in-diagram.
type notInDiagram struct{}

func (notInDiagram) ID() string           { return "deps/not-in-diagram" }
func (notInDiagram) Cases() []engine.Case { return importCases() }
func (notInDiagram) About() engine.About {
	return engine.About{Finds: engine.T("an import between the repository's own units that the dependency diagram does not draw"),
		Why:  engine.T("the diagram is how people and agents know what may depend on what; an import it does not draw makes it wrong"),
		Fix:  engine.T("depend on what the diagram allows, often through an interface the importing unit declares; if the design changed, draw the arrow, which a person reviews"),
		Keys: []string{"rules.deps.diagram"}}
}

// Find reports imports between the repository's units that the diagram does not draw.
func (r notInDiagram) Find(in engine.Input) ([]engine.Finding, error) {
	d := in.Config.Diagram
	if d == nil {
		return nil, nil
	}
	allowed := map[[2]string]bool{}
	for _, e := range d.Edges {
		allowed[[2]string{e.From, e.To}] = true
	}
	unitDir := dirs(in.Facts)
	var out []engine.Finding
	for _, im := range in.Facts.Imports {
		from, to := unitDir[im.FromUnit], unitDir[im.ToUnit]
		if !im.Internal || to == "" || from == "" {
			continue
		}
		a, b := nodeOf(d, from), nodeOf(d, to)
		if a != "" && b != "" && (a == b || allowed[[2]string{a, b}]) {
			continue
		}
		out = append(out, engine.Finding{
			Rule: r.ID(), File: im.Path, Line: im.Line, Anchor: from + " -> " + to,
			Message: engine.T("import not in the diagram: %s → %s (%s)", from, to, describe(d, a, b)),
			Why:     engine.T("the diagram (%s) is how people and agents know what may depend on what; an import it does not draw makes it wrong", d.Path),
			Fix:     r.About().Fix,
		})
	}
	return out, nil
}

func describe(d *config.Diagram, a, b string) engine.Text {
	switch {
	case a == "":
		return engine.T("no node covers the importing unit")
	case b == "":
		return engine.T("no node covers the imported unit")
	}
	return engine.T("no arrow %s --> %s", d.Label(a), d.Label(b))
}

// diagramStale is deps/diagram-stale.
type diagramStale struct{}

func (diagramStale) ID() string           { return "deps/diagram-stale" }
func (diagramStale) Cases() []engine.Case { return staleCases() }
func (diagramStale) About() engine.About {
	return engine.About{Finds: engine.T("a node of the dependency diagram whose label names no unit of the version, or a planned node whose directory already holds one"),
		Why:  engine.T("a box that names nothing makes the diagram look complete when it is not"),
		Fix:  engine.T("correct the label to the unit's directory, remove the node, or mark a box not created yet with the class planned"),
		Keys: []string{"rules.deps.diagram"}}
}

// Find reports nodes of the version's own diagram whose label names no unit of the version.
func (r diagramStale) Find(in engine.Input) ([]engine.Finding, error) {
	d := in.Config.OwnDiagram
	if d == nil {
		return nil, nil
	}
	unitDir := dirs(in.Facts)
	var out []engine.Finding
	for _, n := range d.Nodes {
		names := namesAUnit(n.Label, unitDir)
		switch {
		case n.Planned && names:
			out = append(out, engine.Finding{
				Rule: r.ID(), File: d.Path, Line: n.PlannedLine, Anchor: n.Label + "\x00" + config.PlannedClass,
				Message: engine.T("the diagram's node %s is planned, but %s already holds a unit", n.ID, n.Label),
				Why:     engine.T("a planned box whose package exists tells readers it is still to be written"),
				Fix:     engine.T("remove the class planned from node %s", n.ID),
			})
		case !n.Planned && !names:
			fix := r.About().Fix
			if unitsBelow(n.Label, unitDir) {
				fix = engine.T("correct the label to the unit's directory, or to %s to take in the units under it; remove the node; or mark a box not created yet as planned", n.Label+"/...")
			}
			out = append(out, engine.Finding{
				Rule: r.ID(), File: d.Path, Line: n.Line, Anchor: n.Label,
				Message: engine.T("the diagram's node %s names %s, which holds no unit", n.ID, n.Label),
				Why:     r.About().Why,
				Fix:     fix,
			})
		}
	}
	return out, nil
}

func namesAUnit(label string, unitDir map[string]string) bool {
	for _, dir := range unitDir {
		if covers(label, dir) > 0 {
			return true
		}
	}
	return false
}

// unitsBelow reports whether units lie in subdirectories of the directory a label names, which the
// same label ending in "/..." would take in.
func unitsBelow(label string, unitDir map[string]string) bool {
	for _, dir := range unitDir {
		if strings.HasPrefix(dir, label+"/") {
			return true
		}
	}
	return false
}

// All returns the deps/ rules.
func All() []engine.Rule { return []engine.Rule{notInDiagram{}, diagramStale{}} }
