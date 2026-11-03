// Package adapter is the analyzer the engine uses: it gives each file of a version to the language
// that claims it and merges what the languages report.
//
// Design: docs/design/adapters.md, docs/design/adapters-treesitter.md
package adapter

import (
	"slices"
	"sort"

	"github.com/rerorets/hadome/internal/adapter/golang"
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
	// (such as go.mod or tsconfig.json) to understand them.
	Analyze(v engine.Version, c config.Config, paths []string) (*facts.Analysis, error)
}

// Analyzer reads a version with every language it has.
type Analyzer struct {
	Languages []Language
}

// New returns the analyzer with every language this build of hadome has.
func New() Analyzer {
	langs := []Language{golang.Adapter{}}
	for _, l := range treesitter.Languages() { // a different type with and without cgo
		langs = append(langs, l)
	}
	return Analyzer{Languages: langs}
}

// Analyze lists every file of the version. Each goes to the first language that claims it, among
// those the configuration enables; files no language claims are listed with role other.
func (an Analyzer) Analyze(v engine.Version, c config.Config) (*facts.Analysis, error) {
	paths, err := walk.Files(v.FS)
	if err != nil {
		return nil, err
	}
	byLang := map[int][]string{}
	a := &facts.Analysis{}
	for _, p := range paths {
		i := slices.IndexFunc(an.Languages, func(l Language) bool { return enabled(c, l.Name()) && l.Claims(p) })
		if i < 0 {
			a.Files = append(a.Files, facts.File{Path: p, Role: facts.RoleOther})
			continue
		}
		byLang[i] = append(byLang[i], p)
	}
	for i, l := range an.Languages {
		if len(byLang[i]) == 0 {
			continue
		}
		la, err := l.Analyze(v, c, byLang[i])
		if err != nil {
			return nil, err
		}
		merge(a, la)
	}
	sortAnalysis(a)
	return a, nil
}

func enabled(c config.Config, lang string) bool {
	return len(c.Languages) == 0 || slices.Contains(c.Languages, lang)
}

func merge(into, from *facts.Analysis) {
	into.Files = append(into.Files, from.Files...)
	into.Comments = append(into.Comments, from.Comments...)
	into.Symbols = append(into.Symbols, from.Symbols...)
	into.Imports = append(into.Imports, from.Imports...)
	into.Tests = append(into.Tests, from.Tests...)
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
}

func less(p1 string, l1 int, p2 string, l2 int) bool {
	if p1 != p2 {
		return p1 < p2
	}
	return l1 < l2
}
