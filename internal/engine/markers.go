package engine

import (
	"regexp"
	"strings"

	"github.com/rerorets/hadome/internal/facts"
)

// The prefix is "gate:" and never the tool's name, so renaming the tool never forces users to edit code.
var markerPattern = regexp.MustCompile(`gate:allow\s+(\S+)(?:\s+reason:(.*))?`)

const (
	ruleMarkerWithoutReason = "engine/marker-without-reason"
	ruleMarkerUnknownRule   = "engine/marker-unknown-rule"
	ruleMarkerUnused        = "engine/marker-unused"
)

// parseMarkers returns the valid markers in the comments, and findings for invalid ones.
func parseMarkers(comments []facts.Comment, known map[string]bool) ([]Marker, []Finding) {
	var valid []Marker
	var bad []Finding
	for _, c := range comments {
		m := markerPattern.FindStringSubmatch(c.Text)
		if m == nil {
			continue
		}
		rule, reason := m[1], strings.TrimSpace(m[2])
		switch {
		case reason == "":
			bad = append(bad, Finding{
				Rule: ruleMarkerWithoutReason, File: c.Path, Line: c.Line, Anchor: rule,
				Message: "exception for " + rule + " has no reason",
				Why:     "an exception without a reason cannot be reviewed, so it has no effect",
				Fix:     "write the reason after 'reason:', or remove the marker",
			})
		case !known[rule]:
			bad = append(bad, Finding{
				Rule: ruleMarkerUnknownRule, File: c.Path, Line: c.Line, Anchor: rule,
				Message: "exception names an unknown rule: " + rule,
				Why:     "a marker for a rule that does not exist suppresses nothing",
				Fix:     "use a rule id that hadome reports, or remove the marker",
			})
		default:
			valid = append(valid, Marker{Rule: rule, Reason: reason, File: c.Path, Line: c.Line})
		}
	}
	return valid, bad
}

// suppress removes findings covered by a marker (same rule and file, on the marker's line or the
// next one) and reports markers that cover nothing.
func suppress(findings []Finding, markers []Marker) ([]Finding, []Finding) {
	used := make([]bool, len(markers))
	var kept []Finding
	for _, f := range findings {
		covered := false
		for i, m := range markers {
			if m.Rule == f.Rule && m.File == f.File && (f.Line == m.Line || f.Line == m.Line+1) {
				used[i], covered = true, true
			}
		}
		if !covered {
			kept = append(kept, f)
		}
	}
	var unused []Finding
	for i, m := range markers {
		if !used[i] {
			unused = append(unused, Finding{
				Rule: ruleMarkerUnused, File: m.File, Line: m.Line, Anchor: m.Rule + "|" + m.Reason,
				Message: "exception for " + m.Rule + " suppresses nothing",
				Why:     "stale exceptions pile up and hide what is really excepted",
				Fix:     "remove the marker",
			})
		}
	}
	return kept, unused
}
