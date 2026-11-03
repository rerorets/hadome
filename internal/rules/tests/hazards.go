// Package tests holds the tests/ rules: how tests are written, and what they guard.
//
// Design: docs/design/rules-tests.md
package tests

import (
	"path/filepath"
	"strconv"

	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
)

// hazardRule reports every occurrence of one hazard category inside tests.
type hazardRule struct {
	id       string
	category facts.HazardCategory
	message  string
	why      string
	fix      string
	cases    []engine.Case
	// applies, when set, returns which tests of a version the rule looks at.
	applies func(in engine.Input) func(facts.Test) bool
}

func (r hazardRule) ID() string           { return r.id }
func (r hazardRule) Cases() []engine.Case { return r.cases }

func (r hazardRule) Find(in engine.Input) ([]engine.Finding, error) {
	var out []engine.Finding
	seen := map[string]bool{}
	applies := func(facts.Test) bool { return true }
	if r.applies != nil {
		applies = r.applies(in)
	}
	for _, t := range in.Facts.Tests {
		if !applies(t) {
			continue
		}
		for _, h := range t.Hazards {
			file := h.Path
			if file == "" {
				file = t.Path
			}
			// A hazard in a helper is reached from every test that calls it; report it once.
			key := file + "\x00" + h.Evidence + "\x00" + itoa(h.Line)
			if h.Category != r.category || seen[key] {
				continue
			}
			seen[key] = true
			anchor := t.Name + "|" + h.Evidence
			if file != t.Path {
				anchor = file + "|" + h.Evidence
			}
			out = append(out, engine.Finding{
				Rule: r.id, File: file, Line: h.Line, Anchor: anchor,
				Message: r.message + ": " + h.Evidence, Why: r.why, Fix: r.fix,
			})
		}
	}
	return out, nil
}

// NoClock is tests/no-clock.
func NoClock() engine.Rule {
	return hazardRule{
		id: "tests/no-clock", category: facts.HazardClock,
		message: "test reads the clock",
		why:     "the result depends on when the test runs; it passes today and fails at midnight, at month end, or in another time zone",
		fix:     "pass the time in as a value, or inject a clock",
		cases:   clockCases(),
	}
}

// NoSleep is tests/no-sleep.
func NoSleep() engine.Rule {
	return hazardRule{
		id: "tests/no-sleep", category: facts.HazardSleep,
		message: "test waits by sleeping",
		why:     "a sleep is either too long (a slow suite) or too short (a flaky test); it never waits for the actual event",
		fix:     "wait for the event itself: a channel, a callback, or a condition with a deadline that fails loudly",
		cases:   sleepCases(),
	}
}

// AlwaysTrue is tests/always-true.
func AlwaysTrue() engine.Rule {
	return hazardRule{
		id: "tests/always-true", category: facts.HazardAlwaysTrue,
		message: "assertion cannot fail",
		why:     "the test reports success no matter what the code does",
		fix:     "assert on the behaviour under test",
		cases:   alwaysTrueCases(),
	}
}

// skipWithoutReason is tests/skip-without-reason.
type skipWithoutReason struct{}

func (skipWithoutReason) ID() string           { return "tests/skip-without-reason" }
func (skipWithoutReason) Cases() []engine.Case { return skipCases() }

func (r skipWithoutReason) Find(in engine.Input) ([]engine.Finding, error) {
	var out []engine.Finding
	for _, t := range in.Facts.Tests {
		if (t.Skip.Skipped || t.Skip.Conditional) && t.Skip.Reason == "" {
			out = append(out, engine.Finding{
				Rule: r.ID(), File: t.Path, Line: t.Skip.Line, Anchor: t.Name,
				Message: t.Name + " is skipped without a reason",
				Why:     "a skipped test protects nothing, and without a reason nobody can decide when it should come back",
				Fix:     "write the reason in the skip itself, for example " + skipExample(t.Path),
			})
		}
	}
	return out, nil
}

func itoa(n int) string { return strconv.Itoa(n) }

// skipExample shows a skip with a reason in the form the test file's language reads it.
func skipExample(path string) string {
	switch filepath.Ext(path) {
	case ".go":
		return `t.Skip("waits for …")`
	case ".py":
		return `@pytest.mark.skip(reason="waits for …")`
	case ".rs":
		return `#[ignore = "waits for …"]`
	case ".java", ".kt":
		return `@Disabled("waits for …")`
	case ".ts", ".tsx", ".js", ".jsx", ".mts", ".cts", ".mjs", ".cjs":
		return "a comment on or above it.skip: // waits for …"
	}
	return "a reason the language's skip accepts"
}
