// Package tests holds the tests/ rules: how tests are written, and what they guard.
//
// Design: docs/design/rules-tests.md
package tests

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
)

// hazardRule reports every occurrence of one hazard category inside tests.
type hazardRule struct {
	id       string
	category facts.HazardCategory
	message  string // a format with one verb, for the evidence
	finds    string
	why      string
	fix      string
	keys     []string
	cases    []engine.Case
	// applies, when set, returns which tests of a version the rule looks at.
	applies func(in engine.Input) func(facts.Test) bool
	// excuses, when set, returns which hazards of a test the rule leaves out though the test is
	// looked at.
	excuses func(in engine.Input) func(facts.Test, facts.Hazard) bool
}

func (r hazardRule) ID() string           { return r.id }
func (r hazardRule) Cases() []engine.Case { return r.cases }
func (r hazardRule) About() engine.About {
	return engine.About{Finds: engine.T(r.finds), Why: engine.T(r.why), Fix: engine.T(r.fix), Keys: r.keys}
}

func (r hazardRule) Find(in engine.Input) ([]engine.Finding, error) {
	var out []engine.Finding
	seen := map[string]bool{}
	applies := func(facts.Test) bool { return true }
	if r.applies != nil {
		applies = r.applies(in)
	}
	excused := func(facts.Test, facts.Hazard) bool { return false }
	if r.excuses != nil {
		excused = r.excuses(in)
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
			if h.Category != r.category || seen[key] || excused(t, h) {
				continue
			}
			seen[key] = true
			anchor := t.Name + "|" + h.Evidence
			if file != t.Path {
				anchor = file + "|" + h.Evidence
			}
			out = append(out, engine.Finding{
				Rule: r.id, File: file, Line: h.Line, Anchor: anchor,
				Message: engine.T(r.message, excerpt(h.Evidence)), Why: engine.T(r.why), Fix: engine.T(r.fix),
			})
		}
	}
	return out, nil
}

// excerptLimit is the most characters of a hazard's source text a message shows.
const excerptLimit = 80

// excerpt is the part of a hazard's source text a one-line message shows: its first line, cut to
// excerptLimit characters, with "…" where anything was left out. The anchor keeps the whole text.
func excerpt(evidence string) string {
	first, rest, multiline := strings.Cut(strings.TrimSpace(evidence), "\n")
	first = strings.TrimSpace(first)
	if r := []rune(first); len(r) > excerptLimit {
		return string(r[:excerptLimit]) + "…"
	}
	if multiline && strings.TrimSpace(rest) != "" {
		return first + " …"
	}
	return first
}

// NoClock is tests/no-clock.
func NoClock() engine.Rule {
	return hazardRule{
		id: "tests/no-clock", category: facts.HazardClock,
		message: "test reads the clock: %s",
		finds:   "a test that reads the current time directly",
		keys:    []string{"rules.tests.hazards.clock"},
		why:     "the result depends on when the test runs; it passes today and fails at midnight, at month end, or in another time zone",
		fix:     "pass the time in as a value, or inject a clock",
		cases:   clockCases(),
	}
}

// NoSleep is tests/no-sleep.
func NoSleep() engine.Rule {
	return hazardRule{
		id: "tests/no-sleep", category: facts.HazardSleep,
		message: "test waits by sleeping: %s",
		finds:   "a test that waits by sleeping",
		keys:    []string{"rules.tests.hazards.sleep"},
		why:     "a sleep is either too long (a slow suite) or too short (a flaky test); it never waits for the actual event",
		fix:     "wait for the event itself: a channel, a callback, or a condition with a deadline that fails loudly",
		cases:   sleepCases(),
	}
}

// AlwaysTrue is tests/always-true.
func AlwaysTrue() engine.Rule {
	return hazardRule{
		id: "tests/always-true", category: facts.HazardAlwaysTrue,
		message: "assertion cannot fail: %s",
		finds:   "an assertion that cannot fail, such as assertTrue(true) or comparing a value with itself",
		why:     "the test reports success no matter what the code does",
		fix:     "assert on the behaviour under test",
		cases:   alwaysTrueCases(),
	}
}

// skipWithoutReason is tests/skip-without-reason.
type skipWithoutReason struct{}

func (skipWithoutReason) ID() string           { return "tests/skip-without-reason" }
func (skipWithoutReason) Cases() []engine.Case { return skipCases() }
func (skipWithoutReason) About() engine.About {
	return engine.About{Finds: engine.T("a test that is skipped or disabled without a reason"),
		Why: engine.T("a skipped test protects nothing, and without a reason nobody can decide when it should come back"),
		Fix: engine.T("write the reason in the skip itself")}
}

func (r skipWithoutReason) Find(in engine.Input) ([]engine.Finding, error) {
	var out []engine.Finding
	for _, t := range in.Facts.Tests {
		if (t.Skip.Skipped || t.Skip.Conditional) && t.Skip.Reason == "" {
			out = append(out, engine.Finding{
				Rule: r.ID(), File: t.Path, Line: t.Skip.Line, Anchor: t.Name,
				Message: engine.T("%s is skipped without a reason", t.Name),
				Why:     r.About().Why,
				Fix:     engine.T("write the reason in the skip itself, for example %s", skipExample(t.Path)),
			})
		}
	}
	return out, nil
}

func itoa(n int) string { return strconv.Itoa(n) }

// skipExample shows a skip with a reason in the form the test file's language reads it.
func skipExample(path string) engine.Text {
	switch filepath.Ext(path) {
	case ".go":
		return engine.Verbatim(`t.Skip("waits for …")`)
	case ".py":
		return engine.Verbatim(`@pytest.mark.skip(reason="waits for …")`)
	case ".rs":
		return engine.Verbatim(`#[ignore = "waits for …"]`)
	case ".java", ".kt":
		return engine.Verbatim(`@Disabled("waits for …")`)
	case ".ts", ".tsx", ".js", ".jsx", ".mts", ".cts", ".mjs", ".cjs":
		return engine.T("a comment on or above it.skip: // waits for …")
	}
	return engine.T("a reason the language's skip accepts")
}
