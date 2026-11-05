package engine

import (
	"github.com/rerorets/hadome/internal/config"
)

// LoosenedID is the engine's own check on configuration. It is not a rule: no exception marker
// can silence it, because the configuration it judges is the thing an exception would hide behind.
const LoosenedID = "config/loosened"

// introduces reports whether the change adds .hadome.yml where base has none. Such a change
// introduces hadome: hadome did not run on base, so there is nothing to loosen, and a first
// configuration written as the setup steps say must not stop on a review.
func introduces(base, head config.Config) bool {
	return len(base.Layers) == 0 && len(head.Layers) > 0
}

// loosened returns a review for every change that makes head's configuration more permissive than
// base's. Head's configuration judges head, so loosening it is a decision for a person.
func loosened(base, head config.Config) []Finding {
	var out []Finding
	for _, c := range config.Diff(base, head) {
		if !c.Looser {
			continue
		}
		msg := T("configuration changes %s: %s → %s", c.Key, c.Removed, c.Added)
		switch {
		case c.Added == "":
			msg = T("configuration removes %s entry: %s", c.Key, c.Removed)
		case c.Removed == "":
			msg = T("configuration adds to %s: %s", c.Key, c.Added)
		}
		out = append(out, Finding{
			Rule: LoosenedID, File: c.File, Line: c.Line, Anchor: c.Key + "\x00" + c.Removed + "\x00" + c.Added, Review: true,
			Message: msg,
			Why:     T("the configuration in this change judges this change; removing, changing or adding to it can make the check more permissive"),
			Fix:     T("if the removal is intended, a person approves it; otherwise restore the entry"),
		})
	}
	return out
}
