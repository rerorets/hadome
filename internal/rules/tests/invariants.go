package tests

import (
	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
)

// invariantUnguarded is tests/invariant-unguarded: an invariant in configuration that no active
// test guards. Its anchor is the invariant, so losing the last guard is a new finding in head and
// moving a guard to another test changes nothing.
type invariantUnguarded struct{}

func (invariantUnguarded) ID() string           { return "tests/invariant-unguarded" }
func (invariantUnguarded) Cases() []engine.Case { return invariantCases() }
func (invariantUnguarded) About() engine.About {
	return engine.About{Finds: engine.T("an invariant in configuration that no active test guards"),
		Why:  engine.T("the invariant is written down, but nothing fails when it is broken"),
		Fix:  engine.T("write a test that checks it; built-in types are recognised from the test's calls, others need guards: in the test's doc comment"),
		Keys: []string{"invariants"}}
}

func (r invariantUnguarded) Find(in engine.Input) ([]engine.Finding, error) {
	var out []engine.Finding
	for _, inv := range in.Config.Invariants {
		if guarded(inv, in.Facts.Tests) {
			continue
		}
		msg := engine.T("invariant %s is guarded by no active test", inv.Key())
		for _, t := range targets(inv) {
			if !hasMatch(in.Facts.Symbols, t) {
				msg = engine.T("%s; no symbol matches %s", msg, t)
			}
		}
		out = append(out, engine.Finding{
			Rule: r.ID(), File: config.FileName, Anchor: inv.Key(), Message: msg,
			Why: r.About().Why,
			Fix: fix(inv),
		})
	}
	return out, nil
}

// shaped are the built-in types whose guarding tests are recognised by the shape of their calls.
// The value is the fix: a format whose one verb is the targets.
var shaped = map[string]string{
	"idempotent": "write an active test that calls %s two or more times",
	"round-trip": "write an active test that calls %s",
	"isolated":   "write an active test that calls %s with two different users or tenants",
}

func fix(inv config.Invariant) engine.Text {
	if inv.ID != "" {
		return engine.T("write a test that checks it and add `guards: %s` to its doc comment", inv.Key())
	}
	ts := targets(inv)
	all := engine.Verbatim(ts[0])
	for _, t := range ts[1:] {
		all = engine.T("%s and %s", all, t)
	}
	if shape, ok := shaped[inv.Type]; ok {
		return engine.T(shape, all)
	}
	return engine.T("write an active test that calls %s and add `guards: %s` to its doc comment", all, inv.Key())
}

func targets(inv config.Invariant) []string {
	if inv.Target != "" {
		return []string{inv.Target}
	}
	return inv.Targets
}

func guarded(inv config.Invariant, tests []facts.Test) bool {
	for _, t := range tests {
		if !t.Skip.Skipped && guards(inv, t) {
			return true
		}
	}
	return false
}

// guards applies docs/design/rules-tests.md, "Which tests guard an invariant".
func guards(inv config.Invariant, t facts.Test) bool {
	if inv.ID != "" {
		return declares(t, inv.Key())
	}
	for _, target := range targets(inv) {
		c, ok := callTo(t, target)
		switch {
		case !ok:
			return false
		case inv.Type == "idempotent" && c.Times < 2:
			return false
		case inv.Type == "isolated" && c.Scopes < 2:
			return false
		}
	}
	if _, ok := shaped[inv.Type]; ok {
		return true
	}
	return declares(t, inv.Key())
}

func declares(t facts.Test, key string) bool {
	for _, v := range t.Declared["guards"] {
		if v == key {
			return true
		}
	}
	return false
}

// callTo merges the test's calls to every symbol the configured target names.
func callTo(t facts.Test, target string) (facts.Call, bool) {
	var merged facts.Call
	found := false
	for _, c := range t.Calls {
		if config.TargetNames(target, c.Target) {
			found = true
			merged.Times += c.Times
			merged.Scopes = max(merged.Scopes, c.Scopes)
		}
	}
	return merged, found
}

func hasMatch(symbols []facts.Symbol, target string) bool {
	for _, s := range symbols {
		if config.TargetNames(target, s.ID) {
			return true
		}
	}
	return false
}
