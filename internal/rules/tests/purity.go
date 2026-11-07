package tests

import (
	"cmp"
	"sort"

	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
)

// pureUnits returns the units of the repository's non-test code that are pure: they import nothing
// that performs input or output, and only pure units of the repository. A unit imported but not
// found among the files is unknown, and so is a unit with an import whose destination is unknown;
// unknown is never pure.
func pureUnits(a *facts.Analysis) map[string]bool {
	units, impure := purity(a)
	out := map[string]bool{}
	for u := range units {
		if _, ok := impure[u]; !ok {
			out[u] = true
		}
	}
	return out
}

// impurity is why a unit is not pure. Cause is the import that reaches input or output, has an
// unknown destination or names a unit hadome did not read, and At is the unit whose own import it is.
// Import is the unit's own import that leads there: Cause itself when At is the unit, otherwise an
// import of a unit of the repository.
type impurity struct {
	At     string
	Import facts.Import
	Cause  facts.Import
}

// purity returns the units of the repository's non-test code, and why each one that is not pure is
// not. Units that import each other in a cycle are decided together, as one: the cycle is pure only
// when none of its units reaches out. Deciding them one at a time would make the answer depend on
// which unit of the cycle is looked at first.
func purity(a *facts.Analysis) (map[string]bool, map[string]impurity) {
	units := map[string]bool{}
	for _, f := range a.Files {
		if f.Unit != "" && f.Role != facts.RoleTest && f.Role != facts.RoleOther {
			units[f.Unit] = true
		}
	}
	deps := map[string][]facts.Import{}
	for _, im := range a.Imports {
		deps[im.FromUnit] = append(deps[im.FromUnit], im)
	}
	for _, ims := range deps {
		sort.SliceStable(ims, func(i, j int) bool {
			if ims[i].Path != ims[j].Path {
				return ims[i].Path < ims[j].Path
			}
			if ims[i].Line != ims[j].Line {
				return ims[i].Line < ims[j].Line
			}
			return ims[i].ToUnit < ims[j].ToUnit
		})
	}
	impure := map[string]impurity{}
	for _, cycle := range components(units, deps) {
		decide(cycle, units, deps, impure)
	}
	return units, impure
}

// decide records why the units of one cycle (or a single unit) are not pure, if they are not. The
// units they import outside the cycle are already decided.
func decide(cycle []string, units map[string]bool, deps map[string][]facts.Import, impure map[string]impurity) {
	in := map[string]bool{}
	for _, u := range cycle {
		in[u] = true
	}
	own := map[string]impurity{}
	first := ""
	for _, u := range cycle {
		for _, im := range deps[u] {
			if why, ok := reachesOut(u, im, in, units, impure); ok {
				own[u] = why
				first = cmp.Or(first, u)
				break
			}
		}
	}
	if first == "" {
		return
	}
	for _, u := range cycle {
		why, ok := own[u]
		if !ok {
			why = own[first]
			why.Import = inCycle(deps[u], u, in, why.Import)
		}
		impure[u] = why
	}
}

// reachesOut says whether an import of unit u, a unit of the cycle in, makes u impure, and why: it
// performs input or output, its destination is unknown or not read, or it imports a unit outside the
// cycle that is not pure.
func reachesOut(u string, im facts.Import, in, units map[string]bool, impure map[string]impurity) (impurity, bool) {
	if im.IO || im.Unknown || (im.Internal && !units[im.ToUnit]) {
		return impurity{At: u, Import: im, Cause: im}, true
	}
	if w, ok := impure[im.ToUnit]; ok && im.Internal && !in[im.ToUnit] {
		return impurity{At: w.At, Import: im, Cause: w.Cause}, true
	}
	return impurity{}, false
}

// inCycle returns u's first import of another unit of the cycle, or otherwise when it has none.
func inCycle(ims []facts.Import, u string, in map[string]bool, otherwise facts.Import) facts.Import {
	for _, im := range ims {
		if im.Internal && in[im.ToUnit] && im.ToUnit != u {
			return im
		}
	}
	return otherwise
}

// components returns the strongly connected components of the import graph between the units
// (Tarjan's algorithm): units that import each other, directly or not, are in one component. A
// component comes after every component it imports. Units and the units of a component are in
// sorted order, so the result is the same however the maps were built.
func components(units map[string]bool, deps map[string][]facts.Import) [][]string {
	var order []string
	for u := range units {
		order = append(order, u)
	}
	sort.Strings(order)
	index, low, onStack := map[string]int{}, map[string]int{}, map[string]bool{}
	var stack []string
	var out [][]string
	var visit func(u string)
	visit = func(u string) {
		index[u], low[u] = len(index), len(index)
		stack = append(stack, u)
		onStack[u] = true
		for _, im := range deps[u] {
			v := im.ToUnit
			if !im.Internal || !units[v] {
				continue
			}
			if _, seen := index[v]; !seen {
				visit(v)
				low[u] = min(low[u], low[v])
			} else if onStack[v] {
				low[u] = min(low[u], index[v])
			}
		}
		if low[u] != index[u] {
			return
		}
		var c []string
		for {
			v := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			onStack[v] = false
			c = append(c, v)
			if v == u {
				break
			}
		}
		sort.Strings(c)
		out = append(out, c)
	}
	for _, u := range order {
		if _, seen := index[u]; !seen {
			visit(u)
		}
	}
	return out
}

