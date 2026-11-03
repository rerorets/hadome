// Package suggest finds invariant candidates: places in the code that usually carry an invariant
// nobody has configured. It only informs; nothing here changes an exit code.
//
// Design: docs/design/suggest.md
package suggest

import (
	"sort"
	"strings"
	"unicode"

	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
)

// byPrefix maps an invariant type to the name prefixes that suggest it, and why.
var byPrefix = []struct {
	typ, why string
	prefixes []string
	scoped   bool
}{
	{"idempotent", "creates things; a retried request must not create them twice",
		[]string{"Create", "Add", "Insert", "Register", "Submit", "Enqueue"}, false},
	{"erasable", "deletes things; what is deleted must remain nowhere",
		[]string{"Delete", "Remove", "Erase", "Purge"}, false},
	{"isolated", "reads data for a user or tenant; nobody else's data may come back",
		[]string{"Get", "List", "Find", "Search", "Read", "Load", "Fetch", "Query"}, true},
}

// pairs are the operations whose round trip must give back the original.
var pairs = [][2]string{{"Export", "Import"}, {"Encode", "Decode"}, {"Marshal", "Unmarshal"},
	{"Serialize", "Deserialize"}, {"Save", "Load"}}

// Candidates returns the candidates of one version that its configuration does not already name.
func Candidates(in engine.Input) []engine.Candidate {
	syms := candidateSymbols(in.Facts)
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
			if (!p.scoped || s.Scoped) && beginsWith(name, p.prefixes) {
				add(p.typ, p.why, s, s.ID)
			}
		}
		for _, pair := range pairs {
			if rest, ok := strings.CutPrefix(name, pair[0]); ok && (rest == "" || startsUpper(rest)) {
				if other, ok := byID[prefix+pair[1]+rest]; ok {
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

// candidateSymbols are the exported functions and methods. Symbols never come from test or generated code.
func candidateSymbols(a *facts.Analysis) []facts.Symbol {
	var out []facts.Symbol
	for _, s := range a.Symbols {
		if s.Exported && (s.Kind == facts.SymbolFunction || s.Kind == facts.SymbolMethod) {
			out = append(out, s)
		}
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
	return id[strings.LastIndex(id, "/")+1:]
}

// beginsWith reports whether one of the words is the whole name or is followed by an upper-case
// letter or digit, so that Address is not an Add.
func beginsWith(name string, words []string) bool {
	for _, w := range words {
		if rest, ok := strings.CutPrefix(name, w); ok && (rest == "" || startsUpper(rest)) {
			return true
		}
	}
	return false
}

func startsUpper(s string) bool {
	r := []rune(s)[0]
	return unicode.IsUpper(r) || unicode.IsDigit(r)
}
