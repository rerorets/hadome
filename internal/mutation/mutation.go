// Package mutation is hadome mutate: it breaks the functions a change touched, one small change at a
// time, runs the configured test command, and reports breaks that no test notices and that base's
// tests did notice, or that base had no counterpart for.
//
// Design: docs/design/mutation.md
package mutation

import (
	"errors"
	"fmt"
	"go/build"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

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
	// Trees is the working directory the versions are copied into, kept between runs (TreesFor).
	Trees string
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
	baseFacts, headFacts, err := analyzeBoth(m.Analyzer, bv, config.ForBase(baseCfg, headCfg), hv, headCfg)
	if err != nil {
		return engine.Report{}, err
	}
	if hasPackages(headCfg.Mutation.Test) && !hasGo(headFacts) {
		return engine.Report{}, fmt.Errorf("configuration of %s: mutation.test holds %s, which only Go tests fill, and %s has no Go file; "+
			"narrow with {tests} instead (hadome explain mutation.test)", hv.Label, packagesHole, hv.Label)
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

// analyzeBoth reads both versions, in one call when the analyzer can.
func analyzeBoth(an engine.Analyzer, bv engine.Version, bc config.Config, hv engine.Version, hc config.Config) (*facts.Analysis, *facts.Analysis, error) {
	if pa, ok := an.(engine.PairAnalyzer); ok {
		return pa.AnalyzePair(bv, bc, hv, hc)
	}
	b, err := an.Analyze(bv, bc)
	if err != nil {
		return nil, nil, err
	}
	h, err := an.Analyze(hv, hc)
	return b, h, err
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
	shown := vs.headFacts.Names()
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
	mutants, outside := splitBuilt(buildContext(c.Test), hv, mutants)
	found[hv.Label] = append(found[hv.Label], notBuilt(outside)...)
	if len(mutants) > c.MaxMutants {
		found[hv.Label] = append(found[hv.Label], untried(mutants[c.MaxMutants:], shown))
		mutants = mutants[:c.MaxMutants]
	}
	total := len(outside) + len(mutants)
	for i, mu := range outside {
		fmt.Fprintln(m.Progress, progress(i+1, total, mu, Unbuilt, shown).In(lang))
	}
	if len(mutants) == 0 {
		return found, nil
	}
	release, err := m.hold()
	if err != nil {
		return nil, err
	}
	defer release()
	head, err := m.runner("head", hv, marks(vs.headFacts, mutants), c, lang)
	if err != nil {
		return nil, err
	}
	pick := picker(vs.headFacts)
	var survivors []facts.Mutant
	for i, mu := range mutants {
		out, err := head.try(mu, pick(mu.Func))
		if err != nil {
			return nil, err
		}
		fmt.Fprintln(m.Progress, progress(len(outside)+i+1, total, mu, out, shown).In(lang))
		if out == Survived {
			survivors = append(survivors, mu)
			found[hv.Label] = append(found[hv.Label], finding(mu, shown))
		}
	}
	baseSurvivors, err := m.inBase(bv, vs.baseFacts, survivors, c, lang)
	if err != nil {
		return nil, err
	}
	found[bv.Label] = baseSurvivors
	return found, nil
}

// inBase tries base's counterparts of head's survivors. A counterpart that survives in base too is a
// gap that was already there; the engine then leaves the head survivor alone.
func (m *Mutate) inBase(bv engine.Version, baseFacts *facts.Analysis, survivors []facts.Mutant, c config.Mutation, lang string) ([]engine.Finding, error) {
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
	base, err := m.runner("base", bv, marks(baseFacts, counterparts), c, lang)
	if err != nil {
		return nil, fmt.Errorf("base: %w", err)
	}
	pick, shown := picker(baseFacts), baseFacts.Names()
	var out []engine.Finding
	for _, mu := range counterparts {
		o, err := base.try(mu, pick(mu.Func))
		if err != nil {
			return nil, err
		}
		if o == Survived {
			out = append(out, finding(mu, shown))
		}
	}
	return out, nil
}

// runner copies a version into its directory under Trees, named by its role (head or base), and runs its tests once with nothing
// broken. The files holding mutants get, in each run before breaking, a comment of their language.
func (m *Mutate) runner(role string, v engine.Version, marked map[string]string, c config.Mutation, lang string) (*Runner, error) {
	dir := filepath.Join(m.Trees, role)
	if err := writeTree(v, dir); err != nil {
		return nil, err
	}
	skipped := func(cmd []string) {
		fmt.Fprintln(m.Progress, engine.T("mutate: %s fails with nothing broken, so it judges no mutant; they go to the next command",
			strings.Join(cmd, " ")).In(lang))
	}
	r, err := newRunner(dir, c, marked, skipped)
	if err != nil {
		return nil, err
	}
	if err := r.clean(); err != nil {
		return nil, err
	}
	return r, nil
}

// marks gives each file holding a mutant the line comment of its language.
func marks(a *facts.Analysis, mutants []facts.Mutant) map[string]string {
	languages := map[string]string{}
	for _, f := range a.Files {
		languages[f.Path] = f.Language
	}
	out := map[string]string{}
	for _, mu := range mutants {
		out[mu.Path] = lineComment(languages[mu.Path])
	}
	return out
}

// hold takes the lock of the working directory, so that no other hadome mutate copies into it.
func (m *Mutate) hold() (release func(), err error) {
	if m.Trees == "" {
		return nil, errors.New("no working directory to copy the versions into")
	}
	if err := os.MkdirAll(m.Trees, 0o755); err != nil {
		return nil, err
	}
	return lock(filepath.Join(m.Trees, "lock"))
}

// callers maps each function to the active tests that call it.
type callers map[string][]facts.Test

// testsCalling maps each function to the active tests that call it.
func testsCalling(a *facts.Analysis) callers {
	out := callers{}
	for _, t := range a.Tests {
		if t.Skip.Skipped {
			continue
		}
		for _, c := range t.Calls {
			out[c.Target] = append(out[c.Target], t)
		}
	}
	for k := range out {
		sort.SliceStable(out[k], func(i, j int) bool { return out[k][i].Name < out[k][j].Name })
	}
	return out
}

// picker returns what picks, for a function of the version, the tests its mutants' stages run.
func picker(a *facts.Analysis) func(fn string) picked {
	calling, r := testsCalling(a), reaching(a)
	return func(fn string) picked {
		return picked{calling: calling[fn], reaching: r.of(fn), all: a.Tests}
	}
}

// hasGo reports whether a version has a Go file.
func hasGo(a *facts.Analysis) bool {
	for _, f := range a.Files {
		if f.Language == "go" {
			return true
		}
	}
	return false
}

func finding(m facts.Mutant, names facts.Names) engine.Finding {
	msg := engine.T("%s: changing `%s` to `%s` does not fail any test", names.Short(m.Func), m.Original, m.Replacement)
	// A mutant that replaces code with nothing, such as one that drops a `!`, removes it.
	if m.Operator == "remove a call statement" || m.Replacement == "" {
		msg = engine.T("%s: removing `%s` does not fail any test", names.Short(m.Func), m.Original)
	}
	return engine.Finding{Rule: RuleID, File: m.Path, Line: m.Line, Anchor: m.Anchor,
		Message: msg,
		Why:     survived{}.About().Why, Fix: survived{}.About().Fix}
}

// untried reports the mutants beyond the limit, for a person to review: they were not tried, so
// they are known neither to be caught nor to survive.
func untried(rest []facts.Mutant, names facts.Names) engine.Finding {
	funcs := map[string]struct{}{}
	for _, m := range rest {
		funcs[names.Short(m.Func)] = struct{}{}
	}
	shown := make([]string, 0, len(funcs))
	for f := range funcs {
		shown = append(shown, f)
	}
	sort.Strings(shown)
	return engine.Finding{Rule: RuleID, File: config.FileName, Anchor: "untried", Review: true,
		Message: engine.T("%d mutants beyond mutation.max-mutants were not tried, in %s", len(rest), strings.Join(shown, ", ")),
		Why:     engine.T("untried code is not known to be guarded"),
		Fix:     engine.T("split the change, or have a person decide whether to raise mutation.max-mutants")}
}

// splitBuilt separates the mutants in files the test command's build compiles from those in files it
// leaves out. Running the tests against the latter proves nothing: they pass whatever the break.
func splitBuilt(ctx build.Context, v engine.Version, mutants []facts.Mutant) (built, outside []facts.Mutant) {
	known := map[string]bool{}
	for _, mu := range mutants {
		out, ok := known[mu.Path]
		if !ok {
			out = unbuilt(ctx, v.FS, mu.Path)
			known[mu.Path] = out
		}
		if out {
			outside = append(outside, mu)
		} else {
			built = append(built, mu)
		}
	}
	return built, outside
}

// notBuilt reports, for a person to review, one finding per file whose mutants were not tried
// because the build that runs the tests does not compile the file. They are neither caught nor
// survivors.
func notBuilt(outside []facts.Mutant) []engine.Finding {
	var out []engine.Finding
	byFile := map[string][]facts.Mutant{}
	var files []string
	for _, mu := range outside {
		if byFile[mu.Path] == nil {
			files = append(files, mu.Path)
		}
		byFile[mu.Path] = append(byFile[mu.Path], mu)
	}
	for _, f := range files {
		ms := byFile[f]
		out = append(out, engine.Finding{Rule: RuleID, File: f, Line: ms[0].Line, Anchor: "unbuilt|" + f, Review: true,
			Message: engine.T("%d mutants in %s were not tried: the build that runs the tests does not compile this file", len(ms), f),
			Why:     engine.T("untried code is not known to be guarded"),
			Fix:     engine.T("a person checks the tests of this file, or runs mutate where the build compiles it, such as with CGO_ENABLED=0 in mutation.test")})
	}
	return out
}

// short drops the import path of a symbol ID: example.com/app/store.Append is store.Append.
// progress is the line written after trying the n-th of total mutants: where the break is, what it
// broke, and whether a test noticed.
func progress(n, total int, mu facts.Mutant, o Outcome, names facts.Names) engine.Text {
	result := engine.T("caught")
	switch o {
	case Survived:
		result = engine.T("survived")
	case Unbuilt:
		result = engine.T("not built")
	}
	return engine.T("mutate %d/%d %s:%d %s, %s: %s", n, total, mu.Path, mu.Line, names.Short(mu.Func), engine.T(mu.Operator), result)
}