// unitOf returns the unit holding the symbol with this ID, the unit itself when the ID names one, or
// "" when there is neither.
func unitOf(a *facts.Analysis, id string) string {
	path := ""
	for _, s := range a.Symbols {
		if s.ID == id {
			path = s.Path
			break
		}
	}
	for _, f := range a.Files {
		if path != "" && f.Path == path {
			return f.Unit
		}
	}
	if path == "" && a.HasTarget(id) {
		return id
	}
	return ""
}

// testsPureUnitsOnly reports whether every target of a test is in a pure unit. A test that also
// calls code that is not pure may need its mock for that code.
func testsPureUnitsOnly(in engine.Input) func(facts.Test) bool {
	pure := pureUnits(in.Facts)
	return func(t facts.Test) bool {
		for _, c := range t.Calls {
			if !pure[unitOf(in.Facts, c.Target)] {
				return false
			}
		}
		return len(t.Calls) > 0
	}
}

// NoMock is tests/no-mock.
func NoMock() engine.Rule {
	return hazardRule{
		id: "tests/no-mock", category: facts.HazardMock, applies: testsPureUnitsOnly,
		message: "test of pure code uses a mock: %s",
		finds:   "a mock in a test whose targets are all in pure units (units that touch no input or output)",
		keys:    []string{"rules.tests.hazards.mock", "rules.tests.io"},
		why:     "the code under test touches nothing outside; needing a mock means side effects leaked into it, and the mock hides that",
		fix:     "move the side effect out of the unit, and test the unit with plain values",
		cases:   noMockCases(),
	}
}

// purityLost is tests/purity-lost: a unit that was pure in base and is not in head. A person decides
// whether the side effect belongs there, so it needs review instead of failing.
type purityLost struct{}

func (purityLost) ID() string           { return "tests/purity-lost" }
func (purityLost) Cases() []engine.Case { return purityCases() }
func (purityLost) About() engine.About {
	return engine.About{Finds: engine.T("a unit that was pure in base and reaches input or output in head, through its imports or the global APIs it uses"),
		Why:  engine.T("side effects entered code that used to take values and return values; sometimes intended, often a design slip"),
		Fix:  engine.T("if intended, a person approves it; otherwise move the side effect to a unit that is not pure and pass its results in"),
		Keys: []string{"rules.tests.io"}, Review: true}
}

func (r purityLost) Measure(in engine.Input) ([]engine.Item, error) {
	var out []engine.Item
	for u := range pureUnits(in.Facts) {
		out = append(out, engine.Item{
			Rule: r.ID(), Key: u, File: fileOf(in.Facts, u), Review: true,
			Detail: engine.T("%s is no longer pure: it now reaches input or output through its imports", u),
			Why:    r.About().Why,
			Fix:    r.About().Fix,
		})
	}
	return out, nil
}

// RemovedLegitimately is true when the unit no longer exists in head.
func (purityLost) RemovedLegitimately(it engine.Item, head engine.Input) (bool, error) {
	for _, f := range head.Facts.Files {
		if f.Unit == it.Key && f.Role != facts.RoleTest {
			return false, nil
		}
	}
	return true, nil
}

// Lost reports the unit at its own import that makes it impure in head, and says what that import
// reaches: input or output, a global API, an unknown destination or a unit hadome did not read.
func (purityLost) Lost(it engine.Item, head engine.Input) engine.Item {
	_, impure := purity(head.Facts)
	why, ok := impure[it.Key]
	if !ok {
		return it
	}
	it.File, it.Line = why.Import.Path, why.Import.Line
	reason := reasonOf(why.Cause)
	switch {
	case why.At == it.Key:
		it.Detail = engine.T("%s is no longer pure: it %s", it.Key, reason)
	case why.At == why.Import.ToUnit:
		it.Detail = engine.T("%s is no longer pure: it imports %s, which %s", it.Key, why.At, reason)
	default:
		it.Detail = engine.T("%s is no longer pure: it imports %s, and further on %s %s", it.Key, why.Import.ToUnit, why.At, reason)
	}
	return it
}

// reasonOf says what an import that makes its unit impure does, as a predicate of that unit.
func reasonOf(im facts.Import) engine.Text {
	switch {
	case im.Global:
		return engine.T("uses the global %s, which performs input or output", im.ToUnit)
	case im.Unknown:
		return engine.T("has an import whose destination is decided only at run time: %s", im.Text)
	case im.IO:
		return engine.T("imports %s, which performs input or output", im.ToUnit)
	}
	return engine.T("imports %s, which hadome did not find among the repository's source files", im.ToUnit)
}

// fileOf returns a source file of the unit, so the review points somewhere in the tree.
func fileOf(a *facts.Analysis, unit string) string {
	for _, f := range a.Files {
		if f.Unit == unit && f.Role == facts.RoleSource {
			return f.Path
		}
	}
	return ""
}
