// Package adapter is the analyzer the engine uses: it gives each file of a version to the language
// that claims it and merges what the languages report.
//
// Design: docs/design/adapters.md, docs/design/adapters-treesitter.md
package adapter

import (
	"fmt"
	"slices"
	"sort"

	"github.com/rerorets/hadome/internal/adapter/ansible"
	"github.com/rerorets/hadome/internal/adapter/golang"
	"github.com/rerorets/hadome/internal/adapter/reuse"
	"github.com/rerorets/hadome/internal/adapter/sql"
	"github.com/rerorets/hadome/internal/adapter/terraform"
	"github.com/rerorets/hadome/internal/adapter/treesitter"
	"github.com/rerorets/hadome/internal/adapter/walk"
	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
)

// Language is one language's adapter.
type Language interface {
	// Name is the language's name in configuration (config.Languages).
	Name() string
	Claims(path string) bool
	// Analyze reads the given files, which it claimed, and may read any other file of the version
	// (such as go.mod or tsconfig.json) to understand them. It reads the version only through r.
	Analyze(r *reuse.Run, c config.Analysis, paths []string) (*facts.Analysis, error)
}

// configuredClaimer is a language that also claims files the configuration names, such as the
// playbooks of files.ansible.
type configuredClaimer interface {
	ClaimsConfigured(c config.Analysis, path string) bool
}

// linker is a language that reads what the other languages found, after every language has been
// read: the SQL adapter reads the strings tests hold as SQL.
type linker interface {
	// ReadsTexts reports whether Link reads the tests' strings (facts.Test.Texts) of a version in
	// which the linker claimed paths. It is asked before any language reads the version.
	ReadsTexts(c config.Analysis, paths []string) bool
	Link(r *reuse.Run, c config.Analysis, a *facts.Analysis) error
}

// textSource is a language that hands over the strings its tests hold. AnalyzeWithoutTexts is
// Analyze collecting none, with every other fact unchanged: collecting them examines every node of
// every test body, which is wasted on a version no linker reads them in.
type textSource interface {
	AnalyzeWithoutTexts(r *reuse.Run, c config.Analysis, paths []string) (*facts.Analysis, error)
}

// claims reports whether the language claims the file under this configuration.
func claims(l Language, c config.Analysis, p string) bool {
	if cc, ok := l.(configuredClaimer); ok {
		return cc.ClaimsConfigured(c, p)
	}
	return l.Claims(p)
}

// Analyzer reads a version with every language it has.
type Analyzer struct {
	Languages []Language
	// NoReuse makes every stage run on every call: the analysis takes the same path, but no
	// remembered result is ever returned. The facts are the same either way.
	NoReuse bool
}

// New returns the analyzer with every language this build of hadome has.
func New() Analyzer {
	langs := []Language{golang.Adapter{}}
	for _, l := range treesitter.Languages() { // a different type with and without cgo
		langs = append(langs, l)
	}
	langs = append(langs, terraform.Adapter{}, ansible.Adapter{}, sql.New())
	return Analyzer{Languages: langs}
}

// WithoutReuse returns the analyzer that never returns a remembered result, for comparing the
// facts with and without reuse.
func (an Analyzer) WithoutReuse() engine.Analyzer {
	an.NoReuse = true
	return an
}

// plan is how a version's files go to the languages.
type plan struct {
	byLang map[int][]string
	other  []string // files no language claims
	texts  bool     // whether a linker reads the tests' strings
}

// plan lists every file of the version. Each goes to the first language that claims it, among
// those the configuration enables; files no language claims are listed with role other.
func (an Analyzer) plan(v engine.Version, c config.Analysis) (plan, error) {
	paths, err := walk.Files(v.FS)
	if err != nil {
		return plan{}, err
	}
	p := plan{byLang: map[int][]string{}}
	for _, path := range paths {
		i := slices.IndexFunc(an.Languages, func(l Language) bool { return enabled(c, l.Name()) && claims(l, c, path) })
		if i < 0 {
			p.other = append(p.other, path)
			continue
		}
		p.byLang[i] = append(p.byLang[i], path)
	}
	p.texts = an.readsTexts(c, p.byLang)
	return p, nil
}

// side is one version being analysed.
type side struct {
	v    engine.Version
	c    config.Analysis
	plan plan
	run  *reuse.Run
	a    *facts.Analysis
}

