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
	languages := unitLanguages(a, unitDir)
	for _, im := range a.Imports {
		if im.Internal {
			pairs = append(pairs, [2]string{unitDir[im.FromUnit], unitDir[im.ToUnit]})
		}
	}
	return draft(wording{"Dependencies", "`hadome suggest diagram` drew this draft from the imports as they are. " +
		"Tidy its boxes and arrows, then name this file in `rules.deps.diagram` of `.hadome.yml`.",
		"These boxes import each other. Decide which way is right and remove the imports going the other way:"}, languages, pairs, depth)
}

// DraftCalls is a call diagram drawn from the version's calls as they are, counted as
// deps/call-not-in-diagram counts them, in the form of DraftDiagram.
func DraftCalls(a *facts.Analysis, depth int) string {
	unitDir := dirs(a)
	var pairs [][2]string
	languages := unitLanguages(a, unitDir)
	for _, c := range callEdges(a) {
		pairs = append(pairs, [2]string{unitDir[c.fromUnit], unitDir[c.toUnit]})
	}
	return draft(wording{"Calls", "`hadome suggest calls` drew this draft from the calls as they are. " +
		"Tidy its boxes and arrows, then name this file in `rules.deps.calls` of `.hadome.yml`. " +
		"The box of the code that makes and wires everything (a DI container, `main`) is worth marking `:::wiring`, " +
		"which takes its calls out of the check.",
		"These boxes call each other. Decide which way is right and remove the calls going the other way:"}, languages, pairs, depth)
}

// wording is what a draft says besides its diagram: its heading, the note under it, and the line
// that introduces boxes with arrows both ways.
type wording struct {
	title, note, twoWay string
}

// draft writes the file: every unit's box, then an arrow for each pair of boxes some pair of units
// goes between. A pair with a unit of no directory is left out, as the rules leave it out. Boxes
// with arrows both ways are listed after the note, for a person to pick a direction.
func draft(w wording, languages map[string]string, pairs [][2]string, depth int) string {
	boxOf := boxes(languages, depth)
	labels := slices.Compact(slices.Sorted(maps.Values(boxOf)))
	ids := nodeIDs(labels)
	var arrows []string
	var boxPairs [][2]string
	drawn := map[[2]string]bool{}
	for _, p := range pairs {
		from, to := boxOf[p[0]], boxOf[p[1]]
		if from != "" && to != "" && from != to {
			arrows = append(arrows, fmt.Sprintf("  %s --> %s\n", ids[from], ids[to]))
			boxPairs = append(boxPairs, [2]string{from, to})
			drawn[[2]string{from, to}] = true
		}
	}
	slices.Sort(arrows)
	var both []string
	for _, p := range boxPairs {
		if p[0] < p[1] && drawn[[2]string{p[1], p[0]}] {
			both = append(both, fmt.Sprintf("- `%s` ⇄ `%s`\n", p[0], p[1]))
		}
	}
	both = slices.Compact(slices.Sorted(slices.Values(both)))
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n%s\n\n", w.title, w.note)
	if len(both) > 0 {
		fmt.Fprintf(&b, "%s\n\n%s\n", w.twoWay, strings.Join(both, ""))
	}
	b.WriteString("```mermaid\nflowchart LR\n")
	for _, l := range labels {
		fmt.Fprintf(&b, "  %s[\"%s\"]\n", ids[l], l)
	}
	b.WriteString(strings.Join(slices.Compact(arrows), ""))
	b.WriteString("```\n")
	return b.String()
}

// boxes maps each unit's directory to the label of its box. Levels are counted only at directories
// that branch, those with two or more child directories holding units, so that a long unbranched
// path (src/main/kotlin/com/example in Java and Kotlin) does not put everything in one box. A unit
// cut at depth levels shares the box of the directory it was cut at, which ends in "/..." when a unit
// lies below it; a unit that branches fewer times gets a box of its own directory. Each unit's box is
// the one covering it most specifically, as nodeOf picks.
func boxes(languages map[string]string, depth int) map[string]string {
	// language -> directory ("" for the root) -> its children holding units of the language
	children := map[string]map[string]map[string]struct{}{}
	for u, lang := range languages {
		if children[lang] == nil {
			children[lang] = map[string]map[string]struct{}{}
		}
		parent := ""
		for _, child := range prefixes(u) {
			if children[lang][parent] == nil {
				children[lang][parent] = map[string]struct{}{}
			}
			children[lang][parent][child] = struct{}{}
			parent = child
		}
	}
	group := func(dir string) (string, bool) {
		level, parent := 0, ""
		for _, child := range prefixes(dir) {
			if len(children[languages[dir]][parent]) >= 2 {
				level++
			}
			if level == depth {
				return child, true
			}
			parent = child
		}
		return dir, false
	}
	below := map[string]bool{}
	for u := range languages {
		if g, cut := group(u); cut && g != u {
			below[g] = true
		}
	}
	out := map[string]string{}
	for u := range languages {
		g, _ := group(u)
		if below[g] {
			g += "/..."
		}
		out[u] = g
	}
	return out
}

// unitLanguages maps the directory of each unit to its language: that of its units' files, the
// first by name when they differ (a directory can hold units of two languages). A unit of no
// directory is left out, as the rules leave it out.
func unitLanguages(a *facts.Analysis, unitDir map[string]string) map[string]string {
	out := map[string]string{}
	for _, f := range a.Files {
		d := unitDir[f.Unit]
		if d == "" {
			continue
		}
		if l, ok := out[d]; !ok || f.Language < l {
			out[d] = f.Language
		}
	}
	return out
}

// prefixes are the directories from the root down to dir, dir included: a/b/c gives a, a/b and
// a/b/c. The root itself (".") has none.
func prefixes(dir string) []string {
	if dir == "." {
		return nil
	}
	parts := strings.Split(dir, "/")
	out := make([]string, len(parts))
	for i := range parts {
		out[i] = strings.Join(parts[:i+1], "/")
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
