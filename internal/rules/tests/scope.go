package tests

import (
	"github.com/rerorets/hadome/internal/engine"
)

// scopeShrunk is tests/scope-shrunk: every target guarded by an active test in base must still be
// guarded in head, unless the target itself is gone.
type scopeShrunk struct{}

func (scopeShrunk) ID() string           { return "tests/scope-shrunk" }
func (scopeShrunk) Cases() []engine.Case { return scopeCases() }

func (r scopeShrunk) Measure(in engine.Input) ([]engine.Item, error) {
	seen := map[string]bool{}
	var out []engine.Item
	for _, t := range in.Facts.Tests {
		if t.Skip.Skipped {
			continue // a skipped test guards nothing
		}
		for _, c := range t.Calls {
			target := c.Target
			if seen[target] {
				continue
			}
			seen[target] = true
			out = append(out, engine.Item{
				Rule: r.ID(), Key: target, File: t.Path, Line: t.Line,
				Detail: "guarded scope shrank: " + target + " is no longer exercised by any active test (was: " + t.Name + ")",
				Why:    "the behaviour of " + target + " can now change without any test noticing",
				Fix:    "keep or restore a test that calls " + target + ", or remove " + target + " itself if it is no longer needed",
			})
		}
	}
	return out, nil
}

// RemovedLegitimately is true when the target no longer exists in head: the code was removed or renamed.
func (scopeShrunk) RemovedLegitimately(it engine.Item, head engine.Input) (bool, error) {
	return !head.Facts.HasSymbol(it.Key), nil
}

// targetUnknown is tests/target-unknown: a test whose target hadome could not identify at all.
// It needs a person's review instead of failing, because nothing uncertain may block.
type targetUnknown struct{}

func (targetUnknown) ID() string           { return "tests/target-unknown" }
func (targetUnknown) Cases() []engine.Case { return unknownCases() }

func (r targetUnknown) Find(in engine.Input) ([]engine.Finding, error) {
	var out []engine.Finding
	for _, t := range in.Facts.Tests {
		if len(t.Calls) == 0 && t.Unresolved > 0 && !t.Skip.Skipped {
			out = append(out, engine.Finding{
				Rule: r.ID(), File: t.Path, Line: t.Line, Anchor: t.Name, Review: true,
				Message: "could not identify what " + t.Name + " tests",
				Why:     "hadome cannot tell whether removing this test would shrink what the suite guards",
				Fix:     "call the code under test through its package-level API (a function, or a method on a value built by a constructor)",
			})
		}
	}
	return out, nil
}

// All returns the tests/ rules of this slice.
func All() []engine.Rule {
	return []engine.Rule{NoClock(), NoSleep(), AlwaysTrue(), skipWithoutReason{}, scopeShrunk{}, targetUnknown{}, invariantUnguarded{}, NoMock(), purityLost{}, guardsNothing{}, duplicate{}}
}
