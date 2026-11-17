package deps

import (
	"slices"
	"strings"

	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
)

// cycle is deps/cycle.
type cycle struct{}

func (cycle) ID() string           { return "deps/cycle" }
func (cycle) Cases() []engine.Case { return cycleCases() }
func (cycle) About() engine.About {
	return engine.About{Finds: engine.T("an import of a pair of units base does not have that closes a cycle between units"),
		Why:  engine.T("units in a cycle can no longer be changed, tested or split apart one at a time; it is the plainest sign that the direction of dependencies broke"),
		Fix:  engine.T("decide which direction is right and remove the import going the other way, often by letting the inner unit declare an interface the outer one implements"),
		Keys: []string{"rules.deps.cycles.allow"}}
}

// importsAcrossParents are the languages in which an import between a directory and its ancestor
// does not count: a Rust crate compiles as one, so its root declaring modules that use crate:: back
// is how Rust is written, and a Java or Kotlin package and its subpackage are close to one.
var importsAcrossParents = map[string]bool{"rust": true, "java": true, "kotlin": true}

// edge is a counted import between two units' directories.
type edge struct {
	from, to string
	im       facts.Import
}

// FindAgainst reports head's imports of directory pairs base has no import of, when head's imports
// lead back from the imported directory to the importing one.
func (r cycle) FindAgainst(base, head engine.Input) ([]engine.Finding, error) {
	old := map[[2]string]bool{}
	for _, e := range counted(base.Facts, base.Config.CycleAllow) {
		old[[2]string{e.from, e.to}] = true
	}
	edges := counted(head.Facts, head.Config.CycleAllow)
	next := map[string][]string{}
	for _, e := range edges {
		next[e.from] = append(next[e.from], e.to)
	}
	for d, to := range next {
		next[d] = slices.Compact(slices.Sorted(slices.Values(to)))
	}
	reported := map[[2]string]bool{}
	var out []engine.Finding
	for _, e := range edges {
		p := [2]string{e.from, e.to}
		if old[p] || reported[p] {
			continue
		}
		back := shortestPath(next, e.to, e.from)
		if back == nil {
			continue
		}
		reported[p] = true
		way := strings.Join(append([]string{e.from}, back...), " → ")
		out = append(out, engine.Finding{
			Rule: r.ID(), File: e.im.Path, Line: e.im.Line, Anchor: e.from + " -> " + e.to,
			Message: engine.T("import closes a cycle: %s", way),
			Why:     r.About().Why,
			Fix:     r.About().Fix,
		})
	}
	return out, nil
}

// counted are the imports deps/cycle follows, between two different directories of units of
// product code, earliest by path and line first. Imports in test files, those between an allowed
// pair, and those between a directory and its ancestor in the languages of importsAcrossParents
// are left out. Type-only imports never reach the facts.
func counted(a *facts.Analysis, allow [][2]string) []edge {
	unitDir := dirs(a)
	files := map[string]facts.File{}
	for _, f := range a.Files {
		files[f.Path] = f
	}
	var out []edge
	for _, im := range a.Imports {
		f := files[im.Path]
		from, to := unitDir[im.FromUnit], unitDir[im.ToUnit]
		if !im.Internal || from == "" || to == "" || from == to || f.Role != facts.RoleSource && f.Role != facts.RoleGenerated ||
			allowed(allow, from, to) || importsAcrossParents[f.Language] && (ancestor(from, to) || ancestor(to, from)) {
			continue
		}
		out = append(out, edge{from, to, im})
	}
	slices.SortStableFunc(out, func(x, y edge) int {
		if c := strings.Compare(x.im.Path, y.im.Path); c != 0 {
			return c
		}
		return x.im.Line - y.im.Line
	})
	return out
}

// allowed reports whether one directory matches one glob of an allowed pair and the other the other.
func allowed(allow [][2]string, a, b string) bool {
	for _, p := range allow {
		in := func(glob, dir string) bool { return config.MatchesAny([]string{glob}, dir) }
		if in(p[0], a) && in(p[1], b) || in(p[0], b) && in(p[1], a) {
			return true
		}
	}
	return false
}

// ancestor reports whether directory a lies above directory b, another directory. The root (".")
// lies above every other directory.
func ancestor(a, b string) bool {
	return a == "." || strings.HasPrefix(b, a+"/")
}

// shortestPath returns the directories of a shortest way from one directory to another, both
// included, or nil when there is none. Among ways of the same length, the first by the names of
// its directories is taken, since next lists each directory's neighbours in order of their names.
func shortestPath(next map[string][]string, from, to string) []string {
	prev := map[string]string{from: ""}
	queue := []string{from}
	for len(queue) > 0 {
		d := queue[0]
		queue = queue[1:]
		if d == to {
			var way []string
			for ; d != ""; d = prev[d] {
				way = append(way, d)
			}
			slices.Reverse(way)
			return way
		}
		for _, n := range next[d] {
			if _, seen := prev[n]; !seen {
				prev[n] = d
				queue = append(queue, n)
			}
		}
	}
	return nil
}
