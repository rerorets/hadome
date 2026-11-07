package unknown_test

import (
	"testing"

	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/rules/unknown"
)

// viewpoint: external contract
// target: unknown.All
// What hadome could not read or identify never blocks and is never hidden: every unknown/ rule
// sends what it finds to a person.
func TestEveryUnknownRuleNeedsReview(t *testing.T) {
	for _, r := range unknown.All() {
		d, ok := r.(engine.Describer)
		if !ok || !d.About().Review {
			t.Errorf("%s does not ask for review", r.ID())
		}
	}
}
