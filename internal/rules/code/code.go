// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

// Package code holds the code/ rules: values that belong in configuration, errors that are dropped,
// and copies of functions, in product code.
//
// Design: docs/design/rules-code.md
package code

import (
	"fmt"
	"sort"
	"strings"

	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
)

// All returns the code/ rules.
func All() []engine.Rule {
	return []engine.Rule{configLiteral{}, swallowedError{}, duplicate{}, divergedCopy{}}
}

// place is one thing a counting rule found in a function: where, and what it looks like.
type place struct {
	line     int
	evidence string
}

// placeFindings reports each place as a finding anchored at its function, not at its line or its
// text. Findings of one function then share a fingerprint and the engine matches them by count: a
// function with more of them than on base is a regression, and editing the line of one that was
// there already is not. Each sits at its own line, so an exception marker covers only the place it
// is written at, never one added later in the same function.
func placeFindings(rule string, in engine.Input, about engine.About, what string,
	placesOf func(facts.Symbol) []place) []engine.Finding {
	names := in.Facts.Names()
	var out []engine.Finding
	for _, s := range in.Facts.Symbols {
		if !function(s) {
			continue
		}
		for _, p := range placesOf(s) {
			out = append(out, engine.Finding{
				Rule: rule, File: s.Path, Line: p.line, Anchor: s.ID,
				Message: engine.T("%s: %s: %s", names.Short(s.ID), engine.T(what), p.evidence),
				Why:     about.Why, Fix: about.Fix,
			})
		}
	}
	return out
}

func function(s facts.Symbol) bool {
	return s.Kind == facts.SymbolFunction || s.Kind == facts.SymbolMethod
}

// configLiteral is code/config-literal: values in a function's body that belong in configuration.
type configLiteral struct{}

func (configLiteral) ID() string { return "code/config-literal" }
func (configLiteral) Cases() []engine.Case {
	return append(configLiteralCases(), languageConfigLiteralCases...)
}

// languageConfigLiteralCases are cases in languages only a cgo build reads; see cases_cgo.go.
var languageConfigLiteralCases []engine.Case

func (configLiteral) About() engine.About {
	return engine.About{
		Finds: engine.T("values in product functions that belong in configuration: URLs with a host, IP addresses, hosts and ports, ports, durations built from literals, and literals passed to the names rules.code.config-literal.names lists; a function with more of them than on base is a regression"),
		Why:   engine.T("the value works where it is, and the next environment, or the next person who changes it, has to find every copy in the code"),
		Fix:   engine.T("read the value from where the repository reads its configuration; if it is the same everywhere and never changes, make it a named constant"),
		Keys:  []string{"rules.code.config-literal.paths", "rules.code.config-literal.names"},
	}
}

func (r configLiteral) Find(in engine.Input) ([]engine.Finding, error) {
	return placeFindings(r.ID(), in, r.About(), "a value that belongs in configuration", func(s facts.Symbol) []place {
		if config.MatchesAny(in.Config.ConfigPaths, s.Path) {
			return nil
		}
		var ps []place
		for _, l := range s.Literals {
			ps = append(ps, place{l.Line, l.Evidence})
		}
		return ps
	}), nil
}

// swallowedError is code/swallowed-error: places in a function's body that drop an error.
type swallowedError struct{}

func (swallowedError) ID() string { return "code/swallowed-error" }
func (swallowedError) Cases() []engine.Case {
	return append(swallowedErrorCases(), languageSwallowedErrorCases...)
}

var languageSwallowedErrorCases []engine.Case

func (swallowedError) About() engine.About {
	return engine.About{
		Finds: engine.T("places in product functions where an error is caught or returned and then dropped: an empty catch or except, a Go error assigned to _, a Rust .ok() whose value is unused; a function with more of them than on base is a regression"),
		Why:   engine.T("the failure disappears without a trace: the change passes its tests, and the problem shows up later somewhere else"),
		Fix:   engine.T("return the error, or wrap and re-throw it; when dropping it is the intent, write a reasoned exception"),
	}
}

func (r swallowedError) Find(in engine.Input) ([]engine.Finding, error) {
	return placeFindings(r.ID(), in, r.About(), "an error dropped", func(s facts.Symbol) []place {
		var ps []place
		for _, w := range s.Swallows {
			ps = append(ps, place{w.Line, w.Evidence})
		}
		return ps
	}), nil
}

