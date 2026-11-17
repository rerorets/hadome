package deps

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/rerorets/hadome/internal/facts"
)

// DraftDiagram is a dependency diagram drawn from the version's imports as they are, as the
// contents of a Markdown file: configured, it gives the version no deps/ finding. Units are grouped
// into boxes at depth levels from the root (box).
func DraftDiagram(a *facts.Analysis, depth int) string {
	unitDir := dirs(a)
	var pairs [][2]string
	for _, im := range a.Imports {
		if im.Internal {
			pairs = append(pairs, [2]string{unitDir[im.FromUnit], unitDir[im.ToUnit]})
		}
	}
	return draft("Dependencies", "`hadome suggest diagram` drew this draft from the imports as they are. "+
		"Tidy its boxes and arrows, then name this file in `rules.deps.diagram` of `.hadome.yml`.", unitDir, pairs, depth)
}

// DraftCalls is a call diagram drawn from the version's calls as they are, counted as
// deps/call-not-in-diagram counts them, in the form of DraftDiagram.
func DraftCalls(a *facts.Analysis, depth int) string {
	unitDir := dirs(a)
	var pairs [][2]string
	for _, c := range callEdges(a) {
		pairs = append(pairs, [2]string{unitDir[c.fromUnit], unitDir[c.toUnit]})
	}
	return draft("Calls", "`hadome suggest calls` drew this draft from the calls as they are. "+
		"Tidy its boxes and arrows, then name this file in `rules.deps.calls` of `.hadome.yml`. "+
		"The box of the code that makes and wires everything (a DI container, `main`) is worth marking `:::wiring`, "+
		"which takes its calls out of the check.", unitDir, pairs, depth)
}

// draft writes the file: every unit's box, then an arrow for each pair of boxes some pair of units
// goes between. A pair with a unit of no directory is left out, as the rules leave it out.
func draft(title, note string, unitDir map[string]string, pairs [][2]string, depth int) string {
	var units []string
	for _, d := range unitDir {
		if d != "" {
			units = append(units, d)
		}
	}
	boxOf := boxes(units, depth)
	labels := slices.Compact(slices.Sorted(maps.Values(boxOf)))
	ids := nodeIDs(labels)
	var arrows []string
	for _, p := range pairs {
		from, to := boxOf[p[0]], boxOf[p[1]]
		if from != "" && to != "" && from != to {
			arrows = append(arrows, fmt.Sprintf("  %s --> %s\n", ids[from], ids[to]))
		}
	}
	slices.Sort(arrows)
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n%s\n\n```mermaid\nflowchart LR\n", title, note)
	for _, l := range labels {
		fmt.Fprintf(&b, "  %s[\"%s\"]\n", ids[l], l)
	}
	b.WriteString(strings.Join(slices.Compact(arrows), ""))
	b.WriteString("```\n")
	return b.String()
}

// boxes maps each unit's directory to the label of its box: the directory's first depth levels,
// ending in "/..." when a unit lies below them, so that one box takes in a directory and everything
// under it. A unit no deeper than depth gets a box of its own directory. Each unit's box is the one
// covering it most specifically, as nodeOf picks.
func boxes(units []string, depth int) map[string]string {
	group := func(dir string) string {
		parts := strings.Split(dir, "/")
		if len(parts) > depth {
			parts = parts[:depth]
		}
		return strings.Join(parts, "/")
	}
	below := map[string]bool{}
	for _, u := range units {
		if g := group(u); g != u {
			below[g] = true
		}
	}
	out := map[string]string{}
	for _, u := range units {
		g := group(u)
		if below[g] && g != "." {
			g += "/..."
		}
		out[u] = g
	}
	return out
}

// nodeIDs gives each label, in order, a Mermaid node ID: its letters and digits, the rest made _, with a number
// added when two labels come out the same. The root's box is root. An ID Mermaid reads otherwise
// (end, or one starting with a digit) gets a prefix.
func nodeIDs(labels []string) map[string]string {
	out, taken := map[string]string{}, map[string]bool{}
	for _, l := range labels {
		id := strings.Trim(strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
				return r
			}
			return '_'
		}, strings.TrimSuffix(l, "/...")), "_")
		if l == "." {
			id = "root"
		}
		if id == "" || strings.EqualFold(id, "end") || id[0] >= '0' && id[0] <= '9' {
			id = "u_" + id
		}
		base := id
		for n := 2; taken[id]; n++ {
			id = fmt.Sprintf("%s%d", base, n)
		}
		taken[id] = true
		out[l] = id
	}
	return out
}
