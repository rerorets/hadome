// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

package engine

import (
	"fmt"
	"slices"
	"strings"

	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/facts"
)

// GateNotRunID is the engine's own report of a gate that head declares but base does not run the
// same way. Like LoosenedID it is not a rule, so no exception marker can silence it.
const GateNotRunID = "config/gate-not-run"

// splitGates divides head's gates into those base runs with the same command, which the check runs,
// and the rest, which it holds back. A gate's command is code from the change, and the change's own
// configuration cannot be what lets that code run: only a configuration already on the base branch,
// which a person merged, can.
func splitGates(base, head config.Config) (run, held []config.Gate) {
	trusted := map[string][]string{}
	for _, g := range base.Gates {
		trusted[g.ID] = g.Run
	}
	for _, g := range head.Gates {
		if cmd, ok := trusted[g.ID]; ok && slices.Equal(cmd, g.Run) {
			run = append(run, g)
		} else {
			held = append(held, g)
		}
	}
	return run, held
}

// heldReviews returns a review for every gate held back, saying it did not run.
func heldReviews(base, head config.Config, held []config.Gate) []Finding {
	inBase := map[string]bool{}
	for _, g := range base.Gates {
		inBase[g.ID] = true
	}
	var out []Finding
	for _, g := range held {
		msg := T("gate %s is new in this change and was not run: %s", g.ID, strings.Join(g.Run, " "))
		if inBase[g.ID] {
			msg = T("gate %s changes its command in this change and was not run: %s", g.ID, strings.Join(g.Run, " "))
		}
		out = append(out, Finding{
			Rule: GateNotRunID, File: config.FileName, Line: head.GateLine(g.ID), Anchor: g.ID, Review: true,
			Message: msg,
			Why:     T("a gate runs a command, and a command that only this change declares has not been reviewed by anyone, so hadome does not run it"),
			Fix:     T("a person reviews the command; once the change is on the base branch, the gate runs in every later check"),
		})
	}
	return out
}

// ruleSet is what a check runs: every rule, and the IDs of the gates it held back.
type ruleSet struct {
	all  []Rule
	held map[string]bool
}

// rules returns the built-in rules and those head's configuration adds, and the gates it held back
// because base does not run them with the same command. A held gate's command is never started, not
// even for its self-test.
func (e *Engine) rules(hv Version, baseCfg, headCfg config.Config) (ruleSet, []config.Gate, error) {
	if e.ConfigRules == nil {
		return ruleSet{all: e.Rules}, nil, nil
	}
	run, held := splitGates(baseCfg, headCfg)
	trusted := headCfg
	trusted.Gates = run
	more, err := e.ConfigRules(trusted)
	if err != nil {
		return ruleSet{}, nil, fmt.Errorf("configuration of %s:\n%w", hv.Label, err)
	}
	// Built-in rules have their cases checked before hadome is released. Configured rules live
	// only in the user's repository, so a check is the one place to test them before trusting
	// what they report.
	for _, r := range more {
		if st, ok := r.(SelfTester); ok {
			if err := st.SelfTest(); err != nil {
				return ruleSet{}, nil, fmt.Errorf("%s failed its own self-test, so the check does not use it:\n%w", r.ID(), err)
			}
		}
	}
	ids := map[string]bool{}
	for _, g := range held {
		ids[g.ID] = true
	}
	return ruleSet{all: append(append([]Rule{}, e.Rules...), more...), held: ids}, held, nil
}

// ownComments leaves out the comments whose marker names a rule reported elsewhere or held back.
func (e *Engine) ownComments(cs []facts.Comment, held map[string]bool) []facts.Comment {
	var out []facts.Comment
	for _, c := range cs {
		if m := markerPattern.FindStringSubmatch(c.Text); m != nil && (held[m[1]] || e.Elsewhere != nil && e.Elsewhere(m[1])) {
			continue
		}
		out = append(out, c)
	}
	return out
}
