package engine

import (
	"encoding/json"
	"fmt"
	"io"
)

// JSONSchemaVersion is the version of the JSON report. It changes only by adding fields.
const JSONSchemaVersion = 1

type jsonReport struct {
	Schema        int           `json:"schema"`
	Regressions   []jsonFinding `json:"regressions"`
	Reviews       []jsonFinding `json:"reviews"`
	NewExceptions []Marker      `json:"new_exceptions"`
	Candidates    []Candidate   `json:"candidates"`
	Untouched     int           `json:"untouched"`
	ExitCode      int           `json:"exit_code"`
}

// jsonFinding is a finding with its wording written out, so that the JSON carries plain strings.
type jsonFinding struct {
	Rule    string `json:"rule"`
	File    string `json:"file"`
	Line    int    `json:"line"`
	Message string `json:"message"`
	Why     string `json:"why"`
	Fix     string `json:"fix"`
	Value   int    `json:"value,omitempty"`
}

func jsonFindings(fs []Finding, lang string) []jsonFinding {
	out := make([]jsonFinding, 0, len(fs))
	for _, f := range fs {
		out = append(out, jsonFinding{Rule: f.Rule, File: f.File, Line: f.Line,
			Message: f.Message.In(lang), Why: f.Why.In(lang), Fix: f.Fix.In(lang), Value: f.Value})
	}
	return out
}

// WriteJSON writes the report for agents and CI. Only the values of message, why and fix depend on
// lang; the keys and everything else are the same in every language.
func WriteJSON(w io.Writer, r Report, lang string) error {
	out := jsonReport{Schema: JSONSchemaVersion,
		Regressions: jsonFindings(r.Regressions, lang), Reviews: jsonFindings(r.Reviews, lang),
		NewExceptions: r.NewExceptions, Candidates: r.Candidates, Untouched: r.Untouched, ExitCode: r.ExitCode()}
	if out.NewExceptions == nil {
		out.NewExceptions = []Marker{}
	}
	if out.Candidates == nil {
		out.Candidates = []Candidate{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// WriteText writes the report for people, with the findings' wording and the summary in lang.
func WriteText(w io.Writer, r Report, lang string) {
	for _, f := range r.Regressions {
		fmt.Fprintf(w, "✗ %-28s %s\n  %s\n  why: %s\n  fix: %s\n",
			f.Rule, f.Message.In(lang), location(f.File, f.Line), f.Why.In(lang), f.Fix.In(lang))
	}
	for _, f := range r.Reviews {
		fmt.Fprintf(w, "? %-28s %s\n  %s\n  why: %s\n", f.Rule, f.Message.In(lang), location(f.File, f.Line), f.Why.In(lang))
	}
	for _, m := range r.NewExceptions {
		fmt.Fprintf(w, "? %-28s new exception: %s\n  %s:%d\n", m.Rule, m.Reason, m.File, m.Line)
	}
	if len(r.Candidates) > 0 {
		fmt.Fprintln(w, "Invariant candidates this change adds (information only; propose them to a person, `hadome suggest` prints them as configuration):")
		for _, c := range r.Candidates {
			fmt.Fprintf(w, "  %s  %s — %s\n", c.Key(), location(c.File, c.Line), c.Why)
		}
	}
	fmt.Fprintln(w, summary(r).In(lang))
}

// location is "file:line", or just the file for findings about a whole file, such as configuration.
func location(file string, line int) string {
	if line == 0 {
		return file
	}
	return fmt.Sprintf("%s:%d", file, line)
}

func summary(r Report) Text {
	switch r.ExitCode() {
	case ExitRegression:
		return T("%d regression(s). %d existing finding(s) unchanged (not blocking).", len(r.Regressions), r.Untouched)
	case ExitReview:
		return T("No regressions. %d item(s) need a person's review. %d existing finding(s) unchanged.",
			len(r.Reviews)+len(r.NewExceptions), r.Untouched)
	default:
		return T("No regressions. %d existing finding(s) unchanged (not blocking).", r.Untouched)
	}
}
