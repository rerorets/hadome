package treesitter

import (
	"strings"

	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/facts"
)

// tables are a language's hazard and io tables: the built-in entries plus those configuration adds.
type tables struct {
	calls map[string]facts.HazardCategory // a callee as written, such as "Date.now" or "time.sleep"
	mocks []string                        // units whose import makes a file use mocks
	io    []string                        // units that perform input or output, and everything under them
	// asserts are the functions configuration adds to the assertions, as tests call them.
	asserts map[string]bool
}

// with returns a copy with the configuration's entries added. Configuration never removes any.
func (t *tables) with(c config.Config) *tables {
	out := &tables{calls: map[string]facts.HazardCategory{}, mocks: append([]string{}, t.mocks...),
		io: append(append([]string{}, t.io...), c.IO...), asserts: map[string]bool{}}
	for _, e := range c.Assertions {
		out.asserts[e] = true
	}
	for k, v := range t.calls {
		out.calls[k] = v
	}
	for cat, entries := range c.Hazards {
		for _, e := range entries {
			if cat == "mock" {
				out.mocks = append(out.mocks, e)
			} else {
				out.calls[e] = facts.HazardCategory(cat)
			}
		}
	}
	return out
}

// under reports whether unit is entry or inside it: the next character is a path separator of
// one of the languages ("/", ".", "::"). Go's "/..." form is accepted too.
func under(unit, entry string) bool {
	entry = strings.TrimSuffix(entry, "/...")
	if unit == entry {
		return true
	}
	rest, ok := strings.CutPrefix(unit, entry)
	return ok && (strings.HasPrefix(rest, "/") || strings.HasPrefix(rest, ".") || strings.HasPrefix(rest, "::"))
}

func (t *tables) isIO(unit string) bool   { return anyUnder(unit, t.io) }
func (t *tables) isMock(unit string) bool { return anyUnder(unit, t.mocks) }

func anyUnder(unit string, entries []string) bool {
	for _, e := range entries {
		if under(unit, e) {
			return true
		}
	}
	return false
}
