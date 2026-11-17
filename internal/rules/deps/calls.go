package deps

import (
	"sort"
	"strings"

	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
)

// callEdge is one call between two of the repository's units, at the line of the call, or of the
// caller where an adapter does not know it. Via is the method the code names when the call lands on
// another one, an implementation of it; it is empty when the callee is the one named.
type callEdge struct {
	fromUnit, toUnit string
	path             string
	line             int
	caller, callee   string
	via              string
	outside          bool // the caller is the file's code outside functions
}

// callEdges lists the calls of product code from one unit to another, as far as hadome can follow
// them: the calls of symbols, of entry-point code that is no symbol, and of files outside their
// functions. A callee that is no symbol of the version (test or generated code) is not counted.
func callEdges(a *facts.Analysis) []callEdge {
	r := newReaches(a)
	var out []callEdge
	add := func(path string, line int, caller string, calls []string, at map[string]int, outside bool) {
		from := r.unitOf[path]
		for _, c := range calls {
			called := line
			if at[c] > 0 {
				called = at[c]
			}
			for _, t := range r.targets(c) {
				if from != "" && t.unit != from {
					e := callEdge{fromUnit: from, toUnit: t.unit, path: path, line: called, caller: caller, callee: t.callee, outside: outside}
					if t.callee != c {
						e.via = c
					}
					out = append(out, e)
				}
			}
		}
	}
	for _, s := range a.Symbols {
		add(s.Path, s.Line, s.ID, s.Calls, s.CallAt, false)
		add(s.Path, s.Line, s.ID, s.Through, s.CallAt, false)
	}
	for _, e := range a.Entries {
		if e.Symbol == "" {
			add(e.Path, e.Line, "", e.Calls, e.CallAt, false)
		}
	}
	for _, f := range a.Files {
		add(f.Path, 1, "", f.Calls, f.CallAt, true)
		add(f.Path, 1, "", f.Through, f.CallAt, true)
	}
	return out
}

// reaches knows where a call lands: the version's symbols, their units, and which types implement
// or extend which.
type reaches struct {
	unitOf   map[string]string // file path -> unit
	declared map[string]string // symbol ID -> unit
	kind     map[string]facts.SymbolKind
	subtypes map[string][]string // type ID -> the types naming it among their Supertypes
	supers   map[string][]string // type ID -> its Supertypes
}

func newReaches(a *facts.Analysis) reaches {
	r := reaches{unitOf: map[string]string{}, declared: map[string]string{}, kind: map[string]facts.SymbolKind{},
		subtypes: map[string][]string{}, supers: map[string][]string{}}
	for _, f := range a.Files {
		r.unitOf[f.Path] = f.Unit
	}
	for _, s := range a.Symbols {
		r.declared[s.ID], r.kind[s.ID] = r.unitOf[s.Path], s.Kind
		r.supers[s.ID] = s.Supertypes
		for _, t := range s.Supertypes {
			r.subtypes[t] = append(r.subtypes[t], s.ID)
		}
	}
	return r
}

// target is a unit a call lands in, and the function or method there.
type target struct{ unit, callee string }

// targets returns where a call of id lands. A function lands where it is declared. Method M of type
// T lands in every type implementing or extending T, at any depth, that declares M itself, and in
// the code a value of T itself runs: T's M, or else the nearest types T extends that declare M. A
// method no type of the repository declares (a library's, or code the compiler writes) lands
// nowhere, and neither does an interface's own method, which has no body.
func (r reaches) targets(id string) []target {
	i := strings.LastIndex(id, ".")
	owner := ""
	if i > 0 {
		owner = id[:i]
	}
	k, isType := r.kind[owner]
	isType = isType && (k == facts.SymbolType || k == facts.SymbolInterface)
	if !isType {
		if u, ok := r.declared[id]; ok {
			return []target{{u, id}}
		}
		return nil
	}
	method := id[i+1:]
	var out []target
	if k != facts.SymbolInterface {
		out = r.inherited(owner, method)
	}
	seen := map[string]bool{owner: true}
	queue := append([]string(nil), r.subtypes[owner]...)
	for len(queue) > 0 {
		t := queue[0]
		queue = queue[1:]
		if seen[t] {
			continue
		}
		seen[t] = true
		if u, ok := r.declared[t+"."+method]; ok {
			out = append(out, target{u, t + "." + method})
		}
		queue = append(queue, r.subtypes[t]...)
	}
	return out
}

// inherited returns where method of class t lands for a value of t itself: t's own method, or
// else, going up what t extends or implements, the first types on each path that declare it. A
// class declaring it wins over an interface: an interface's method runs only as a default, when no
// class above has one.
func (r reaches) inherited(t, method string) []target {
	var classes, ifaces []target
	seen := map[string]bool{}
	queue := []string{t}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if seen[c] {
			continue
		}
		seen[c] = true
		if u, ok := r.declared[c+"."+method]; ok {
			if r.kind[c] == facts.SymbolInterface {
				ifaces = append(ifaces, target{u, c + "." + method})
			} else {
				classes = append(classes, target{u, c + "." + method})
			}
			continue
		}
		queue = append(queue, r.supers[c]...)
	}
	if len(classes) > 0 {
		return classes
	}
	return ifaces
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
			Message: engine.T("call not in the call diagram: %s → %s (%s): %s", from, to, describeCall(d, a, b), calling(c)),
			Why:     engine.T("the call diagram (%s) tells which layers a request goes through; a call it does not draw can skip a layer or go backwards unseen", d.Path),
			Fix:     r.About().Fix,
		})
	}
	return out, nil
}

// calling says who calls what: the calling function, or entry-point code that is no symbol, and
// the callee, with the method the code names when the call lands on an implementation of it.
func calling(c callEdge) engine.Text {
	var who any = c.caller
	switch {
	case c.outside:
		who = engine.T("the code outside functions in %s", c.path)
	case c.caller == "":
		who = engine.T("the entry point")
	}
	if c.via != "" {
		return engine.T("%s calls %s through %s", who, c.callee, c.via)
	}
	return engine.T("%s calls %s", who, c.callee)
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

// Unfollowed sums the calls hadome cannot follow (Symbol.Unfollowed) by unit, over the units the
// call diagram draws, as the units' directories. Without a call diagram it counts nothing.
func Unfollowed(in engine.Input) map[string]int {
	d := in.Config.Calls
	if d == nil {
		return nil
	}
	unitOf := map[string]string{}
	for _, f := range in.Facts.Files {
		unitOf[f.Path] = f.Unit
	}
	unitDir := dirs(in.Facts)
	out := map[string]int{}
	for _, s := range in.Facts.Symbols {
		if dir := unitDir[unitOf[s.Path]]; s.Unfollowed > 0 && dir != "" && nodeOf(d, dir) != "" {
			out[dir] += s.Unfollowed
		}
	}
	return out
}
