package tests

import (
	"fmt"
	"sort"
	"strings"

	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
)

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

// duplicate is tests/duplicate: tests with the same body. Like a size limit, there is one finding per
// body and its Value is how many tests share it, so a third copy is a regression and renaming a copy
// is not. The finding sits at the first test by file and line.
type duplicate struct{}

func (duplicate) ID() string           { return "tests/duplicate" }
func (duplicate) Cases() []engine.Case { return duplicateCases() }

func (r duplicate) Find(in engine.Input) ([]engine.Finding, error) {
	same := map[string][]facts.Test{}
	for _, t := range in.Facts.Tests {
		if t.Body != "" && !t.Skip.Skipped {
			same[t.Body] = append(same[t.Body], t)
		}
	}
	var out []engine.Finding
	for body, ts := range same {
		if len(ts) < 2 {
			continue
		}
		sort.Slice(ts, func(i, j int) bool {
			if ts[i].Path != ts[j].Path {
				return ts[i].Path < ts[j].Path
			}
			return ts[i].Line < ts[j].Line
		})
		var where []string
		for _, t := range ts {
			where = append(where, fmt.Sprintf("%s (%s:%d)", t.Name, t.Path, t.Line))
		}
		out = append(out, engine.Finding{
			Rule: r.ID(), File: ts[0].Path, Line: ts[0].Line, Anchor: body, Value: len(ts),
			Message: fmt.Sprintf("%d tests have the same body: %s", len(ts), strings.Join(where, ", ")),
			Why:     "checking the same thing twice guards nothing more, and a change of behaviour has to be made in every copy",
			Fix:     "delete all but one, or change the input if they were meant to check different cases",
		})
	}
	return out, nil
}
