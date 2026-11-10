package adapter

import (
	"github.com/rerorets/hadome/internal/adapter/reuse"
	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
)

// PairWithStats is AnalyzePair also returning how often each stage was served and run.
func (an Analyzer) PairWithStats(bv engine.Version, bc config.Config, hv engine.Version, hc config.Config) (*facts.Analysis, *facts.Analysis, map[string]reuse.Stat, error) {
	s, b, h, err := an.pair(bv, bc, hv, hc)
	if err != nil {
		return nil, nil, nil, err
	}
	defer s.End()
	return b, h, s.Stats(), nil
}

// Held is what a session holds when an analysis call returns, before it ends: by stage, what the
// session still remembers, and how often each stage was served and run.
type Held struct {
	Remembered []string
	Stats      map[string]reuse.Stat
}

// AnalyzeHeld is Analyze also returning what the session held at its end.
func (an Analyzer) AnalyzeHeld(v engine.Version, c config.Config) (*facts.Analysis, Held, error) {
	s, a, err := an.one(v, c)
	if err != nil {
		return nil, Held{}, err
	}
	defer s.End()
	return a, Held{s.Remembered(), s.Stats()}, nil
}

// PairHeld is AnalyzePair also returning what the session held at its end.
func (an Analyzer) PairHeld(bv engine.Version, hv engine.Version, c config.Config) (Held, error) {
	s, _, _, err := an.pair(bv, c, hv, c)
	if err != nil {
		return Held{}, err
	}
	defer s.End()
	return Held{s.Remembered(), s.Stats()}, nil
}

// OneLanguageHeld reads the language named lang of base and then of head as AnalyzePair does, and
// returns what the session holds right after, before it lets go of the language's results.
func (an Analyzer) OneLanguageHeld(bv, hv engine.Version, c config.Config, lang string) (Held, error) {
	s := reuse.NewSession(an, !an.NoReuse)
	defer s.End()
	b, err := an.side(bv, c)
	if err != nil {
		return Held{}, err
	}
	h, err := an.side(hv, c)
	if err != nil {
		return Held{}, err
	}
	b.in(s)
	h.in(s)
	if err := s.Expect(h.run, h.plan.paths); err != nil {
		return Held{}, err
	}
	for i, l := range an.Languages {
		if l.Name() == lang {
			if _, err := an.both(s, b, h, i); err != nil {
				return Held{}, err
			}
		}
	}
	return Held{s.Remembered(), s.Stats()}, nil
}

// InOneSession reads the versions one after another, language by language, in one session.
func (an Analyzer) InOneSession(vs []engine.Version, c config.Config) ([]*facts.Analysis, error) {
	s := reuse.NewSession(an, !an.NoReuse)
	defer s.End()
	var sides []*side
	for _, v := range vs {
		sd, err := an.side(v, c)
		if err != nil {
			return nil, err
		}
		sd.in(s)
		sides = append(sides, sd)
	}
	for i := range an.Languages {
		for _, sd := range sides {
			if err := an.language(sd, i); err != nil {
				return nil, err
			}
		}
		s.EndLanguage()
	}
	var out []*facts.Analysis
	for _, sd := range sides {
		if err := an.link(sd); err != nil {
			return nil, err
		}
		sortAnalysis(sd.a)
		out = append(out, sd.a)
	}
	return out, nil
}
