package tests

import (
	"strings"

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
	return engine.About{Finds: "an invariant in configuration that no active test guards",
		Why:  "the invariant is written down, but nothing fails when it is broken",
		Fix:  "write a test that checks it; built-in types are recognised from the test's calls, others need guards: in the test's doc comment",
		Keys: []string{"invariants"}}
}

func (r invariantUnguarded) Find(in engine.Input) ([]engine.Finding, error) {
	var out []engine.Finding
	for _, inv := range in.Config.Invariants {
		if guarded(inv, in.Facts.Tests) {
			continue
		}
		msg := "invariant " + inv.Key() + " is guarded by no active test"
		for _, t := range targets(inv) {
			if !hasMatch(in.Facts.Symbols, t) {
				msg += "; no symbol matches " + t
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
var shaped = map[string]string{
	"idempotent": "calls %s two or more times",
	"round-trip": "calls %s",
	"isolated":   "calls %s with two different users or tenants",
}

func fix(inv config.Invariant) string {
	declare := "add `guards: " + inv.Key() + "` to its doc comment"
	if inv.ID != "" {
		return "write a test that checks it and " + declare
	}
	all := strings.Join(targets(inv), " and ")
	if shape, ok := shaped[inv.Type]; ok {
		return "write an active test that " + strings.Replace(shape, "%s", all, 1)
	}
	return "write an active test that calls " + all + " and " + declare
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
