// Package suggest finds invariant candidates: places in the code that usually carry an invariant
// nobody has configured. It only informs; nothing here changes an exit code.
//
// Design: docs/design/suggest.md
package suggest

import (
	"path"
	"slices"
	"sort"
	"strings"
	"unicode"

	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
)

// byPrefix maps an invariant type to the name prefixes that suggest it, and why. A prefix listed in
// io suggests it only in a unit that does input or output: the word also names many functions that
// only compute a value (AddDays, Add(a, b)).
var byPrefix = []struct {
	typ, why string
	prefixes []string
	io       []string
	scoped   bool
}{
	{"idempotent", "creates things; a retried request must not create them twice",
		[]string{"Create", "Add", "Insert", "Register", "Submit", "Enqueue"}, []string{"Add", "Register"}, false},
	{"erasable", "deletes things; what is deleted must remain nowhere",
		[]string{"Delete", "Remove", "Erase", "Purge"}, nil, false},
	{"isolated", "reads data for a user or tenant; nobody else's data may come back",
		[]string{"Get", "List", "Find", "Search", "Read", "Load", "Fetch", "Query"}, nil, true},
}

// pairs are the operations whose round trip must give back the original.
var pairs = [][2]string{{"Export", "Import"}, {"Encode", "Decode"}, {"Marshal", "Unmarshal"},
	{"Serialize", "Deserialize"}, {"Save", "Load"}}

// Candidates returns the candidates of one version that its configuration does not already name.
func Candidates(in engine.Input) []engine.Candidate {
	syms := candidateSymbols(in.Facts)
	io := ioFiles(in.Facts)
	byID := map[string]facts.Symbol{}
	for _, s := range syms {
		byID[s.ID] = s
	}
	var out []engine.Candidate
	add := func(typ, why string, s facts.Symbol, ids ...string) {
		if configured(in.Config.Invariants, typ, ids) {
			return
		}
		targets := make([]string, len(ids))
		for i, id := range ids {
			targets[i] = target(id)
		}
		out = append(out, engine.Candidate{Type: typ, Targets: targets, File: s.Path, Line: s.Line, Why: why})
	}
	for _, s := range syms {
		prefix, name := split(s.ID)
		for _, p := range byPrefix {
			for _, w := range p.prefixes {
				if _, _, ok := begins(name, w); ok && (!p.scoped || s.Scoped) && (!slices.Contains(p.io, w) || io[s.Path]) {
					add(p.typ, p.why, s, s.ID)
					break
				}
			}
		}
		for _, pair := range pairs {
			if rest, spell, ok := begins(name, pair[0]); ok {
				if other, ok := byID[prefix+spell(pair[1])+rest]; ok {
					add("round-trip", "writes out and reads back; reading back must give the original", s, s.ID, other.ID)
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key() < out[j].Key() })
	return out
}

// configured reports whether an invariant of this type already names every one of the symbols.
func configured(invs []config.Invariant, typ string, ids []string) bool {
	for _, inv := range invs {
		if inv.Type != typ {
			continue
		}
		targets := inv.Targets
		if inv.Target != "" {
			targets = []string{inv.Target}
		}
		named := 0
		for _, id := range ids {
			for _, t := range targets {
				if config.TargetNames(t, id) {
					named++
					break
				}
			}
		}
		if named == len(ids) {
			return true
		}
	}
	return false
}

// candidateSymbols are the exported functions and methods, without UI components. Symbols never
// come from test or generated code.
func candidateSymbols(a *facts.Analysis) []facts.Symbol {
	var out []facts.Symbol
	for _, s := range a.Symbols {
		if s.Exported && (s.Kind == facts.SymbolFunction || s.Kind == facts.SymbolMethod) && !component(s) {
			out = append(out, s)
		}
	}
	return out
}

// component reports a React component: in .tsx and .jsx files, a function whose name starts with an
// upper-case letter is one. It is a part of a screen, not an operation (DeleteRowDialog).
func component(s facts.Symbol) bool {
	ext := path.Ext(s.Path)
	_, name := split(s.ID)
	return (ext == ".tsx" || ext == ".jsx") && s.Kind == facts.SymbolFunction && name != "" && unicode.IsUpper([]rune(name)[0])
}

// ioFiles are the files of units that import something that does input or output.
func ioFiles(a *facts.Analysis) map[string]bool {
	units := map[string]bool{}
	for _, im := range a.Imports {
		if im.IO {
			units[im.FromUnit] = true
		}
	}
	out := map[string]bool{}
	for _, f := range a.Files {
		out[f.Path] = f.Unit != "" && units[f.Unit]
	}
	return out
}

// split cuts a symbol ID into everything up to the last name and the name: "example.com/app/store.Store."
// and "Delete" for "example.com/app/store.Store.Delete".
func split(id string) (string, string) {
	i := strings.LastIndex(id, ".")
	return id[:i+1], id[i+1:]
}

// target writes a symbol ID the way configuration names it: the unit's last path element, then the name.
func target(id string) string {
	return facts.ShortName(id)
}

// begins reports whether name begins with word, a PascalCase word such as Delete, in any of three
// spellings: DeleteRow, deleteRow or delete_row. The word must be the whole name or be followed by
// what starts the next word in that spelling, so that Address is not an Add. It returns the rest of
// the name and how to spell another word the same way.
func begins(name, word string) (string, func(string) string, bool) {
	same := func(w string) string { return w }
	if rest, ok := strings.CutPrefix(name, word); ok && (rest == "" || startsUpper(rest)) {
		return rest, same, true
	}
	if rest, ok := strings.CutPrefix(name, lowerFirst(word)); ok && (rest == "" || startsUpper(rest) || strings.HasPrefix(rest, "_")) {
		return rest, lowerFirst, true
	}
	return "", nil, false
}

func lowerFirst(w string) string {
	r := []rune(w)
	r[0] = unicode.ToLower(r[0])
	return string(r)
}

func startsUpper(s string) bool {
	r := []rune(s)[0]
	return unicode.IsUpper(r) || unicode.IsDigit(r)
}
