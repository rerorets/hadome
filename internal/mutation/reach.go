package mutation

import (
	"slices"
	"sort"

	"github.com/rerorets/hadome/internal/facts"
)

// reach finds the tests that reach a function: those that call a function of a unit from which the
// function's unit can be reached by the product code's imports, and those in such a unit. It reads
// the version's facts only (docs/design/mutation.md, "The callers' stage").
type reach struct {
	unitOf    map[string]string   // function ID -> its unit
	fileUnit  map[string]string   // file -> its unit
	importers map[string][]string // unit -> the units whose product code imports it
	tests     []facts.Test        // the tests that are not skipped
	byUnit    map[string][]facts.Test
}

// reaching reads what reach needs from a version's facts.
func reaching(a *facts.Analysis) *reach {
	r := &reach{unitOf: map[string]string{}, fileUnit: map[string]string{}, importers: map[string][]string{},
		byUnit: map[string][]facts.Test{}}
	for _, f := range a.Files {
		r.fileUnit[f.Path] = f.Unit
	}
	for _, s := range a.Symbols {
		r.unitOf[s.ID] = r.fileUnit[s.Path]
	}
	for _, im := range a.Imports {
		// Only imports of the repository's own units lead anywhere; an import decided at run time
		// (Unknown) names no unit.
		if im.Internal && im.FromUnit != im.ToUnit && !slices.Contains(r.importers[im.ToUnit], im.FromUnit) {
			r.importers[im.ToUnit] = append(r.importers[im.ToUnit], im.FromUnit)
		}
	}
	for _, t := range a.Tests {
		if !t.Skip.Skipped {
			r.tests = append(r.tests, t)
		}
	}
	return r
}

// of returns the tests that reach the function, sorted by name then path. They depend only on the
// function's unit, so every function of a unit gets the same tests.
func (r *reach) of(fn string) []facts.Test {
	unit, ok := r.unitOf[fn]
	if !ok {
		return nil
	}
	if out, ok := r.byUnit[unit]; ok {
		return out
	}
	units := map[string]bool{unit: true}
	for next := []string{unit}; len(next) > 0; {
		var more []string
		for _, u := range next {
			for _, from := range r.importers[u] {
				if !units[from] {
					units[from] = true
					more = append(more, from)
				}
			}
		}
		next = more
	}
	var out []facts.Test
	for _, t := range r.tests {
		if units[r.fileUnit[t.Path]] || slices.ContainsFunc(t.Calls, func(c facts.Call) bool {
			u, ok := r.unitOf[c.Target]
			return ok && units[u]
		}) {
			out = append(out, t)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Path < out[j].Path
	})
	r.byUnit[unit] = out
	return out
}
