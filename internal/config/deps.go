// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

package config

// depsRules reads the validated settings of the deps/ rules besides the diagrams: the layers of
// rules.deps.layers, and the pairs of rules.deps.cycles.allow split into their two globs.
func depsRules(deps map[string]any) ([]ArchLayer, [][2]string) {
	var layers []ArchLayer
	for _, v := range list(deps["layers"]) {
		m := object(v)
		layers = append(layers, ArchLayer{Name: str(m["name"]), Paths: textList(m["paths"])})
	}
	var allow [][2]string
	for _, e := range textList(object(deps["cycles"])["allow"]) {
		a, b, _ := GlobPair(e)
		allow = append(allow, [2]string{a, b})
	}
	return layers, allow
}
