//go:build cgo

package treesitter

import (
	"maps"
	"slices"
	"sort"

	"github.com/rerorets/hadome/internal/facts"
)

// supers collects the supertypes each type of product code names, as the IDs the type's file
// resolves them to, for Symbol.Supertypes.
type supers map[string][]string

// add records the supertypes type id names, written as types (List<T>, a.b.Store), resolved by what
// the written type stands for in its file.
func (s supers) add(id string, written []string, resolve func(written string) tbind) {
	for _, w := range written {
		if b := resolve(w); b.id != "" {
			s[id] = append(s[id], b.id)
		}
	}
}

// fill sets Symbol.Supertypes, keeping the repository's types only, sorted and without repeats.
func (s supers) fill(a *facts.Analysis) {
	isType := map[string]bool{}
	for _, sym := range a.Symbols {
		isType[sym.ID] = sym.Kind == facts.SymbolType || sym.Kind == facts.SymbolInterface
	}
	for i := range a.Symbols {
		sym := &a.Symbols[i]
		seen := map[string]bool{}
		for _, t := range s[sym.ID] {
			if isType[t] && t != sym.ID && !seen[t] {
				seen[t] = true
				sym.Supertypes = append(sym.Supertypes, t)
			}
		}
		sort.Strings(sym.Supertypes)
	}
}

// callLines maps the ID of each method called to the line of its first call.
type callLines map[string]int

// add records a call of id at line at, keeping the earlier line.
func (c callLines) add(id string, at int) {
	if old, ok := c[id]; !ok || at < old {
		c[id] = at
	}
}

// setThrough sets Symbol.Through of the functions and methods to the calls through a value's
// declared type that are not already among their calls, with their lines.
func setThrough(a *facts.Analysis, through map[string]callLines) {
	for i := range a.Symbols {
		addThrough(&a.Symbols[i], through[a.Symbols[i].ID])
	}
}

// addThrough adds to a symbol the calls of through that are not among its calls.
func addThrough(s *facts.Symbol, through callLines) {
	for _, c := range slices.Sorted(maps.Keys(through)) {
		if slices.Contains(s.Calls, c) {
			continue
		}
		s.Through = append(s.Through, c)
		if s.CallAt == nil {
			s.CallAt = map[string]int{}
		}
		s.CallAt[c] = through[c]
	}
}

// setUnfollowed sets Symbol.Unfollowed of the functions and methods.
func setUnfollowed(a *facts.Analysis, unfollowed map[string]int) {
	for i := range a.Symbols {
		a.Symbols[i].Unfollowed = unfollowed[a.Symbols[i].ID]
	}
}

// javaSupertypes returns the types a Java class, interface, enum or record names after extends and
// implements.
func javaSupertypes(f *file, n *node) []string {
	var out []string
	for _, c := range named(n) {
		switch c.Kind() {
		case "superclass":
			for _, t := range named(c) {
				out = append(out, f.text(t))
			}
		case "super_interfaces", "extends_interfaces":
			for _, l := range named(c) {
				for _, t := range named(l) {
					out = append(out, f.text(t))
				}
			}
		}
	}
	return out
}

// ktSupertypes returns the types a Kotlin class or object names after the colon, called as a
// constructor (Base()) or not (Port), and delegated or not (Port by impl).
func ktSupertypes(f *file, n *node) []string {
	var out []string
	for _, c := range named(n) {
		if c.Kind() != "delegation_specifiers" {
			continue
		}
		for _, d := range named(c) {
			walk(d, func(x *node) bool {
				if x.Kind() == "user_type" {
					out = append(out, f.text(x))
					return false
				}
				return true
			})
		}
	}
	return out
}
