// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

package config

// decode turns validated plain values into a Config. Validation has already guaranteed every type.
func decode(raw map[string]any, layers []string) Config {
	c := Config{raw: raw, Layers: layers, Hazards: map[string][]string{}}
	c.Schema, _ = raw["schema"].(int)
	c.Languages = textList(raw["languages"])
	c.Messages = str(orDefault(raw["messages"], "en"))
	for _, v := range list(raw["invariants"]) {
		m := v.(map[string]any)
		c.Invariants = append(c.Invariants, Invariant{
			Type: str(m["type"]), Target: str(m["target"]), Targets: textList(m["targets"]),
			ID: str(m["id"]), Says: str(m["says"]),
		})
	}
	for _, v := range list(raw["gates"]) {
		m := v.(map[string]any)
		c.Gates = append(c.Gates, Gate{ID: str(m["id"]), Kind: str(m["kind"]), Run: textList(m["run"]),
			Paths: textList(m["paths"]), Timeout: str(m["timeout"]), Options: m["options"]})
	}
	tests := object(object(raw["rules"])["tests"])
	for category, v := range object(tests["hazards"]) {
		c.Hazards[category] = textList(v)
	}
	c.IO = textList(tests["io"])
	c.Assertions = textList(tests["assertions"])
	for lang, v := range object(tests["sql-sinks"]) {
		if c.SQLSinks == nil {
			c.SQLSinks = map[string][]string{}
		}
		c.SQLSinks[lang] = textList(v)
	}
	for _, v := range list(object(raw["sql"])["databases"]) {
		m := object(v)
		c.Databases = append(c.Databases, Database{Name: str(m["name"]), Dialect: str(m["dialect"]),
			Paths: textList(m["paths"]), SearchPath: textList(m["search-path"]), DefaultSchema: str(m["default-schema"]),
			Catalog: str(m["catalog"]), Order: textList(m["order"]), Tests: textList(m["tests"])})
	}
	if m, ok := raw["mutation"].(map[string]any); ok {
		c.Mutation = &Mutation{Test: textList(m["test"]), Timeout: str(m["timeout"]),
			MaxMutants: intOr(m["max-mutants"], 100), TestsSeparator: str(orDefault(m["tests-separator"], "|"))}
	}
	c.decodeFiles(object(raw["files"]))
	size := object(object(raw["rules"])["size"])
	c.Size = Limits{}
	for _, k := range sizeKey.Fields {
		c.Size[k.Name] = intOr(size[k.Name], k.Default.(int))
	}
	c.ArchLayers, c.CycleAllow = depsRules(object(object(raw["rules"])["deps"]))
	c.TodoForm = DefaultTodo
	comments := object(object(raw["rules"])["comments"])
	if form, ok := comments["todo"].(string); ok {
		c.TodoForm = form
	}
	for _, v := range list(comments["entry-points"]) {
		m := object(v)
		c.EntryPoints = append(c.EntryPoints, EntryPoint{Names: textList(m["names"]), Paths: textList(m["paths"])})
	}
	c.CodeSettings = codeSettings(object(object(raw["rules"])["code"]))
	return c
}

// decodeFiles reads the globs of files, which give files their roles.
func (c *Config) decodeFiles(files map[string]any) {
	c.TestFiles = textList(files["tests"])
	c.GeneratedFiles = textList(files["generated"])
	c.AnsibleFiles = textList(files["ansible"])
	c.ProductFiles = textList(files["product"])
}

func orDefault(v, def any) any {
	if v == nil {
		return def
	}
	return v
}

func intOr(v any, def int) int {
	if i, ok := v.(int); ok {
		return i
	}
	return def
}

func list(v any) []any { l, _ := v.([]any); return l }
func str(v any) string { s, _ := v.(string); return s }

func object(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func textList(v any) []string {
	var out []string
	for _, e := range list(v) {
		out = append(out, str(e))
	}
	return out
}
