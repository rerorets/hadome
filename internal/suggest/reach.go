package suggest

import "github.com/rerorets/hadome/internal/facts"

// reach answers, for one version, which functions reach input or output and which an entry point
// that can be retried reaches. Design: docs/design/suggest.md, "Conditions of idempotent".
type reach struct {
	io      map[string]bool // function IDs that reach input or output
	unitIO  map[string]bool // units that reach input or output through their imports
	unitOf  map[string]string
	reached map[string]bool // function IDs an entry point reaches
	entries bool            // whether the version has any entry point
}

func newReach(a *facts.Analysis) *reach {
	r := &reach{io: map[string]bool{}, unitIO: unitsReachingIO(a), unitOf: map[string]string{},
		reached: map[string]bool{}, entries: len(a.Entries) > 0}
	for _, f := range a.Files {
		r.unitOf[f.Path] = f.Unit
	}
	syms := map[string]facts.Symbol{}
	for _, s := range a.Symbols {
		if s.Kind == facts.SymbolFunction || s.Kind == facts.SymbolMethod {
			syms[s.ID] = s
		}
	}
	// Reaching input or output spreads from callee to caller until nothing changes, so that calls
	// in a cycle give the same answer whichever function is looked at first.
	for _, s := range syms {
		if r.ownIO(s) {
			r.io[s.ID] = true
		}
	}
	for changed := true; changed; {
		changed = false
		for _, s := range syms {
			if r.io[s.ID] || !s.Traced {
				continue
			}
			for _, c := range s.Calls {
				if r.io[c] {
					r.io[s.ID], changed = true, true
					break
				}
			}
		}
	}
	var queue []string
	for _, e := range a.Entries {
		if e.Symbol != "" {
			queue = append(queue, e.Symbol)
		}
		queue = append(queue, e.Calls...)
	}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if r.reached[id] {
			continue
		}
		r.reached[id] = true
		queue = append(queue, syms[id].Calls...)
	}
	return r
}

// ownIO reports whether a function reaches input or output without looking at the functions it
// calls. A function the adapter did not trace is decided by its unit.
func (r *reach) ownIO(s facts.Symbol) bool {
	if !s.Traced {
		return r.unitIO[r.unitOf[s.Path]]
	}
	if s.IO {
		return true
	}
	for _, u := range s.Units {
		if r.unitIO[u] {
			return true
		}
	}
	return false
}

// retried reports whether a symbol reaches input or output and, when the version has an entry
// point and the symbol was traced, whether an entry point reaches it.
func (r *reach) retried(s facts.Symbol) bool {
	return r.io[s.ID] && (!s.Traced || !r.entries || r.reached[s.ID])
}

// unitsReachingIO are the units that import something that does input or output, or import a unit
// of the repository that does, at any depth.
func unitsReachingIO(a *facts.Analysis) map[string]bool {
	out := map[string]bool{}
	importers := map[string][]string{}
	var queue []string
	for _, im := range a.Imports {
		switch {
		case im.IO && !out[im.FromUnit]:
			out[im.FromUnit] = true
			queue = append(queue, im.FromUnit)
		case im.Internal:
			importers[im.ToUnit] = append(importers[im.ToUnit], im.FromUnit)
		}
	}
	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]
		for _, from := range importers[u] {
			if !out[from] {
				out[from] = true
				queue = append(queue, from)
			}
		}
	}
	return out
}