// duplicate is code/duplicate: product functions with the same body. There is one finding per group
// of copies, and its Value is how many functions are in it. Which group is new is decided against
// base, not by the group's body or first function: a group is new when it has more functions than
// the base group it continues, the one with its body (so renaming or moving a copy changes nothing)
// or the one its functions were copies in (so changing every copy the same way changes nothing).
type duplicate struct{}

func (duplicate) ID() string           { return "code/duplicate" }
func (duplicate) Cases() []engine.Case { return append(duplicateCases(), languageDuplicateCases...) }

var languageDuplicateCases []engine.Case

func (duplicate) About() engine.About {
	return engine.About{
		Finds: engine.T("product functions with the same body, ignoring names, comments, whitespace and the names of parameters and local variables, and, in different files, calling the same known functions; a new copy is a regression"),
		Why:   engine.T("every fix has to be made in each copy, and a copy that is forgotten keeps the bug"),
		Fix:   engine.T("delete all but one and call the one that is left; how to share the code is the author's decision"),
		Keys:  []string{"rules.code.duplicate.min-tokens"},
	}
}

func (r duplicate) FindAgainst(base, head engine.Input) ([]engine.Finding, error) {
	// Groups and functions are known by their build as well, so that the copies of one operating
	// system's file are not taken for another's.
	baseBuilds, headBuilds := buildsOf(base.Facts), buildsOf(head.Facts)
	bySize := map[string]int{}  // base groups' sizes by build and body
	inGroup := map[string]int{} // which base group each function was in, by build and ID
	for i, g := range groups(base) {
		k := baseBuilds[g[0].Path] + "\x00" + g[0].Body
		bySize[k] = max(bySize[k], len(g))
		for _, s := range g {
			inGroup[baseBuilds[s.Path]+"\x00"+s.ID] = i + 1
		}
	}
	names := head.Facts.Names()
	var out []engine.Finding
	for _, g := range groups(head) {
		was := bySize[headBuilds[g[0].Path]+"\x00"+g[0].Body]
		together := map[int]int{} // how many of the group were in each base group
		for _, s := range g {
			if i := inGroup[headBuilds[s.Path]+"\x00"+s.ID]; i > 0 {
				together[i]++
			}
		}
		for _, n := range together {
			was = max(was, n)
		}
		if len(g) <= was {
			continue
		}
		var where []string
		for _, s := range g {
			where = append(where, fmt.Sprintf("%s (%s:%d)", names.Short(s.ID), s.Path, s.Line))
		}
		out = append(out, engine.Finding{
			Rule: r.ID(), File: g[0].Path, Line: g[0].Line, Anchor: g[0].Body, Value: len(g),
			Message: engine.T("%d functions have the same body: %s", len(g), strings.Join(where, ", ")),
			Why:     r.About().Why, Fix: r.About().Fix,
		})
	}
	return out, nil
}

// files are what pairing functions needs of each file: Go files' build conditions.
type files map[string]string

func buildsOf(a *facts.Analysis) files {
	out := files{}
	for _, f := range a.Files {
		if f.Language == "go" {
			out[f.Path] = f.Build
		}
	}
	return out
}

// copied reports whether two functions with the same body are copies of each other. In one file
// the same names mean the same things. In different files they may not (each file imports its own
// round), so the two must also call the same known functions and use the same units, and neither may
// call a value of unknown type. Go files that are never built together (one per operating system)
// hold versions of one function, not copies.
func (fs files) copied(a, b facts.Symbol) bool {
	if a.Path == b.Path {
		return true
	}
	if !facts.BuildTogether(fs[a.Path], fs[b.Path]) {
		return false
	}
	ka := known(a)
	return ka != "\x01" && ka == known(b)
}

// groups returns the version's groups of copies, each in file and line order, the groups in the
// order of their first function. Functions too small to compare are left out.
func groups(in engine.Input) [][]facts.Symbol {
	fs := buildsOf(in.Facts)
	same := map[string][]facts.Symbol{}
	var bodies []string
	for _, s := range in.Facts.Symbols {
		if !function(s) || s.Body == "" || s.BodyTokens < in.Config.MinTokens {
			continue
		}
		if same[s.Body] == nil {
			bodies = append(bodies, s.Body)
		}
		same[s.Body] = append(same[s.Body], s)
	}
	var out [][]facts.Symbol
	for _, b := range bodies {
		if ss := same[b]; len(ss) > 1 {
			sort.SliceStable(ss, func(i, j int) bool { return before(ss[i], ss[j]) })
			out = append(out, fs.join(ss)...)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return before(out[i][0], out[j][0]) })
	return out
}

