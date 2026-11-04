package engine

import (
	"github.com/rerorets/hadome/internal/config"
)

// LoosenedID is the engine's own check on configuration. It is not a rule: no exception marker
// can silence it, because the configuration it judges is the thing an exception would hide behind.
const LoosenedID = "config/loosened"

// loosened returns a review for every change that makes head's configuration more permissive than
// base's. Head's configuration judges head, so loosening it is a decision for a person.
func loosened(base, head config.Config) []Finding {
	var out []Finding
	for _, c := range config.Diff(base, head) {
		if !c.Looser {
			continue
		}
		msg := "configuration changes " + c.Key + ": " + c.Removed + " → " + c.Added
		switch {
		case c.Added == "":
			msg = "configuration removes " + c.Key + " entry: " + c.Removed
		case c.Removed == "":
			msg = "configuration adds to " + c.Key + ": " + c.Added
		}
		out = append(out, Finding{
			Rule: LoosenedID, File: c.File, Line: c.Line, Anchor: c.Key + "\x00" + c.Removed + "\x00" + c.Added, Review: true,
			Message: msg,
			Why:     "the configuration in this change judges this change; removing, changing or adding to it can make the check more permissive",
			Fix:     "if the removal is intended, a person approves it; otherwise restore the entry",
		})
	}
	return out
}
