// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

package config

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
)

// FileName is the product layer's file, at the repository root.
const FileName = ".hadome.yml"

// Config is the merged configuration of one version.
type Config struct {
	Schema     int
	Languages  []string // empty means detected
	Invariants []Invariant
	Gates      []Gate
	Hazards    map[string][]string // hazard category -> entries added to the built-in table
	IO         []string            // import paths added to the built-in io table
	Assertions []string            // functions added to the built-in table of assertions
	// TestFiles and GeneratedFiles are globs that give files the test and generated roles.
	TestFiles      []string
	GeneratedFiles []string
	// AnsibleFiles are globs of playbooks the Ansible adapter reads outside its layout.
	AnsibleFiles []string
	// ProductFiles are globs of files that are product code although their directory or name would
	// make them test support files. They never make a test file product code.
	ProductFiles []string
	// Mutation configures hadome mutate; nil when it is not configured.
	Mutation *Mutation
	Size     Limits // limits of the size/ rules, defaults filled in
	TodoForm string // the regular expression a TODO comment must match
	// EntryPoints are the framework entry points comments/missing-doc asks no doc comment of.
	EntryPoints  []EntryPoint
	CodeSettings // rules.code
	// Messages is the language of the wording hadome writes: en (the default) or ja.
	Messages string
	// Diagram is the dependency diagram that judges imports: head's, for both versions. Nil when none
	// is configured.
	Diagram *Diagram
	// OwnDiagram is this version's own diagram, whose boxes must name units of this version's tree.
	OwnDiagram *Diagram
	// Calls is the call diagram that judges calls between units: head's, for both versions. Nil when
	// none is configured. OwnCalls is this version's own, as OwnDiagram is.
	Calls    *Diagram
	OwnCalls *Diagram
	// ArchLayers are the layers of the architecture, rules.deps.layers, from the inside out.
	ArchLayers []ArchLayer
	// CycleAllow are the pairs of directory globs whose imports between each other deps/cycle does
	// not follow (rules.deps.cycles.allow).
	CycleAllow [][2]string
	// Layers are the files that were read, earliest layer first. Empty means built-in settings only.
	Layers []string
	// Databases are the SQL databases of sql.databases, in order. Empty means every .sql file is one
	// database whose dialect is decided from its files.
	Databases []Database
	// SQLSinks are the calls that take SQL added by rules.tests.sql-sinks, by language name.
	SQLSinks map[string][]string

	raw map[string]any
	// lines are the lines of .hadome.yml by key path (rules.size.params, rules.tests.io[0]).
	lines map[string]int
}

// ArchLayer is one layer of the architecture: its name and the globs of its units' directories.
type ArchLayer struct {
	Name  string
	Paths []string
}

// Limits are the size/ rules' limits by key name ("file-lines", "params", …).
type Limits map[string]int

// Invariant is one promise the product keeps: a built-in Type with its targets, or a product-specific ID.
type Invariant struct {
	Type    string
	Target  string
	Targets []string
	ID      string
	Says    string
}

// Key identifies the invariant across versions.
func (i Invariant) Key() string {
	switch {
	case i.ID != "":
		return i.ID
	case i.Target != "":
		return i.Type + " " + i.Target
	default:
		return i.Type + " " + strings.Join(i.Targets, ",")
	}
}

// TargetNames reports whether a configured target ("store.Append") names a symbol ID
// ("example.com/app/store.Append"): the ID is the target, or ends with it after a path separator.
func TargetNames(target, id string) bool {
	return id == target || strings.HasSuffix(id, "/"+target)
}

// Database is one SQL database of sql.databases: a set of objects whose names share one namespace.
type Database struct {
	Name    string
	Dialect string // empty: decided from the markers in its files
	// Paths are globs of the database's .sql files; empty means every .sql file.
	Paths []string
	// SearchPath is the order unqualified names are searched in.
	SearchPath []string
	// DefaultSchema is the schema of unqualified definitions.
	DefaultSchema string
	// Catalog is the database (project) name; the database part of names is compared only when set.
	Catalog string
	// Order are globs giving the order the database's files are applied in.
	Order []string
	// Tests are globs of test files whose SQL is resolved against this database.
	Tests []string
}

// EntryPoint names symbols a framework calls by name: a symbol whose name without its unit matches
// one of Names, in a file matching one of Paths (any file when Paths is empty).
type EntryPoint struct {
	Names []string
	Paths []string
}

// Matches reports whether the entry point covers a symbol named name in the file at p.
func (e EntryPoint) Matches(name, p string) bool {
	if len(e.Paths) > 0 && !MatchesAny(e.Paths, p) {
		return false
	}
	for _, n := range e.Names {
		if ok, err := path.Match(n, name); err == nil && ok {
			return true
		}
	}
	return false
}

// Gate is one product-specific gate.
type Gate struct {
	ID      string
	Kind    string
	Run     []string
	Paths   []string // globs; empty means every file
	Timeout string
	Options any // passed to the gate untouched
}

// GateLine returns the line of the gate with this id in .hadome.yml, or of the gates key when the
// gate comes from a shared layer.
func (c Config) GateLine(id string) int {
	return c.lineOf(Change{Key: gateKey.Name, Added: id})
}

