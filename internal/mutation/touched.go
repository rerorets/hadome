package mutation

import (
	"io/fs"
	"sort"
	"strings"

	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
)

// touched returns the functions of head to break, sorted by ID: those whose code the change changed
// (new ones included), and those that a test the change wrote or rewrote calls, in a file that can
// be built with the test's. The second catches a change that weakens only a test.
//
// A version may hold several functions with one ID (the same name in files split by build tags,
// a package's init functions), so a function is matched by its ID and its body: it is unchanged
// while base has a function of that ID with the same body not yet matched to another.
func touched(base, head *facts.Analysis, bv, hv engine.Version) []facts.Symbol {
	baseText := map[string]map[string]int{} // ID -> body -> how many base functions have it
	for _, s := range functions(base) {
		if baseText[s.ID] == nil {
			baseText[s.ID] = map[string]int{}
		}
		baseText[s.ID][bodyText(bv, s)]++
	}
	baseBody := map[string]string{}
	for _, t := range base.Tests {
		baseBody[t.Path+"\x00"+t.Name] = t.Body
	}
	build := map[string]string{}
	for _, f := range head.Files {
		build[f.Path] = f.Build
	}
	callers := map[string][]string{} // function ID -> the files of the rewritten tests calling it
	for _, t := range head.Tests {
		if b, ok := baseBody[t.Path+"\x00"+t.Name]; ok && b == t.Body && t.Body != "" {
			continue
		}
		for _, c := range t.Calls {
			callers[c.Target] = append(callers[c.Target], t.Path)
		}
	}
	// A function of a file no calling test is ever built with is not one the test can guard.
	called := func(s facts.Symbol) bool {
		for _, p := range callers[s.ID] {
			if facts.BuildTogether(build[p], build[s.Path]) {
				return true
			}
		}
		return false
	}
	var out []facts.Symbol
	for _, s := range functions(head) {
		body := bodyText(hv, s)
		unchanged := baseText[s.ID][body] > 0
		if unchanged {
			baseText[s.ID][body]--
		}
		if !unchanged || called(s) {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// functions are the functions and methods of a version's product code.
func functions(a *facts.Analysis) []facts.Symbol {
	source := map[string]bool{}
	for _, f := range a.Files {
		source[f.Path] = f.Role == facts.RoleSource
	}
	var out []facts.Symbol
	for _, s := range a.Symbols {
		if source[s.Path] && (s.Kind == facts.SymbolFunction || s.Kind == facts.SymbolMethod) {
			out = append(out, s)
		}
	}
	return out
}

// bodyText is a function's text with whitespace collapsed, so that reindenting it is no change.
func bodyText(v engine.Version, s facts.Symbol) string {
	src, err := fs.ReadFile(v.FS, s.Path)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(src), "\n")
	if s.Line < 1 || s.EndLine > len(lines) || s.EndLine < s.Line {
		return ""
	}
	return strings.Join(strings.Fields(strings.Join(lines[s.Line-1:s.EndLine], "\n")), " ")
}
