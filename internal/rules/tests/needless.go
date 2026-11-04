package tests

import "github.com/rerorets/hadome/internal/engine"

// guardsNothing is tests/guards-nothing: a test that calls no code of the repository and reads none
// of its values. Whatever the code does, the test's result stays the same.
type guardsNothing struct{}

func (guardsNothing) ID() string           { return "tests/guards-nothing" }
func (guardsNothing) Cases() []engine.Case { return guardsNothingCases() }

func (r guardsNothing) Find(in engine.Input) ([]engine.Finding, error) {
	var out []engine.Finding
	for _, t := range in.Facts.Tests {
		// One unresolved call may reach the repository; tests/target-unknown covers a test made only of those.
		if len(t.Calls) == 0 && t.Unresolved == 0 && t.Reads == 0 && !t.Skip.Skipped {
			out = append(out, engine.Finding{
				Rule: r.ID(), File: t.Path, Line: t.Line, Anchor: t.Name,
				Message: t.Name + " calls no code of this repository and reads none of its values",
				Why:     "the code can change in any way without this test noticing",
				Fix:     "call the code the test means to check, or delete the test if it is not needed",
			})
		}
	}
	return out, nil
}
