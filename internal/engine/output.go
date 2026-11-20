// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

package engine

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"unicode/utf8"

	"github.com/rerorets/hadome/internal/config"
)

// JSONSchemaVersion is the version of the JSON report. It changes only by adding fields.
const JSONSchemaVersion = 1

type jsonReport struct {
	Schema        int             `json:"schema"`
	Regressions   []jsonFinding   `json:"regressions"`
	Reviews       []jsonFinding   `json:"reviews"`
	NewExceptions []Marker        `json:"new_exceptions"`
	Candidates    []Candidate     `json:"candidates"`
	Unfollowed    []Unfollowed    `json:"unfollowed"`
	Introduced    []config.Change `json:"introduced"`
	Untouched     int             `json:"untouched"`
	ExitCode      int             `json:"exit_code"`
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
		NewExceptions: r.NewExceptions, Candidates: r.Candidates, Unfollowed: r.Unfollowed, Untouched: r.Untouched,
		ExitCode: r.ExitCode()}
	if out.NewExceptions == nil {
		out.NewExceptions = []Marker{}
	}
	if out.Candidates == nil {
		out.Candidates = []Candidate{}
	}
	if out.Unfollowed == nil {
		out.Unfollowed = []Unfollowed{}
	}
	if out.Introduced = r.Introduced; out.Introduced == nil {
		out.Introduced = []config.Change{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// The sentences that introduce a list in the text output. They explain to the reader, unlike the
// labels (why:, fix:), which are spelled like the JSON keys and stay English.
var (
	candidatesHeading = T("Invariant candidates this change adds (information only; propose them to a person, `hadome suggest` prints them as configuration):")
	unfollowedHeading = T("Calls on values of unknown type grew (information only; a type annotation makes them followable):")
	introducedHeading = T("Settings this change introduces with .hadome.yml (information only; base has no configuration, so nothing counts as loosening):")
)

// WriteText writes the report for people, with the findings' wording and the summary in lang.
// Every value that is not hadome's own wording goes through plain.
func WriteText(w io.Writer, r Report, lang string) {
	for _, f := range r.Regressions {
		fmt.Fprintf(w, "✗ %-28s %s\n  %s\n  why: %s\n  fix: %s\n", plain(f.Rule), plain(f.Message.In(lang)),
			location(f.File, f.Line), plain(f.Why.In(lang)), plain(f.Fix.In(lang)))
	}
	for _, f := range r.Reviews {
		fmt.Fprintf(w, "? %-28s %s\n  %s\n  why: %s\n", plain(f.Rule), plain(f.Message.In(lang)),
			location(f.File, f.Line), plain(f.Why.In(lang)))
	}
	for _, m := range r.NewExceptions {
		fmt.Fprintf(w, "? %-28s new exception: %s\n  %s:%d\n", plain(m.Rule), plain(m.Reason), plain(m.File), m.Line)
	}
	if len(r.Candidates) > 0 {
		fmt.Fprintln(w, candidatesHeading.In(lang))
		for _, c := range r.Candidates {
			fmt.Fprintf(w, "  %s  %s — %s\n", plain(c.Key()), location(c.File, c.Line), plain(c.Why))
		}
	}
	if len(r.Unfollowed) > 0 {
		fmt.Fprintln(w, unfollowedHeading.In(lang))
		for _, u := range r.Unfollowed {
			fmt.Fprintf(w, "  %s: %d → %d\n", plain(u.Unit), u.Base, u.Head)
		}
	}
	if len(r.Introduced) > 0 {
		fmt.Fprintln(w, introducedHeading.In(lang))
		for _, c := range r.Introduced {
			fmt.Fprintf(w, "  %s  %s  %s\n", plain(c.Key), location(c.File, c.Line), changed(c))
		}
	}
	fmt.Fprintln(w, summary(r).In(lang))
}

// plain is a value as the text output writes it. A value with a character that is not printable (a
// control character such as a newline or ESC, a line separator, a bidirectional override) or with
// bytes that are not UTF-8 is written quoted and escaped like a Go string literal; any other value
// is written as it is. File names, exception reasons and gates' messages come from the repository:
// a newline in one would start a line of its own, which a CI runner can read as a command (GitHub
// Actions runs a line that starts with "::"), and an escape would reach the terminal.
func plain(s string) string {
	if !utf8.ValidString(s) {
		return strconv.Quote(s)
	}
	for _, r := range s {
		if !strconv.IsPrint(r) {
			return strconv.Quote(s)
		}
	}
	return s
}

// changed is a change's values: "removed → added", or only the side it has.
func changed(c config.Change) string {
	switch {
	case c.Removed == "":
		return plain(c.Added)
	case c.Added == "":
		return "removed " + plain(c.Removed)
	}
	return plain(c.Removed) + " → " + plain(c.Added)
}

// location is "file:line", or just the file for findings about a whole file, such as configuration.
func location(file string, line int) string {
	if line == 0 {
		return plain(file)
	}
	return fmt.Sprintf("%s:%d", plain(file), line)
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
