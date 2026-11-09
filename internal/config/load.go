package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/goccy/go-yaml/parser"
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
	EntryPoints []EntryPoint
	// Messages is the language of the wording hadome writes: en (the default) or ja.
	Messages string
	// Diagram is the dependency diagram that judges imports: head's, for both versions. Nil when none
	// is configured.
	Diagram *Diagram
	// OwnDiagram is this version's own diagram, whose boxes must name units of this version's tree.
	OwnDiagram *Diagram
	// Layers are the files that were read, earliest layer first. Empty means built-in settings only.
	Layers []string

	raw map[string]any
	// lines are the lines of .hadome.yml by key path (rules.size.params, rules.tests.io[0]).
	lines map[string]int
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

// Load reads the product layer from product (the version's tree) and the shared layers it extends
// from workdir (the working tree, since shared files live outside the repository). Without a
// product file it returns the built-in settings. An error means the check must not run.
func Load(product fs.FS, workdir string) (Config, error) {
	data, err := fs.ReadFile(product, FileName)
	if errors.Is(err, fs.ErrNotExist) {
		return decode(map[string]any{"schema": 1}, nil), nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("reading %s: %w", FileName, err)
	}
	l := &loader{visiting: map[string]bool{}}
	raw, err := l.layer(FileName, data, workdir)
	if err != nil {
		return Config{}, err
	}
	c := decode(raw, l.layers)
	c.lines = l.lines
	if errs := checkMutation(c); len(errs) > 0 {
		return Config{}, errs
	}
	if path, ok := object(object(raw["rules"])["deps"])["diagram"].(string); ok {
		if c.Diagram, err = loadDiagram(product, path); err != nil {
			return Config{}, err
		}
		c.OwnDiagram = c.Diagram
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

// loadDiagram reads the dependency diagram from the version's tree: it is part of the repository,
// like the code it describes.
func loadDiagram(product fs.FS, path string) (*Diagram, error) {
	data, err := fs.ReadFile(product, path)
	if err != nil {
		return nil, Errors{{File: FileName, Line: 1, Key: "rules.deps.diagram",
			Problem: fmt.Sprintf("cannot read the diagram %s: %v", path, errors.Unwrap(err)),
			Example: "diagram: docs/conventions/dependencies.md"}}
	}
	d, errs := parseDiagram(path, data)
	if len(errs) > 0 {
		return nil, errs
	}
	return d, nil
}

// loader reads one file and the files it extends, depth first, so earlier layers come first.
type loader struct {
	layers   []string
	visiting map[string]bool
	lines    map[string]int // of the first layer read: .hadome.yml itself
}

func (l *loader) layer(name string, data []byte, dir string) (map[string]any, error) {
	v := &validator{file: name, lines: map[string]int{}}
	if l.lines == nil {
		l.lines = v.lines
	}
	f, err := parser.ParseBytes(data, 0)
	if err != nil {
		return nil, Errors{{File: name, Line: 1, Key: "(file)", Problem: "is not valid YAML: " + firstLine(err.Error())}}
	}
	raw := map[string]any{}
	switch {
	case len(f.Docs) > 1:
		v.errs = append(v.errs, Error{File: name, Line: 1, Key: "(file)", Problem: "must hold one YAML document, not several"})
	case len(f.Docs) == 0 || f.Docs[0].Body == nil:
		v.fail(nil, "", "is empty", Spec)
	default:
		if m, ok := v.value(f.Docs[0].Body, "", Spec).(map[string]any); ok {
			raw = m
		}
	}
	if len(v.errs) > 0 {
		return nil, v.errs
	}
	merged := map[string]any{}
	exts, _ := raw["extends"].([]any)
	for i, e := range exts {
		base, err := l.extend(name, e.(string), dir, v.lines[fmt.Sprintf("extends[%d]", i)])
		if err != nil {
			return nil, err
		}
		merged = merge(merged, base, Spec)
	}
	delete(raw, "extends")
	l.layers = append(l.layers, name)
	return merge(merged, raw, Spec), nil
}

func (l *loader) extend(from, ref, dir string, line int) (map[string]any, error) {
	fail := func(problem string) error {
		return Errors{{File: from, Line: line, Key: "extends", Problem: problem,
			Example: "extends: [../shared/hadome.yml]"}}
	}
	if strings.Contains(ref, "://") {
		return nil, fail(fmt.Sprintf("%q is a URL; hadome never reads configuration from the network. "+
			"Check the shared file out locally and point at it", ref))
	}
	p := ref
	if !filepath.IsAbs(p) {
		p = filepath.Join(dir, p)
	}
	p = filepath.Clean(p)
	if l.visiting[p] {
		return nil, fail(fmt.Sprintf("%q extends itself through a cycle", ref))
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, fail(fmt.Sprintf("cannot read %q: %v", ref, errors.Unwrap(err)))
	}
	l.visiting[p] = true
	defer delete(l.visiting, p)
	return l.layer(p, data, filepath.Dir(p))
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	return s
}

// merge lays over on top of under. Objects merge key by key; lists of objects with an identity
// replace entries with the same identity and keep the rest; lists marked OmittedIsWidest are
// replaced as a whole; other lists are joined without duplicates; anything else is replaced.
func merge(under, over map[string]any, k Key) map[string]any {
	out := map[string]any{}
	for name, val := range under {
		out[name] = val
	}
	for name, val := range over {
		f := field(k, name)
		switch {
		case f.Kind == KindObject && len(f.Variants) == 0 && out[name] != nil:
			out[name] = merge(out[name].(map[string]any), val.(map[string]any), f)
		case f.Kind == KindList && !f.OmittedIsWidest && out[name] != nil:
			out[name] = joinLists(out[name].([]any), val.([]any), *f.Elem, f.Identity)
		default:
			out[name] = val
		}
	}
	return out
}

func field(k Key, name string) Key {
	for _, f := range k.Fields {
		if f.Name == name {
			return f
		}
	}
	return Key{}
}

func joinLists(under, over []any, elem Key, identity []string) []any {
	var out []any
	at := map[string]int{}
	for _, list := range [][]any{under, over} {
		for _, val := range list {
			id := identityOf(val, elem, identity)
			if i, ok := at[id]; ok {
				out[i] = val
				continue
			}
			at[id] = len(out)
			out = append(out, val)
		}
	}
	return out
}

// identityOf returns the string that identifies a list entry across layers and versions.
func identityOf(val any, elem Key, identity []string) string {
	m, ok := val.(map[string]any)
	if !ok || elem.Kind != KindObject {
		return fmt.Sprint(val)
	}
	var parts []string
	for _, name := range identity {
		if v, ok := m[name]; ok {
			parts = append(parts, name+"="+fmt.Sprint(v))
		}
	}
	return strings.Join(parts, " ")
}

// ForBase returns the configuration that judges base: head's settings with base's invariants and
// base's own diagram.
// Settings, tables and gates come from head and apply to both versions, so tightening them never
// fails a change on code that was already there. Invariants are compared version by version,
// so a new invariant without a guarding test is a regression.
func ForBase(base, head Config) Config {
	c := head
	c.Invariants = base.Invariants
	c.OwnDiagram = base.OwnDiagram
	return c
}

// Mutation is the mutation setting: how hadome mutate runs the tests.
type Mutation struct {
	Test           []string // the test command; an element may hold {tests}, or be {packages}
	Timeout        string   // a duration such as 2m; empty means three times each command's clean run
	MaxMutants     int
	TestsSeparator string
}
