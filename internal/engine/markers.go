package engine

import (
	"io/fs"
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
				Message: T("exception for %s has no reason", rule),
				Why:     T("an exception without a reason cannot be reviewed, so it has no effect"),
				Fix:     T("write the reason after 'reason:', or remove the marker"),
			})
		case !known[rule]:
			bad = append(bad, Finding{
				Rule: ruleMarkerUnknownRule, File: c.Path, Line: c.Line, Anchor: rule,
				Message: T("exception names an unknown rule: %s", rule),
				Why:     T("a marker for a rule that does not exist suppresses nothing"),
				Fix:     T("use a rule id that hadome reports, or remove the marker"),
			})
		default:
			valid = append(valid, Marker{Rule: rule, Reason: reason, File: c.Path, Line: c.Line})
		}
	}
	return valid, bad
}

// suppress removes findings covered by a marker of the same rule and file, and reports markers that
// cover nothing. A marker covers a finding on its own line or the next one; a whole-file finding is
// covered only by a marker at the top of the file.
func suppress(findings []Finding, markers []Marker) ([]Finding, []Finding) {
	used := make([]bool, len(markers))
	var kept []Finding
	for _, f := range findings {
		covered := false
		for i, m := range markers {
			if m.Rule == f.Rule && m.File == f.File && covers(m, f) {
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
				Message: T("exception for %s suppresses nothing", m.Rule),
				Why:     T("stale exceptions pile up and hide what is really excepted"),
				Fix:     T("remove the marker"),
			})
		}
	}
	return kept, unused
}

func covers(m Marker, f Finding) bool {
	if f.Whole {
		return m.Top
	}
	return f.Line == m.Line || f.Line == m.Line+1
}

// markTop sets Top on each marker on a comment line that only blank lines, comment lines, a shebang
// on line 1 and YAML document separators precede. A line with code before its comment is no comment
// line.
func markTop(markers []Marker, comments []facts.Comment, fsys fs.FS) {
	for i, m := range markers {
		src, err := fs.ReadFile(fsys, m.File)
		if err != nil {
			continue
		}
		markers[i].Top = atTop(strings.Split(string(src), "\n"), commentLines(comments, m.File), m.Line)
	}
}

// commentLines maps each line a comment of the file covers to whether the comment starts on it.
func commentLines(comments []facts.Comment, file string) map[int]bool {
	out := map[int]bool{}
	for _, c := range comments {
		if c.Path != file {
			continue
		}
		for l := c.Line; l <= max(c.Line, c.EndLine); l++ {
			out[l] = out[l] || l == c.Line
		}
	}
	return out
}

func atTop(lines []string, comments map[int]bool, line int) bool {
	for n := 1; n <= line && n <= len(lines); n++ {
		text := strings.TrimSpace(lines[n-1])
		starts, in := comments[n]
		switch {
		case text == "" || text == "---" || n == 1 && strings.HasPrefix(text, "#!"):
		case in && !starts:
		case starts && startsComment(text):
		default:
			return false
		}
	}
	return true
}

func startsComment(text string) bool {
	for _, p := range []string{"//", "/*", "#"} {
		if strings.HasPrefix(text, p) {
			return true
		}
	}
	return false
}
