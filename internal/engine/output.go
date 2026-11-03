package engine

import (
	"encoding/json"
	"fmt"
	"io"
)

// JSONSchemaVersion is the version of the JSON report. It changes only by adding fields.
const JSONSchemaVersion = 1

type jsonReport struct {
	Schema int `json:"schema"`
	Report
	ExitCode int `json:"exit_code"`
}

// WriteJSON writes the report for agents and CI.
func WriteJSON(w io.Writer, r Report) error {
	if r.Regressions == nil {
		r.Regressions = []Finding{}
	}
	if r.Reviews == nil {
		r.Reviews = []Finding{}
	}
	if r.NewExceptions == nil {
		r.NewExceptions = []Marker{}
	}
	if r.Candidates == nil {
		r.Candidates = []Candidate{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(jsonReport{Schema: JSONSchemaVersion, Report: r, ExitCode: r.ExitCode()})
}

// WriteText writes the report for people.
func WriteText(w io.Writer, r Report) {
	for _, f := range r.Regressions {
		fmt.Fprintf(w, "✗ %-28s %s\n  %s\n  why: %s\n  fix: %s\n", f.Rule, f.Message, location(f.File, f.Line), f.Why, f.Fix)
	}
	for _, f := range r.Reviews {
		fmt.Fprintf(w, "? %-28s %s\n  %s\n  why: %s\n", f.Rule, f.Message, location(f.File, f.Line), f.Why)
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
	fmt.Fprintln(w, summary(r))
}

// location is "file:line", or just the file for findings about a whole file, such as configuration.
func location(file string, line int) string {
	if line == 0 {
		return file
	}
	return fmt.Sprintf("%s:%d", file, line)
}

func summary(r Report) string {
	switch r.ExitCode() {
	case ExitRegression:
		return fmt.Sprintf("%d regression(s). %d existing finding(s) unchanged (not blocking).", len(r.Regressions), r.Untouched)
	case ExitReview:
		return fmt.Sprintf("No regressions. %d item(s) need a person's review. %d existing finding(s) unchanged.",
			len(r.Reviews)+len(r.NewExceptions), r.Untouched)
	default:
		return fmt.Sprintf("No regressions. %d existing finding(s) unchanged (not blocking).", r.Untouched)
	}
}
