package mutation

import (
	"io/fs"
	"sort"
	"strings"

	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
)

// touched returns the functions of head to break, sorted by ID: those whose code the change changed
// (new ones included), and those that a test the change wrote or rewrote calls. The second catches
// a change that weakens only a test.
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
	called := map[string]bool{}
	for _, t := range head.Tests {
		if b, ok := baseBody[t.Path+"\x00"+t.Name]; ok && b == t.Body && t.Body != "" {
			continue
		}
		for _, c := range t.Calls {
			called[c.Target] = true
		}
	}
	var out []facts.Symbol
	for _, s := range functions(head) {
		body := bodyText(hv, s)
		unchanged := baseText[s.ID][body] > 0
		if unchanged {
			baseText[s.ID][body]--
		}
		if !unchanged || called[s.ID] {
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
