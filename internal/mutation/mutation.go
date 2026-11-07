// Package mutation is hadome mutate: it breaks the functions a change touched, one small change at a
// time, runs the configured test command, and reports breaks that no test notices and that base's
// tests did notice, or that base had no counterpart for.
//
// Design: docs/design/mutation.md
package mutation

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
)

// Mutator makes the mutants of functions of a version. The adapters implement it; making a mutant
// runs no code.
type Mutator interface {
	Mutants(v engine.Version, funcs []facts.Symbol) ([]facts.Mutant, error)
	// Unmutated returns the languages of the functions that the mutator cannot break.
	Unmutated(funcs []facts.Symbol) []string
}

// ErrNotConfigured is returned when the configuration has no mutation setting.
var ErrNotConfigured = errors.New("mutation is not configured: add mutation.test, the command that runs the tests " +
	"(hadome explain mutation)")

// Mutate runs hadome mutate.
type Mutate struct {
	Analyzer engine.Analyzer
	Mutator  Mutator
	Workdir  string    // where shared configuration layers are read from
	Progress io.Writer // one line per mutant tried; may be io.Discard
	// Lang is the language of the progress lines (en or ja). Empty means messages in head's
	// configuration, as for the report.
	Lang string
}

// Run tries the mutants of head, then base's counterparts of head's survivors, and compares the two
// through the engine. An error means nothing could be judged, and is exit code 2.
func (m *Mutate) Run(src engine.Source) (engine.Report, error) {
	bv, err := src.Base()
	if err != nil {
		return engine.Report{}, err
	}
	hv, err := src.Head()
	if err != nil {
		return engine.Report{}, err
	}
	headCfg, err := config.Load(hv.FS, m.Workdir)
	if err != nil {
		return engine.Report{}, fmt.Errorf("configuration of %s:\n%w", hv.Label, err)
	}
	if headCfg.Mutation == nil {
		return engine.Report{}, ErrNotConfigured
	}
	baseCfg, err := config.Load(bv.FS, m.Workdir)
	if err != nil {
		return engine.Report{}, fmt.Errorf("configuration of %s:\n%w", bv.Label, err)
	}
	baseFacts, err := m.Analyzer.Analyze(bv, config.ForBase(baseCfg, headCfg))
	if err != nil {
		return engine.Report{}, err
	}
	headFacts, err := m.Analyzer.Analyze(hv, headCfg)
	if err != nil {
		return engine.Report{}, err
	}
	vs := versions{base: bv, head: hv, baseFacts: baseFacts, headFacts: headFacts}
	lang := m.Lang
	if lang == "" {
		lang = headCfg.Messages
	}
	found, err := m.judge(vs, touched(baseFacts, headFacts, bv, hv), *headCfg.Mutation, lang)
	if err != nil {
		return engine.Report{}, err
	}
	e := &engine.Engine{Analyzer: m.Analyzer, Rules: []engine.Rule{survived{byVersion: found}}, Workdir: m.Workdir,
		Elsewhere: func(rule string) bool { return rule != RuleID }}
	return e.Check(src)
}

// versions are the two versions compared, each with its facts.
type versions struct {
	base, head           engine.Version
	baseFacts, headFacts *facts.Analysis
}

// judge tries the mutants and returns the findings of each version, by label. lang is the language
// of the progress lines.
func (m *Mutate) judge(vs versions, funcs []facts.Symbol, c config.Mutation, lang string) (map[string][]engine.Finding, error) {
	bv, hv := vs.base, vs.head
	found := map[string][]engine.Finding{}
	for _, lang := range m.Mutator.Unmutated(funcs) {
		found[hv.Label] = append(found[hv.Label], engine.Finding{Rule: RuleID, File: config.FileName,
			Anchor: "unmutated|" + lang, Review: true,
			Message: engine.T("hadome cannot break %s code yet, so the functions this change touched in %s were not tried", lang, lang),
			Why:     engine.T("untried code is not known to be guarded"), Fix: engine.T("a person checks the tests of those functions")})
	}
	mutants, err := m.Mutator.Mutants(hv, funcs)
	if err != nil || len(mutants) == 0 {
		return found, err
	}
	if len(mutants) > c.MaxMutants {
		found[hv.Label] = append(found[hv.Label], untried(mutants[c.MaxMutants:]))
		mutants = mutants[:c.MaxMutants]
	}
	head, err := m.runner(hv, c, 0)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(head.dir)
	callers := testsCalling(vs.headFacts)
	var survivors []facts.Mutant
	for i, mu := range mutants {
		out, err := head.try(mu, callers[mu.Func])
		if err != nil {
			return nil, err
		}
		fmt.Fprintln(m.Progress, progress(i+1, len(mutants), mu, out).In(lang))
		if out == Survived {
			survivors = append(survivors, mu)
			found[hv.Label] = append(found[hv.Label], finding(mu))
		}
	}
	baseSurvivors, err := m.inBase(bv, vs.baseFacts, survivors, c, head.Timeout)
	if err != nil {
		return nil, err
	}
	found[bv.Label] = baseSurvivors
	return found, nil
}

