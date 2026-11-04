package tests

import (
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
)

// pureUnits returns the units of the repository's non-test code that are pure: they import nothing
// that performs input or output, and only pure units of the repository. A unit imported but not
// found among the files is unknown, and so is a unit with an import whose destination is unknown;
// unknown is never pure.
func pureUnits(a *facts.Analysis) map[string]bool {
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
	state := map[string]bool{}
	visiting := map[string]bool{}
	var pure func(u string) bool
	pure = func(u string) bool {
		if v, ok := state[u]; ok {
			return v
		}
		if !units[u] {
			return false
		}
		if visiting[u] {
			return true // an import cycle adds nothing; the other imports of the cycle decide
		}
		visiting[u] = true
		ok := true
		for _, im := range deps[u] {
			if reachesOut(im, pure) {
				ok = false
				break
			}
		}
		delete(visiting, u)
		state[u] = ok
		return ok
	}
	out := map[string]bool{}
	for u := range units {
		if pure(u) {
			out[u] = true
		}
	}
	return out
}

// reachesOut reports an import that makes its unit impure: one that performs input or output,
// one whose destination is unknown, or one of a unit of the repository that is not pure.
func reachesOut(im facts.Import, pure func(string) bool) bool {
	return im.IO || im.Unknown || (im.Internal && !pure(im.ToUnit))
}

// unitOf returns the unit holding the symbol with this ID, or "" when there is none.
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
		message: "test of pure code uses a mock",
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
	return engine.About{Finds: "a unit that was pure in base and reaches input or output in head, through its imports",
		Why:  "side effects entered code that used to take values and return values; sometimes intended, often a design slip",
		Fix:  "if intended, a person approves it; otherwise move the side effect to a unit that is not pure and pass its results in",
		Keys: []string{"rules.tests.io"}, Review: true}
}

func (r purityLost) Measure(in engine.Input) ([]engine.Item, error) {
	var out []engine.Item
	for u := range pureUnits(in.Facts) {
		out = append(out, engine.Item{
			Rule: r.ID(), Key: u, File: fileOf(in.Facts, u), Review: true,
			Detail: u + " is no longer pure: it now reaches input or output through its imports",
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

// fileOf returns a source file of the unit, so the review points somewhere in the tree.
func fileOf(a *facts.Analysis, unit string) string {
	for _, f := range a.Files {
		if f.Unit == unit && f.Role == facts.RoleSource {
			return f.Path
		}
	}
	return ""
}