func before(a, b facts.Symbol) bool {
	return a.Path < b.Path || a.Path == b.Path && a.Line < b.Line
}

// join splits functions with the same body into the groups of two or more that are copies of one
// another, joined as far as any pair links them.
func (fs files) join(ss []facts.Symbol) [][]facts.Symbol {
	parent := make([]int, len(ss))
	for i := range parent {
		parent[i] = i
	}
	root := func(i int) int {
		for parent[i] != i {
			i = parent[i]
		}
		return i
	}
	for i := range ss {
		for j := i + 1; j < len(ss); j++ {
			if fs.copied(ss[i], ss[j]) {
				parent[root(j)] = root(i)
			}
		}
	}
	byRoot := map[int][]facts.Symbol{}
	var order []int
	for i, s := range ss {
		r := root(i)
		if byRoot[r] == nil {
			order = append(order, r)
		}
		byRoot[r] = append(byRoot[r], s)
	}
	var out [][]facts.Symbol
	for _, r := range order {
		if len(byRoot[r]) > 1 {
			out = append(out, byRoot[r])
		}
	}
	return out
}

// known is what a function calls and uses of the repository, or "\x01" when it calls a value of
// unknown type, which may reach anything.
func known(s facts.Symbol) string {
	if s.Unfollowed > 0 {
		return "\x01"
	}
	calls := append([]string(nil), s.Calls...)
	sort.Strings(calls)
	return strings.Join(calls, "\x00") + "\x02" + strings.Join(s.Units, "\x00")
}

// divergedCopy is code/diverged-copy: of functions that were copies on base, the ones the change
// left with the base body while it changed a copy of them.
type divergedCopy struct{}

func (divergedCopy) ID() string { return "code/diverged-copy" }
func (divergedCopy) Cases() []engine.Case {
	return append(divergedCopyCases(), languageDivergedCopyCases...)
}

var languageDivergedCopyCases []engine.Case

func (divergedCopy) About() engine.About {
	return engine.About{
		Finds: engine.T("functions that had the same body as another on base and still have it, when the change edits that other one: the copy may need the same fix"),
		Why:   engine.T("a fix to one copy may be needed in the other, and the copy is not in the diff, so review does not see it either"),
		Fix:   engine.T("make the same change in the copy, or delete one and call the other; for copies meant to change separately, write a reasoned exception"),
		Keys:  []string{"rules.code.duplicate.min-tokens"},
	}
}

// FindAgainst matches each function of base's groups with head's function of the same ID. When one
// ID names several functions on either version (versions under build conditions, overloads), only
// the one in the same file is taken, if there is exactly one; otherwise the function is left out,
// since which one it became cannot be told. A function left with the base body is reported only next to a copy of its own that
// changed: a group joined through a third function may hold two functions that are no copies of
// each other.
func (r divergedCopy) FindAgainst(base, head engine.Input) ([]engine.Finding, error) {
	now, was := map[string][]facts.Symbol{}, map[string]int{}
	for _, s := range head.Facts.Symbols {
		if function(s) {
			now[s.ID] = append(now[s.ID], s)
		}
	}
	for _, s := range base.Facts.Symbols {
		if function(s) {
			was[s.ID]++
		}
	}
	match := func(s facts.Symbol) (facts.Symbol, bool) {
		hs := now[s.ID]
		var same []facts.Symbol
		for _, h := range hs {
			if h.Path == s.Path {
				same = append(same, h)
			}
		}
		switch {
		case len(same) == 1:
			return same[0], true
		case len(same) == 0 && len(hs) == 1 && was[s.ID] == 1:
			return hs[0], true
		}
		return facts.Symbol{}, false
	}
	fs := buildsOf(base.Facts)
	names := head.Facts.Names()
	var out []engine.Finding
	for _, group := range groups(base) {
		var changed, left []facts.Symbol
		var leftNow []facts.Symbol
		for _, s := range group {
			h, ok := match(s)
			switch {
			case !ok:
			case h.Body == s.Body:
				left, leftNow = append(left, s), append(leftNow, h)
			default:
				changed = append(changed, s)
			}
		}
		for i, s := range left {
			for _, c := range changed {
				if fs.copied(s, c) {
					h := leftNow[i]
					out = append(out, engine.Finding{
						Rule: r.ID(), File: h.Path, Line: h.Line, Anchor: h.ID,
						Message: engine.T("%s changed; %s still has the body it had", names.Short(c.ID), names.Short(h.ID)),
						Why:     r.About().Why, Fix: r.About().Fix,
					})
					break
				}
			}
		}
	}
	return out, nil
}
