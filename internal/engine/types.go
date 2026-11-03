// Package engine compares two versions of a repository rule by rule and keeps only what got worse.
// It knows no language and no rule's logic: adapters provide facts, rules turn facts into findings
// or guarded items, and the engine matches them across versions.
//
// Design: docs/design/engine.md
package engine

import (
	"io/fs"
	"strings"

	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/facts"
)

// Version is one read-only version of a repository.
type Version struct {
	Label string
	FS    fs.FS
	// Root is the version's directory on disk, when it has one. Empty for versions read from history.
	Root string
}

// Source provides the two versions to compare.
type Source interface {
	Base() (Version, error)
	Head() (Version, error)
	// Renames maps a path in base to its path in head, for files that were renamed.
	Renames() (map[string]string, error)
}

// Analyzer turns a version into facts. In practice it is the set of language adapters. The
// configuration carries entries added to the adapters' tables.
type Analyzer interface {
	Analyze(v Version, c config.Config) (*facts.Analysis, error)
}

// Input is what a rule sees of one version: the adapters' facts and that version's configuration.
type Input struct {
	Facts  *facts.Analysis
	Config config.Config
	// Version is the version itself, for rules that need its files rather than facts about them.
	Version Version
}

// Rule is anything the engine can run. Every rule is either a FindingRule or a MeasureRule.
type Rule interface {
	ID() string
	// Cases are the rule's self-test cases. A rule without cases is not loaded.
	Cases() []Case
}

// SelfTester is a rule that tests itself instead of providing cases, such as a product-specific
// gate, whose cases live with its command.
type SelfTester interface {
	SelfTest() error
}

// FindingRule finds violations in one version. A finding in head with no match in base is a regression.
// An error means the rule could not run, and a check that could not run never passes.
type FindingRule interface {
	Rule
	Find(in Input) ([]Finding, error)
}

// MeasureRule reports what is guarded in one version. An item in base missing from head is a
// regression unless RemovedLegitimately says otherwise.
type MeasureRule interface {
	Rule
	Measure(in Input) ([]Item, error)
	RemovedLegitimately(it Item, head Input) (bool, error)
}

// Finding is one violation. Anchor must survive unrelated edits; it never contains a line number.
type Finding struct {
	Rule    string `json:"rule"`
	File    string `json:"file"`
	Line    int    `json:"line"`
	Anchor  string `json:"-"`
	Message string `json:"message"`
	Why     string `json:"why"`
	Fix     string `json:"fix"`
	// Review marks findings that need a person's review instead of failing the check (exit code 3).
	Review bool `json:"-"`
	// Value is what the rule measured, for findings about an amount (lines, parameters). A finding
	// that matches base but has a larger Value is a regression: the violation got worse.
	Value int `json:"value,omitempty"`
}

// Item is one guarded thing reported by a measure rule.
type Item struct {
	Rule   string
	Key    string
	File   string
	Line   int
	Detail string
	Why    string
	Fix    string
	// Review marks items whose disappearance needs a person's review instead of failing the check.
	Review bool
}

// Marker is one valid exception written in a comment.
type Marker struct {
	Rule   string `json:"rule"`
	Reason string `json:"reason"`
	File   string `json:"file"`
	Line   int    `json:"line"`
}

// Case is one self-test case: two small trees and the rule IDs expected to regress or need review.
type Case struct {
	Name       string
	Base       fs.FS
	Head       fs.FS
	WantFail   []string
	WantReview []string
}

// Candidate is a place that usually carries an invariant nobody has configured. It is information
// only and never changes the exit code.
type Candidate struct {
	Type    string   `json:"type"`
	Targets []string `json:"targets"`
	File    string   `json:"file"`
	Line    int      `json:"line"`
	Why     string   `json:"why"`
}

// Key identifies the candidate across versions.
func (c Candidate) Key() string { return c.Type + " " + strings.Join(c.Targets, ",") }

// Report is the result of a check. All slices are sorted.
type Report struct {
	Regressions   []Finding `json:"regressions"`
	Reviews       []Finding `json:"reviews"`
	NewExceptions []Marker  `json:"new_exceptions"`
	// Candidates are the invariant candidates this change adds.
	Candidates []Candidate `json:"candidates"`
	Untouched  int         `json:"untouched"`
}

// Exit codes of a check.
const (
	ExitOK         = 0
	ExitRegression = 1
	ExitConfig     = 2
	ExitReview     = 3
)

// ExitCode returns the exit code for the report. Configuration errors are returned as errors
// by Check and never reach a report.
func (r Report) ExitCode() int {
	switch {
	case len(r.Regressions) > 0:
		return ExitRegression
	case len(r.Reviews) > 0 || len(r.NewExceptions) > 0:
		return ExitReview
	default:
		return ExitOK
	}
}
