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
func (guardsNothing) About() engine.About {
	return engine.About{Finds: engine.T("a new test that calls no code of the repository and reads none of its values"),
		Why: engine.T("the code can change in any way without this test noticing"),
		Fix: engine.T("call the code the test means to check, or delete the test if it is not needed")}
}

func (r guardsNothing) Find(in engine.Input) ([]engine.Finding, error) {
	var out []engine.Finding
	for _, t := range in.Facts.Tests {
		// One unresolved call may reach the repository; tests/target-unknown covers a test made only of those.
		if len(t.Calls) == 0 && t.Unresolved == 0 && t.Reads == 0 && !t.Skip.Skipped {
			out = append(out, engine.Finding{
				Rule: r.ID(), File: t.Path, Line: t.Line, Anchor: t.Name,
				Message: engine.T("%s calls no code of this repository and reads none of its values", t.Name),
				Why:     r.About().Why,
				Fix:     r.About().Fix,
			})
		}
	}
	return out, nil
}

// noAssertion is tests/no-assertion: a test with no call that checks anything. It fails only when
// something panics, so the code can return any wrong value without the test noticing.
type noAssertion struct{}

func (noAssertion) ID() string           { return "tests/no-assertion" }
func (noAssertion) Cases() []engine.Case { return noAssertionCases() }
func (noAssertion) About() engine.About {
	return engine.About{Finds: engine.T("a new test with no assertion, in its body or in the helpers of its test files"),
		Why: engine.T("the test passes whatever the code returns, and fails only if something panics or throws"),
		Fix: engine.T("assert on the behaviour the test means to check; if the function is an assertion hadome does not know, " +
			"add it to rules.tests.assertions; if the test only checks that nothing throws, add an exception with the reason"),
		Keys: []string{"rules.tests.assertions"}}
}

func (r noAssertion) Find(in engine.Input) ([]engine.Finding, error) {
	var out []engine.Finding
	for _, t := range in.Facts.Tests {
		if t.Asserts == 0 && !t.AssertsUnknown && !t.Skip.Skipped {
			out = append(out, engine.Finding{
				Rule: r.ID(), File: t.Path, Line: t.Line, Anchor: t.Name,
				Message: engine.T("%s checks nothing: it has no assertion", t.Name),
				Why:     r.About().Why,
				Fix:     r.About().Fix,
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
func (duplicate) About() engine.About {
	return engine.About{Finds: engine.T("tests with the same body, ignoring names, comments and whitespace; a new copy is a regression"),
		Why: engine.T("checking the same thing twice guards nothing more, and a change of behaviour has to be made in every copy"),
		Fix: engine.T("delete all but one, or change the input if they were meant to check different cases")}
}

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
			Message: engine.T("%d tests have the same body: %s", len(ts), strings.Join(where, ", ")),
			Why:     r.About().Why,
			Fix:     r.About().Fix,
		})
	}
	return out, nil
}
