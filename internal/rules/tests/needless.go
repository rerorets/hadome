package tests

import (
	"fmt"
	"strings"

	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
)

// guardsNothing is tests/guards-nothing: a test that calls no code of the repository and reads none
// of its values. Whatever the code does, the test's result stays the same.
type guardsNothing struct{}

func (guardsNothing) ID() string { return "tests/guards-nothing" }
func (guardsNothing) Cases() []engine.Case {
	return append(guardsNothingCases(), languageGuardsCases...)
}

// languageGuardsCases are cases in languages only a cgo build reads; see guards_cases_cgo.go.
var languageGuardsCases []engine.Case

func (guardsNothing) About() engine.About {
	return engine.About{Finds: engine.T("a new test that calls no code of the repository and reads none of its values"),
		Why: engine.T("the code can change in any way without this test noticing"),
		Fix: engine.T("call the code the test means to check, or delete the test if it is not needed")}
}

func (r guardsNothing) Find(in engine.Input) ([]engine.Finding, error) {
	var out []engine.Finding
	for _, t := range in.Facts.Tests {
		// One unresolved call may reach the repository; tests/target-unknown covers a test made only of those.
		// The own test of support code checks that code; a product test that calls only support code
		// guards nothing of the product.
		if len(t.Calls) == 0 && t.Unresolved == 0 && t.Reads == 0 && !t.Drives && !t.TestsSupport && !t.Skip.Skipped {
			msg := engine.T("%s calls no code of this repository and reads none of its values", t.Name)
			if t.CallsSupport {
				msg = engine.T("%s calls only test support code, and no product code nor its values", t.Name)
			}
			out = append(out, engine.Finding{
				Rule: r.ID(), File: t.Path, Line: t.Line, Anchor: t.Name,
				Message: msg,
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

// duplicate is tests/duplicate: tests with the same body. In one file the same tokens name the same
// things, so the same body is enough. Across files they may not (each file's own constant, helper or
// imported handler), so tests in different files are copies only when they also call the same known
// targets and nothing unidentified. Like a size limit, there is one finding per group of copies and
// its Value is how many tests are in it, so a third copy is a regression and renaming a copy is not.
// The finding sits at the group's first test by file and line.
type duplicate struct{}

func (duplicate) ID() string           { return "tests/duplicate" }
func (duplicate) Cases() []engine.Case { return duplicateCases() }
func (duplicate) About() engine.About {
	return engine.About{Finds: engine.T("tests with the same body, ignoring names, comments and whitespace, and, in different files, the same known targets; a new copy is a regression"),
		Why: engine.T("checking the same thing twice guards nothing more, and a change of behaviour has to be made in every copy"),
		Fix: engine.T("delete all but one, or change the input if they were meant to check different cases")}
}

func (r duplicate) Find(in engine.Input) ([]engine.Finding, error) {
	same := map[string][]facts.Test{} // in file and line order, as facts.Analysis lists tests
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
		for _, group := range copies(ts) {
			var where []string
			for _, t := range group {
				where = append(where, fmt.Sprintf("%s (%s:%d)", t.Name, t.Path, t.Line))
			}
			// All of a file's tests with one body are in one group, so the first file tells groups apart.
			out = append(out, engine.Finding{
				Rule: r.ID(), File: group[0].Path, Line: group[0].Line, Anchor: body, Value: len(group),
				Message: engine.T("%d tests have the same body: %s", len(group), strings.Join(where, ", ")),
				Why:     r.About().Why,
				Fix:     r.About().Fix,
			})
		}
	}
	return out, nil
}

// copies splits tests with the same body, in file and line order, into the groups of two or more
// that are copies of one another: tests in the same file, and tests in different files with the
// same known targets, joined as far as either links them.
func copies(ts []facts.Test) [][]facts.Test {
	parent := make([]int, len(ts))
	for i := range parent {
		parent[i] = i
	}
	root := func(i int) int {
		for parent[i] != i {
			i = parent[i]
		}
		return i
	}
	keys := make([]string, len(ts))
	for i, t := range ts {
		keys[i] = knownTargets(t)
	}
	for i := range ts {
		for j := i + 1; j < len(ts); j++ {
			if ts[i].Path == ts[j].Path || (keys[i] != "" && keys[i] == keys[j]) {
				parent[root(j)] = root(i)
			}
		}
	}
	byRoot := map[int][]facts.Test{}
	var order []int
	for i, t := range ts {
		r := root(i)
		if byRoot[r] == nil {
			order = append(order, r)
		}
		byRoot[r] = append(byRoot[r], t)
	}
	var out [][]facts.Test
	for _, r := range order {
		if len(byRoot[r]) > 1 {
			out = append(out, byRoot[r])
		}
	}
	return out
}

// knownTargets is the test's targets (Calls are sorted by target), joined, or "" when what it calls
// is not all known: it has no target, or a call that could not be identified.
func knownTargets(t facts.Test) string {
	if len(t.Calls) == 0 || t.Unresolved > 0 {
		return ""
	}
	targets := make([]string, len(t.Calls))
	for i, c := range t.Calls {
		targets[i] = c.Target
	}
	return strings.Join(targets, "\x00")
}
