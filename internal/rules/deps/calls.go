package deps

import (
	"sort"
	"strings"

	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
)

// callEdge is one call between two of the repository's units, at the caller.
type callEdge struct {
	fromUnit, toUnit string
	path             string
	line             int
	caller, callee   string
}

// callEdges lists the calls of product code from one unit to another, as far as hadome can follow
// them: the calls of symbols, and of entry-point code that is no symbol. A callee that is no symbol
// of the version (test or generated code) is not counted, nor is an interface's method: it has no
// body, so the call does not reach the unit declaring the interface.
func callEdges(a *facts.Analysis) []callEdge {
	unitOf := map[string]string{}
	for _, f := range a.Files {
		unitOf[f.Path] = f.Unit
	}
	interfaces := map[string]bool{}
	for _, s := range a.Symbols {
		interfaces[s.ID] = s.Kind == facts.SymbolInterface
	}
	declared := map[string]string{}
	for _, s := range a.Symbols {
		if i := strings.LastIndex(s.ID, "."); i < 0 || !interfaces[s.ID[:i]] {
			declared[s.ID] = unitOf[s.Path]
		}
	}
	var out []callEdge
	add := func(path string, line int, caller string, calls []string) {
		from := unitOf[path]
		for _, c := range calls {
			if to := declared[c]; from != "" && to != "" && to != from {
				out = append(out, callEdge{fromUnit: from, toUnit: to, path: path, line: line, caller: caller, callee: c})
			}
		}
	}
	for _, s := range a.Symbols {
		add(s.Path, s.Line, s.ID, s.Calls)
	}
	for _, e := range a.Entries {
		if e.Symbol == "" {
			add(e.Path, e.Line, "", e.Calls)
		}
	}
	return out
}

// callNotInDiagram is deps/call-not-in-diagram.
type callNotInDiagram struct{}

func (callNotInDiagram) ID() string           { return "deps/call-not-in-diagram" }
func (callNotInDiagram) Cases() []engine.Case { return callCases() }
func (callNotInDiagram) About() engine.About {
	return engine.About{Finds: engine.T("a call between the repository's own units that the call diagram does not draw"),
		Why:  engine.T("the call diagram tells which layers a request goes through; a call it does not draw can skip a layer or go backwards unseen"),
		Fix:  engine.T("call through the layers the call diagram allows; if the design changed, draw the arrow, which a person reviews"),
		Keys: []string{"rules.deps.calls"}}
}

// Find reports, once for each pair of units, calls the call diagram does not draw. Calls from a
// wiring box are not checked.
func (r callNotInDiagram) Find(in engine.Input) ([]engine.Finding, error) {
	d := in.Config.Calls
	if d == nil {
		return nil, nil
	}
	allowed := map[[2]string]bool{}
	for _, e := range d.Edges {
		allowed[[2]string{e.From, e.To}] = true
	}
	wiring := map[string]bool{}
	for _, n := range d.Nodes {
		wiring[n.ID] = n.Wiring
	}
	unitDir := dirs(in.Facts)
	edges := callEdges(in.Facts)
	sort.SliceStable(edges, func(i, j int) bool {
		if edges[i].path != edges[j].path {
			return edges[i].path < edges[j].path
		}
		return edges[i].line < edges[j].line
	})
	seen := map[[2]string]bool{}
	var out []engine.Finding
	for _, c := range edges {
		from, to := unitDir[c.fromUnit], unitDir[c.toUnit]
		if from == "" || to == "" || seen[[2]string{from, to}] {
			continue
		}
		a, b := nodeOf(d, from), nodeOf(d, to)
		if wiring[a] || a != "" && b != "" && (a == b || allowed[[2]string{a, b}]) {
			continue
		}
		seen[[2]string{from, to}] = true
		out = append(out, engine.Finding{
			Rule: r.ID(), File: c.path, Line: c.line, Anchor: from + " -> " + to,
			Message: engine.T("call not in the call diagram: %s → %s (%s): %s calls %s", from, to, describeCall(d, a, b), caller(c), c.callee),
			Why:     engine.T("the call diagram (%s) tells which layers a request goes through; a call it does not draw can skip a layer or go backwards unseen", d.Path),
			Fix:     r.About().Fix,
		})
	}
	return out, nil
}

// caller names the calling function, or entry-point code that is no symbol.
func caller(c callEdge) any {
	if c.caller == "" {
		return engine.T("the entry point")
	}
	return c.caller
}

func describeCall(d *config.Diagram, a, b string) engine.Text {
	switch {
	case a == "":
		return engine.T("no node covers the calling unit")
	case b == "":
		return engine.T("no node covers the called unit")
	}
	return engine.T("no arrow %s --> %s", d.Label(a), d.Label(b))
}

// diagrams returns the version's own diagrams that are set: the dependency diagram, then the call
// diagram.
func diagrams(c config.Config) []*config.Diagram {
	var out []*config.Diagram
	for _, d := range []*config.Diagram{c.OwnDiagram, c.OwnCalls} {
		if d != nil {
			out = append(out, d)
		}
	}
	return out
}
