package deps_test

import (
	"slices"
	"testing"

	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
	"github.com/rerorets/hadome/internal/rules/deps"
)

// units is an analysis of units at the given directories, each with one source file, and the
// imports from, to, path and line.
func units(dirs []string, imports ...facts.Import) *facts.Analysis {
	a := &facts.Analysis{Imports: imports}
	for _, d := range dirs {
		a.Files = append(a.Files, facts.File{Path: d + "/f.go", Language: "go", Unit: "m/" + d, UnitPath: d, Role: facts.RoleSource})
	}
	return a
}

func imp(from, to, path string, line int) facts.Import {
	return facts.Import{Path: path, Line: line, FromUnit: "m/" + from, ToUnit: "m/" + to, Internal: true}
}

// viewpoint: external contract
// target: deps/cycle
// A finding lands on the earliest import of its pair, by path and line, and names the shortest way
// back, the first by name among ways as short. Pairs base has are never reported.
func TestACycleIsReportedAtTheEarliestImportWithTheShortestWayBack(t *testing.T) {
	var rule engine.BaseFindingRule
	for _, r := range deps.All() {
		if r.ID() == "deps/cycle" {
			rule = r.(engine.BaseFindingRule)
		}
	}
	dirs := []string{"a", "b", "c", "d", "e", "f", "g"}
	old := []facts.Import{imp("b", "a", "b/f.go", 1), imp("b", "c", "b/f.go", 2), imp("c", "a", "c/f.go", 1),
		imp("e", "g", "e/f.go", 1), imp("g", "d", "g/f.go", 1), imp("e", "f", "e/f.go", 2), imp("f", "d", "f/f.go", 1)}
	added := []facts.Import{imp("a", "b", "a/z.go", 1), imp("a", "b", "a/y.go", 7), imp("a", "b", "a/y.go", 3), imp("d", "e", "d/f.go", 1)}
	base := engine.Input{Facts: units(dirs, old...)}
	head := engine.Input{Facts: units(dirs, append(slices.Clone(old), added...)...)}
	for _, p := range []string{"a/y.go", "a/z.go"} {
		head.Facts.Files = append(head.Facts.Files, facts.File{Path: p, Language: "go", Unit: "m/a", UnitPath: "a", Role: facts.RoleSource})
	}
	got, err := rule.FindAgainst(base, head)
	if err != nil {
		t.Fatal(err)
	}
	type at struct {
		file    string
		line    int
		message string
	}
	var places []at
	for _, f := range got {
		places = append(places, at{f.File, f.Line, f.Message.In("en")})
	}
	want := []at{{"a/y.go", 3, "import closes a cycle: a → b → a"}, {"d/f.go", 1, "import closes a cycle: d → e → f → d"}}
	if !slices.Equal(places, want) {
		t.Errorf("want %v, got %v", want, places)
	}
}

// viewpoint: external contract
// target: deps/cycle
// The imports a cycle is followed through are those of product code, generated code included, and
// in Kotlin not those between a directory and one above it, the root included, while those between
// siblings count.
func TestACycleIsFollowedThroughProductCodeOnly(t *testing.T) {
	var rule engine.BaseFindingRule
	for _, r := range deps.All() {
		if r.ID() == "deps/cycle" {
			rule = r.(engine.BaseFindingRule)
		}
	}
	file := func(path, dir, lang string, role facts.Role) facts.File {
		return facts.File{Path: path, Language: lang, Unit: "m/" + dir, UnitPath: dir, Role: role}
	}
	a := &facts.Analysis{
		Files: []facts.File{file("a/f.go", "a", "go", facts.RoleSource), file("b/f.go", "b", "go", facts.RoleSource),
			file("b/f_test.go", "b", "go", facts.RoleTest), file("c/f.go", "c", "go", facts.RoleSource),
			file("c/gen.go", "c", "go", facts.RoleGenerated),
			file("App.kt", ".", "kotlin", facts.RoleSource), file("k/K.kt", "k", "kotlin", facts.RoleSource),
			file("j/J.kt", "j", "kotlin", facts.RoleSource)},
		Imports: []facts.Import{imp("a", "b", "a/f.go", 1), imp("b", "a", "b/f_test.go", 1),
			imp("a", "c", "a/f.go", 2), imp("c", "a", "c/gen.go", 1),
			imp(".", "k", "App.kt", 1), imp("k", ".", "k/K.kt", 1), imp("k", "j", "k/K.kt", 2), imp("j", "k", "j/J.kt", 1)},
	}
	got, err := rule.FindAgainst(engine.Input{Facts: &facts.Analysis{}}, engine.Input{Facts: a})
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, f := range got {
		files = append(files, f.File)
	}
	if want := []string{"a/f.go", "c/gen.go", "j/J.kt", "k/K.kt"}; !slices.Equal(files, want) {
		t.Errorf("want the cycles through generated code and between Kotlin siblings, reported at %v, got %v", want, files)
	}
}