func (an Analyzer) side(s *reuse.Session, v engine.Version, c config.Config) (*side, error) {
	ac := c.Analysis()
	p, err := an.plan(v, ac)
	if err != nil {
		return nil, err
	}
	a := &facts.Analysis{}
	for _, path := range p.other {
		a.Files = append(a.Files, facts.File{Path: path, Role: facts.RoleOther})
	}
	return &side{v: v, c: ac, plan: p, run: s.Run(v.Label, v.FS, ac), a: a}, nil
}

// Analyze reads one version with every language.
func (an Analyzer) Analyze(v engine.Version, c config.Config) (*facts.Analysis, error) {
	s := reuse.NewSession(an, !an.NoReuse)
	defer s.End()
	sd, err := an.side(s, v, c)
	if err != nil {
		return nil, err
	}
	for i := range an.Languages {
		if err := an.language(sd, i); err != nil {
			return nil, err
		}
		s.EndLanguage()
	}
	if err := an.link(sd); err != nil {
		return nil, err
	}
	sortAnalysis(sd.a)
	return sd.a, nil
}

// AnalyzePair reads both versions, language by language, base first. A stage head calls with the
// same input as base returns what base computed. Errors name the version they come from.
func (an Analyzer) AnalyzePair(bv engine.Version, bc config.Config, hv engine.Version, hc config.Config) (*facts.Analysis, *facts.Analysis, error) {
	s, b, h, err := an.pair(bv, bc, hv, hc)
	if err != nil {
		return nil, nil, err
	}
	s.End()
	return b, h, nil
}

// pair is AnalyzePair returning the session too, still holding its ScopePair results; the caller
// ends it.
func (an Analyzer) pair(bv engine.Version, bc config.Config, hv engine.Version, hc config.Config) (*reuse.Session, *facts.Analysis, *facts.Analysis, error) {
	s := reuse.NewSession(an, !an.NoReuse)
	fail := func(v engine.Version, err error) (*reuse.Session, *facts.Analysis, *facts.Analysis, error) {
		s.End()
		return nil, nil, nil, fmt.Errorf("analysing %s: %w", v.Label, err)
	}
	b, err := an.side(s, bv, bc)
	if err != nil {
		return fail(bv, err)
	}
	h, err := an.side(s, hv, hc)
	if err != nil {
		return fail(hv, err)
	}
	if err := s.Expect(h.run); err != nil {
		return fail(hv, err)
	}
	for i := range an.Languages {
		if err := an.language(b, i); err != nil {
			return fail(bv, err)
		}
		s.Settle()
		if err := an.language(h, i); err != nil {
			return fail(hv, err)
		}
		s.EndLanguage()
	}
	if err := an.link(b); err != nil {
		return fail(bv, err)
	}
	if err := an.link(h); err != nil {
		return fail(hv, err)
	}
	sortAnalysis(b.a)
	sortAnalysis(h.a)
	return s, b.a, h.a, nil
}

// language reads the version's files of language i, through the stage that remembers a language's
// whole facts.
func (an Analyzer) language(sd *side, i int) error {
	paths := sd.plan.byLang[i]
	if len(paths) == 0 {
		return nil
	}
	_, isSource := an.Languages[i].(textSource)
	la, err := languageStage.Do(sd.run, languageInput{Index: i, Name: an.Languages[i].Name(), Paths: paths,
		Texts: sd.plan.texts || !isSource})
	if err != nil {
		return err
	}
	merge(sd.a, la.Clone())
	return nil
}

// languageInput is what decides a language's facts besides the version's files: which language,
// which files it claimed, and whether it collects the tests' strings. Texts is part of the key
// because a version a linker reads strings in has different facts from one it does not.
type languageInput struct {
	Index int
	Name  string
	Paths []string
	Texts bool
}

// languageStage is one language's whole analysis of a version. It reads everything through the
// recorded file system, so a version whose every file the language read is the same gets the
// facts back without reading the language again.
var languageStage = reuse.NewReading("adapter/language", reuse.ScopeLanguage, analyzeLanguage)