// Load reads the product layer from product (the version's tree) and the shared layers it extends
// from workdir (the working tree, since shared files live outside the repository). Without a
// product file it returns the built-in settings. An error means the check must not run.
func Load(product fs.FS, workdir string) (Config, error) {
	return load(product, workdir, nil)
}

// LoadHead loads head's configuration as Load does, except that of the files head's .hadome.yml
// extends, it reads only those base's .hadome.yml extends too. Head's extends can name any path, so
// a path the change adds is not read before a person has seen it: Diff reports the change, and the
// layers it would add are left out.
func LoadHead(product fs.FS, workdir string, base Config) (Config, error) {
	read := map[string]bool{}
	for _, e := range list(base.raw["extends"]) {
		read[e.(string)] = true
	}
	return load(product, workdir, read)
}

// load reads the configuration. When extendable is not nil, the product layer extends only the
// files it holds.
func load(product fs.FS, workdir string, extendable map[string]bool) (Config, error) {
	data, err := fs.ReadFile(product, FileName)
	if errors.Is(err, fs.ErrNotExist) {
		return decode(map[string]any{"schema": 1}, nil), nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("reading %s: %w", FileName, err)
	}
	if len(data) > MaxFileSize {
		return Config{}, Errors{{File: FileName, Line: 1, Key: "(file)", Problem: "is " + tooLarge}}
	}
	l := &loader{visiting: map[string]bool{}, extendable: extendable}
	raw, err := l.layer(FileName, data, workdir, false)
	if err != nil {
		return Config{}, err
	}
	if l.extends != nil {
		raw["extends"] = l.extends
	}
	c := decode(raw, l.layers)
	c.lines = l.lines
	if errs := checkMutation(c); len(errs) > 0 {
		return Config{}, errs
	}
	deps := object(object(raw["rules"])["deps"])
	diagram, _ := deps["diagram"].(string)
	if diagram != "" {
		if c.Diagram, err = loadDiagram(product, diagram, DiagramKey); err != nil {
			return Config{}, err
		}
		c.OwnDiagram = c.Diagram
	}
	if calls, ok := deps["calls"].(string); ok {
		if calls == diagram {
			return Config{}, Errors{{File: FileName, Line: max(c.lines[CallsKey], 1), Key: CallsKey,
				Problem: fmt.Sprintf("names %s, the dependency diagram; every flowchart there is read as imports, so draw the calls in a file of their own", calls),
				Example: "calls: docs/conventions/calls.md"}}
		}
		if c.Calls, err = loadDiagram(product, calls, CallsKey); err != nil {
			return Config{}, err
		}
		c.OwnCalls = c.Calls
	}
	return c, nil
}

// checkMutation finds what the specification of mutation.test cannot say: {packages} takes a
// whole element, since it spreads into one element per package, appears once, and is filled by Go
// tests only, so a configuration whose languages lack Go cannot use it.
func checkMutation(c Config) Errors {
	if c.Mutation == nil {
		return nil
	}
	fail := func(i int, problem string) Errors {
		line := c.lines[fmt.Sprintf("mutation.test[%d]", i)]
		if line == 0 {
			line = max(c.lines["mutation.test"], 1)
		}
		return Errors{{File: FileName, Line: line, Key: fmt.Sprintf("mutation.test[%d]", i), Problem: problem,
			Example: `test: [go, test, "{packages}", -run, "^({tests})$"]`}}
	}
	seen := -1
	for i, a := range c.Mutation.Test {
		switch {
		case a == PackagesPlaceholder && seen >= 0:
			return fail(i, PackagesPlaceholder+" appears twice; write it once")
		case a == PackagesPlaceholder:
			seen = i
		case strings.Contains(a, PackagesPlaceholder):
			return fail(i, PackagesPlaceholder+" must be an element by itself, since it becomes one element per package")
		}
	}
	if seen >= 0 && len(c.Languages) > 0 && !slices.Contains(c.Languages, "go") {
		return fail(seen, PackagesPlaceholder+" is filled with Go packages only, and languages has no go; narrow with {tests} instead")
	}
	return nil
}

// PackagesPlaceholder is the element of mutation.test that becomes the packages of the tests run.
const PackagesPlaceholder = "{packages}"

// ForBase returns the configuration that judges base: head's settings with base's invariants and
// base's own diagrams.
// Settings, tables and gates come from head and apply to both versions, so tightening them never
// fails a change on code that was already there. Invariants are compared version by version,
// so a new invariant without a guarding test is a regression.
func ForBase(base, head Config) Config {
	c := head
	c.Invariants = base.Invariants
	c.OwnDiagram = base.OwnDiagram
	c.OwnCalls = base.OwnCalls
	return c
}

// Mutation is the mutation setting: how hadome mutate runs the tests.
type Mutation struct {
	Test           []string // the test command; an element may hold {tests}, or be {packages}
	Timeout        string   // a duration such as 2m; empty means three times each command's clean run
	MaxMutants     int
	TestsSeparator string
}