// inBase tries base's counterparts of head's survivors. A counterpart that survives in base too is a
// gap that was already there; the engine then leaves the head survivor alone.
func (m *Mutate) inBase(bv engine.Version, baseFacts *facts.Analysis, survivors []facts.Mutant, c config.Mutation, timeout time.Duration) ([]engine.Finding, error) {
	if len(survivors) == 0 {
		return nil, nil
	}
	wanted := map[string]bool{}
	for _, s := range survivors {
		wanted[s.Func] = true
	}
	var funcs []facts.Symbol
	for _, s := range functions(baseFacts) {
		if wanted[s.ID] {
			funcs = append(funcs, s)
		}
	}
	mutants, err := m.Mutator.Mutants(bv, funcs)
	if err != nil {
		return nil, err
	}
	byAnchor := map[string]facts.Mutant{}
	for _, mu := range mutants {
		byAnchor[mu.Anchor] = mu
	}
	var counterparts []facts.Mutant
	for _, s := range survivors {
		if mu, ok := byAnchor[s.Anchor]; ok {
			counterparts = append(counterparts, mu)
		}
	}
	if len(counterparts) == 0 {
		return nil, nil
	}
	base, err := m.runner(bv, c, timeout)
	if err != nil {
		return nil, fmt.Errorf("base: %w", err)
	}
	defer os.RemoveAll(base.dir)
	callers := testsCalling(baseFacts)
	var out []engine.Finding
	for _, mu := range counterparts {
		o, err := base.try(mu, callers[mu.Func])
		if err != nil {
			return nil, err
		}
		if o == Survived {
			out = append(out, finding(mu))
		}
	}
	return out, nil
}

// runner copies a version and runs its tests once with nothing broken. timeout, when not zero, is
// the limit already worked out for head.
func (m *Mutate) runner(v engine.Version, c config.Mutation, timeout time.Duration) (*Runner, error) {
	dir, err := copyVersion(v)
	if err != nil {
		return nil, err
	}
	r := &Runner{Test: c.Test, Sep: c.TestsSeparator, dir: dir}
	if c.Timeout != "" {
		if r.Timeout, err = time.ParseDuration(c.Timeout); err != nil {
			os.RemoveAll(dir)
			return nil, err
		}
	}
	took, err := r.clean()
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	switch {
	case r.Timeout != 0:
	case timeout != 0:
		r.Timeout = timeout
	default:
		r.Timeout = 3*took + time.Second // three times a clean run; the second covers a run of almost nothing
	}
	return r, nil
}

// testsCalling maps each function to the names of the active tests that call it.
func testsCalling(a *facts.Analysis) map[string][]string {
	out := map[string][]string{}
	for _, t := range a.Tests {
		if t.Skip.Skipped {
			continue
		}
		for _, c := range t.Calls {
			out[c.Target] = append(out[c.Target], t.Name)
		}
	}
	for k := range out {
		sort.Strings(out[k])
	}
	return out
}

func finding(m facts.Mutant) engine.Finding {
	msg := engine.T("%s: changing `%s` to `%s` does not fail any test", short(m.Func), m.Original, m.Replacement)
	if m.Operator == "remove a call statement" {
		msg = engine.T("%s: removing `%s` does not fail any test", short(m.Func), m.Original)
	}
	return engine.Finding{Rule: RuleID, File: m.Path, Line: m.Line, Anchor: m.Anchor,
		Message: msg,
		Why:     survived{}.About().Why, Fix: survived{}.About().Fix}
}

// untried reports the mutants beyond the limit, for a person to review: they were not tried, so
// they are known neither to be caught nor to survive.
func untried(rest []facts.Mutant) engine.Finding {
	funcs := map[string]bool{}
	for _, m := range rest {
		funcs[short(m.Func)] = true
	}
	names := make([]string, 0, len(funcs))
	for f := range funcs {
		names = append(names, f)
	}
	sort.Strings(names)
	return engine.Finding{Rule: RuleID, File: config.FileName, Anchor: "untried", Review: true,
		Message: engine.T("%d mutants beyond mutation.max-mutants were not tried, in %s", len(rest), strings.Join(names, ", ")),
		Why:     engine.T("untried code is not known to be guarded"),
		Fix:     engine.T("split the change, or have a person decide whether to raise mutation.max-mutants")}
}

// short drops the import path of a symbol ID: example.com/app/store.Append is store.Append.
func short(id string) string { return facts.ShortName(id) }

// progress is the line written after trying the n-th of total mutants: where the break is, what it
// broke, and whether a test noticed.
func progress(n, total int, mu facts.Mutant, o Outcome) engine.Text {
	result := engine.T("caught")
	if o == Survived {
		result = engine.T("survived")
	}
	return engine.T("mutate %d/%d %s:%d %s, %s: %s", n, total, mu.Path, mu.Line, short(mu.Func), engine.T(mu.Operator), result)
}
