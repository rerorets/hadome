package gate

import (
	"fmt"

	"github.com/rerorets/hadome/internal/engine"
)

type findingsAnswer struct {
	Schema   int `json:"schema"`
	Findings []struct {
		File    string `json:"file"`
		Line    int    `json:"line"`
		Anchor  string `json:"anchor"`
		Message string `json:"message"`
		Why     string `json:"why"`
		Fix     string `json:"fix"`
	} `json:"findings"`
}

func (a *findingsAnswer) schema() int { return a.Schema }

// findingGate is a gate of kind finding.
type findingGate struct{ *runner }

func (g *findingGate) ID() string { return g.id() }

func (g *findingGate) Find(in engine.Input) ([]engine.Finding, error) {
	var ans findingsAnswer
	if err := g.ask(in.Version, &ans); err != nil {
		return nil, err
	}
	out := make([]engine.Finding, 0, len(ans.Findings))
	for _, f := range ans.Findings {
		if f.File == "" || f.Anchor == "" {
			return nil, fmt.Errorf("gate %s on %s: every finding needs a file and an anchor", g.id(), in.Version.Label)
		}
		out = append(out, engine.Finding{Rule: g.id(), File: f.File, Line: f.Line, Anchor: f.Anchor,
			Message: engine.Verbatim(f.Message), Why: engine.Verbatim(f.Why), Fix: engine.Verbatim(f.Fix)})
	}
	return out, nil
}

type itemsAnswer struct {
	Schema int `json:"schema"`
	Items  []struct {
		Key    string `json:"key"`
		File   string `json:"file"`
		Line   int    `json:"line"`
		Detail string `json:"detail"`
	} `json:"items"`
}

func (a *itemsAnswer) schema() int { return a.Schema }

type removedAnswer struct {
	Schema     int      `json:"schema"`
	Legitimate []string `json:"legitimate"`
}

func (a *removedAnswer) schema() int { return a.Schema }

// measureGate is a gate of kind measure. It remembers each version's items, so that it asks the
// command about every missing item at once.
type measureGate struct {
	*runner
	items      map[string][]engine.Item // by version label
	legitimate map[string]bool          // answered once per check
}

func (g *measureGate) ID() string { return g.id() }

func (g *measureGate) Measure(in engine.Input) ([]engine.Item, error) {
	var ans itemsAnswer
	if err := g.ask(in.Version, &ans); err != nil {
		return nil, err
	}
	out := make([]engine.Item, 0, len(ans.Items))
	for _, it := range ans.Items {
		if it.Key == "" {
			return nil, fmt.Errorf("gate %s on %s: every item needs a key", g.id(), in.Version.Label)
		}
		out = append(out, engine.Item{Rule: g.id(), Key: it.Key, File: it.File, Line: it.Line,
			Detail: engine.T("%s: %s is no longer guarded (was: %s)", g.id(), it.Key, it.Detail),
			Why:    engine.T("the product's own gate %s reported it in the base version", g.id()),
			Fix:    engine.T("restore it, or, if it was removed on purpose, make the gate answer it as legitimate")})
	}
	g.items[in.Version.Label] = out
	return out, nil
}

// RemovedLegitimately asks the command, once, about every item of base that head lacks.
func (g *measureGate) RemovedLegitimately(it engine.Item, head engine.Input) (bool, error) {
	if g.legitimate == nil {
		if err := g.askRemoved(head); err != nil {
			return false, err
		}
	}
	return g.legitimate[it.Key], nil
}

func (g *measureGate) askRemoved(head engine.Input) error {
	present := map[string]bool{}
	for _, it := range g.items[head.Version.Label] {
		present[it.Key] = true
	}
	var missing []string
	for label, items := range g.items {
		if label == head.Version.Label {
			continue
		}
		for _, it := range items {
			if !present[it.Key] {
				missing = append(missing, it.Key)
			}
		}
	}
	req, err := g.newRequest(head.Version)
	if err != nil {
		return err
	}
	req.Mode, req.Keys, req.Files = "removed", missing, nil
	if req.Keys == nil {
		req.Keys = []string{}
	}
	var ans removedAnswer
	if err := g.call(req, &ans); err != nil {
		return err
	}
	g.legitimate = map[string]bool{}
	for _, k := range ans.Legitimate {
		g.legitimate[k] = true
	}
	return nil
}
