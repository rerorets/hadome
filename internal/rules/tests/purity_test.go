package tests

import (
	"fmt"
	"testing"

	"github.com/rerorets/hadome/internal/facts"
)

// cycle is three units that import each other in a ring, first → second → third → first, with
// the unit at io also importing a package that performs input or output. Files and imports are
// listed starting at shift, so each call builds the same graph in a different order.
func cycle(names [3]string, io, shift int) *facts.Analysis {
	a := &facts.Analysis{}
	for i := range names {
		k := (i + shift) % 3
		a.Files = append(a.Files, facts.File{Path: names[k] + "/x.ts", Role: facts.RoleSource, Unit: names[k]})
		a.Imports = append(a.Imports, facts.Import{Path: names[k] + "/x.ts", Line: 1, FromUnit: names[k],
			ToUnit: names[(k+1)%3], Internal: true})
		if k == io {
			a.Imports = append(a.Imports, facts.Import{Path: names[k] + "/x.ts", Line: 2, FromUnit: names[k],
				ToUnit: "node:fs", IO: true})
		}
	}
	return a
}

// viewpoint: invariant
// target: tests.pureUnits
// Units in an import cycle are pure or not together, whichever unit of the cycle does the input or
// output, whatever order the units are listed in, and however often it is asked: a unit that
// imports one that is not pure is not pure, and in a cycle every unit imports every other one.
func TestEveryUnitOfACycleWithInputOrOutputIsImpure(t *testing.T) {
	orders := [][3]string{{"src/a", "src/b", "src/c"}, {"src/c", "src/a", "src/b"}, {"src/b", "src/c", "src/a"}}
	for _, names := range orders {
		for io := range 3 {
			for shift := range 3 {
				for run := range 20 {
					if pure := pureUnits(cycle(names, io, shift)); len(pure) != 0 {
						t.Fatalf("names %v, input or output in %s, shift %d, run %d: want no pure unit, got %v",
							names, names[io], shift, run, pure)
					}
				}
			}
		}
	}
}

// viewpoint: invariant
// target: tests.pureUnits
// A cycle that reaches nothing outside is pure: the cycle itself adds no input or output.
func TestACycleWithoutInputOrOutputIsPure(t *testing.T) {
	a := cycle([3]string{"src/a", "src/b", "src/c"}, -1, 0)
	pure := pureUnits(a)
	for _, u := range []string{"src/a", "src/b", "src/c"} {
		if !pure[u] {
			t.Errorf("%s: want pure, got %v", u, pure)
		}
	}
}

// viewpoint: invariant
// target: tests.pureUnits
// A unit outside a cycle that imports a unit of an impure cycle is impure; one that the cycle
// imports and that reaches nothing stays pure.
func TestUnitsAroundACycleFollowTheirImports(t *testing.T) {
	for shift := range 3 {
		a := cycle([3]string{"src/a", "src/b", "src/c"}, 1, shift)
		for _, u := range []string{"src/ui", "src/calc"} {
			a.Files = append(a.Files, facts.File{Path: u + "/x.ts", Role: facts.RoleSource, Unit: u})
		}
		a.Imports = append(a.Imports,
			facts.Import{Path: "src/ui/x.ts", Line: 1, FromUnit: "src/ui", ToUnit: "src/c", Internal: true},
			facts.Import{Path: "src/a/x.ts", Line: 3, FromUnit: "src/a", ToUnit: "src/calc", Internal: true})
		got := fmt.Sprint(pureUnits(a))
		if want := fmt.Sprint(map[string]bool{"src/calc": true}); got != want {
			t.Errorf("shift %d: want %s pure, got %s", shift, want, got)
		}
	}
}
