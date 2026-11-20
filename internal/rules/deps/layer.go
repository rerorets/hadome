// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

package deps

import (
	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
)

// layer is deps/layer.
type layer struct{}

func (layer) ID() string           { return "deps/layer" }
func (layer) Cases() []engine.Case { return layerCases() }
func (layer) About() engine.About {
	return engine.About{Finds: engine.T("an import from a unit of an inner layer to a unit of an outer layer"),
		Why:  engine.T("dependencies point from the outside in; an inner layer importing an outer one ties the core to details such as the database or the web framework, and makes it hard to replace and to test"),
		Fix:  engine.T("let the inner layer declare an interface the outer layer implements; if the design changed, rewrite the layers, which a person reviews"),
		Keys: []string{"rules.deps.layers"}}
}

// Find reports imports between the repository's units that go from an inner layer to an outer one.
func (r layer) Find(in engine.Input) ([]engine.Finding, error) {
	layers := in.Config.ArchLayers
	if len(layers) == 0 {
		return nil, nil
	}
	unitDir := dirs(in.Facts)
	var out []engine.Finding
	for _, im := range in.Facts.Imports {
		from, to := unitDir[im.FromUnit], unitDir[im.ToUnit]
		if !im.Internal || from == "" || to == "" {
			continue
		}
		inner, outer := layerOf(layers, from), layerOf(layers, to)
		if inner < 0 || outer <= inner {
			continue
		}
		out = append(out, engine.Finding{
			Rule: r.ID(), File: im.Path, Line: im.Line, Anchor: from + " -> " + to,
			Message: engine.T("import from an inner layer: %s → %s (layer %s imports the outer layer %s)",
				from, to, layers[inner].Name, layers[outer].Name),
			Why: r.About().Why,
			Fix: r.About().Fix,
		})
	}
	return out, nil
}

// layerOf returns the index of the first layer whose globs match the directory, or -1.
func layerOf(layers []config.ArchLayer, dir string) int {
	for i, l := range layers {
		if config.MatchesAny(l.Paths, dir) {
			return i
		}
	}
	return -1
}
