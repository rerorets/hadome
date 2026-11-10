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

// InOneSession reads the versions one after another, language by language, in one session.
func (an Analyzer) InOneSession(vs []engine.Version, c config.Config) ([]*facts.Analysis, error) {
	s := reuse.NewSession(an, !an.NoReuse)
	defer s.End()
	var sides []*side
	for _, v := range vs {
		sd, err := an.side(s, v, c)
		if err != nil {
			return nil, err
		}
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
