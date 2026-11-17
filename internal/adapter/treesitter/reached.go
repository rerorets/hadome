//go:build cgo

package treesitter

import (
	"sort"

	"github.com/rerorets/hadome/internal/facts"
)

// reached is what a piece of code reaches.
type reached struct {
	io    bool
	calls map[string]int // the ID of each function called -> the line of its first call
	units map[string]bool
	// at is the line being read; held keeps it while the code of a value used there is read, so
	// that a call the value makes counts where the value is used.
	at   int
	held bool
}

func newReached() reached { return reached{calls: map[string]int{}, units: map[string]bool{}} }

// call records a call of id at the line being read, unless an earlier line called it.
func (g *reached) call(id string) {
	if _, ok := g.calls[id]; !ok {
		g.calls[id] = g.at
	}
}

// read moves the line being read to n's, unless it is held.
func (g *reached) read(n *node) {
	if !g.held {
		g.at = line(n)
	}
}

// lines returns the line of each call, nil for no call.
func (g *reached) lines() map[string]int {
	if len(g.calls) == 0 {
		return nil
	}
	return g.calls
}

// entry is code at path and line that is an entry point, with its calls.
func (g *reached) entry(path string, at int) facts.Entry {
	return facts.Entry{Path: path, Line: at, Calls: g.ids(), CallAt: g.lines()}
}

// ids returns the IDs of the calls, sorted.
func (g *reached) ids() []string {
	out := make([]string, 0, len(g.calls))
	for k := range g.calls {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