func analyzeLanguage(rd reuse.Reads, in languageInput) (*facts.Analysis, error) {
	l := rd.Env().(Analyzer).Languages[in.Index]
	c := rd.Config()
	if ts, ok := l.(textSource); ok && !in.Texts {
		return ts.AnalyzeWithoutTexts(rd.Run(), c, in.Paths)
	}
	return l.Analyze(rd.Run(), c, in.Paths)
}

// link runs every linker on the version, after every language read it.
func (an Analyzer) link(sd *side) error {
	for _, l := range an.Languages {
		if lk, ok := l.(linker); ok && enabled(sd.c, l.Name()) {
			if err := lk.Link(sd.run, sd.c, sd.a); err != nil {
				return err
			}
		}
	}
	return nil
}

// readsTexts reports whether a linker reads the tests' strings of a version whose files went to
// the languages as byLang says.
func (an Analyzer) readsTexts(c config.Analysis, byLang map[int][]string) bool {
	for i, l := range an.Languages {
		if lk, ok := l.(linker); ok && enabled(c, l.Name()) && lk.ReadsTexts(c, byLang[i]) {
			return true
		}
	}
	return false
}

// mutator is a language that makes mutants of its functions (hadome mutate).
type mutator interface {
	Mutants(v engine.Version, funcs []facts.Symbol) ([]facts.Mutant, error)
}

// Mutants gives each function to the language that claims its file, and returns every mutant in
// the order of the languages, then of each language's own order. A function in a language that
// makes no mutants is left out; Unmutated lists such languages.
func (an Analyzer) Mutants(v engine.Version, funcs []facts.Symbol) ([]facts.Mutant, error) {
	var out []facts.Mutant
	for _, l := range an.Languages {
		m, ok := l.(mutator)
		if !ok {
			continue
		}
		var mine []facts.Symbol
		for _, f := range funcs {
			if l.Claims(f.Path) {
				mine = append(mine, f)
			}
		}
		if len(mine) == 0 {
			continue
		}
		ms, err := m.Mutants(v, mine)
		if err != nil {
			return nil, err
		}
		out = append(out, ms...)
	}
	return out, nil
}

// Unmutated returns the languages of the given functions that make no mutants yet.
func (an Analyzer) Unmutated(funcs []facts.Symbol) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range funcs {
		for _, l := range an.Languages {
			if !l.Claims(f.Path) {
				continue
			}
			if _, ok := l.(mutator); !ok && !seen[l.Name()] {
				seen[l.Name()] = true
				out = append(out, l.Name())
			}
			break
		}
	}
	sort.Strings(out)
	return out
}

func enabled(c config.Analysis, lang string) bool {
	return len(c.Languages) == 0 || slices.Contains(c.Languages, lang)
}

func merge(into, from *facts.Analysis) {
	into.Files = append(into.Files, from.Files...)
	into.Comments = append(into.Comments, from.Comments...)
	into.Symbols = append(into.Symbols, from.Symbols...)
	into.Imports = append(into.Imports, from.Imports...)
	into.Tests = append(into.Tests, from.Tests...)
	into.Entries = append(into.Entries, from.Entries...)
}

// sortAnalysis orders everything by path, then line, as facts.Analysis promises.
func sortAnalysis(a *facts.Analysis) {
	sort.Slice(a.Files, func(i, j int) bool { return a.Files[i].Path < a.Files[j].Path })
	sort.SliceStable(a.Comments, func(i, j int) bool {
		return less(a.Comments[i].Path, a.Comments[i].Line, a.Comments[j].Path, a.Comments[j].Line)
	})
	sort.SliceStable(a.Symbols, func(i, j int) bool { return a.Symbols[i].ID < a.Symbols[j].ID })
	sort.SliceStable(a.Imports, func(i, j int) bool {
		return less(a.Imports[i].Path, a.Imports[i].Line, a.Imports[j].Path, a.Imports[j].Line)
	})
	sort.SliceStable(a.Tests, func(i, j int) bool { return less(a.Tests[i].Path, a.Tests[i].Line, a.Tests[j].Path, a.Tests[j].Line) })
	sort.SliceStable(a.Entries, func(i, j int) bool {
		return less(a.Entries[i].Path, a.Entries[i].Line, a.Entries[j].Path, a.Entries[j].Line)
	})
}

func less(p1 string, l1 int, p2 string, l2 int) bool {
	if p1 != p2 {
		return p1 < p2
	}
	return l1 < l2
}
