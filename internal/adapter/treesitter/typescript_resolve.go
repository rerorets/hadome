//go:build cgo

package treesitter

import (
	"encoding/json"
	"io/fs"
	"path"
	"sort"
	"strings"
)

// How the TypeScript reader turns an import specifier into a file of the repository: relative
// paths, the aliases and baseUrl of the tsconfig.json nearest the importing file, and the packages
// of the repository named by their package.json.

// tsKind is what a specifier turned out to name.
type tsKind int

const (
	tsExternal tsKind = iota // a package from outside the repository
	tsFound                  // a file of the repository
	tsMissing                // looks like the repository's own, yet names no file of it
	tsUnread                 // a path inside a directory hadome does not read (dist/, .generated/)
)

// tsProject is what resolving needs to know about the whole version.
type tsProject struct {
	fsys    fs.FS
	exists  map[string]bool      // the TypeScript and JavaScript files
	configs map[string]*tsConfig // directory -> the config of its nearest tsconfig.json, nil for none
	// packages are the repository's package.json names, longest first, and their directories.
	packages []tsPackage
}

type tsPackage struct {
	name, dir string
	entries   []string // exports["."], module, main and types, relative to dir
}

// tsConfig is what a tsconfig.json, with what it extends, says about resolving.
type tsConfig struct {
	baseURL  string // a directory of the repository; "" when unset
	hasBase  bool
	paths    map[string][]string
	pathsDir string // where paths resolve from when there is no baseUrl
	hasPaths bool
}

func newTSProject(fsys fs.FS, files []*file) *tsProject {
	p := &tsProject{fsys: fsys, exists: map[string]bool{}, configs: map[string]*tsConfig{}}
	dirs := map[string]bool{}
	for _, f := range files {
		p.exists[f.path] = true
		for d := path.Dir(f.path); !dirs[d]; d = path.Dir(d) {
			dirs[d] = true
			if d == "." {
				break
			}
		}
	}
	var sorted []string
	for d := range dirs {
		sorted = append(sorted, d)
	}
	sort.Strings(sorted)
	seen := map[string]bool{}
	for _, d := range sorted {
		if strings.Contains("/"+d+"/", "/node_modules/") {
			continue
		}
		if pkg, ok := p.readPackage(d); ok && !seen[pkg.name] {
			seen[pkg.name] = true
			p.packages = append(p.packages, pkg)
		}
	}
	sort.SliceStable(p.packages, func(i, j int) bool { return len(p.packages[i].name) > len(p.packages[j].name) })
	return p
}

func (p *tsProject) readPackage(dir string) (tsPackage, bool) {
	data, err := fs.ReadFile(p.fsys, path.Join(dir, "package.json"))
	if err != nil {
		return tsPackage{}, false
	}
	var raw struct {
		Name    string          `json:"name"`
		Main    string          `json:"main"`
		Module  string          `json:"module"`
		Types   string          `json:"types"`
		Exports json.RawMessage `json:"exports"`
	}
	if json.Unmarshal(jsonc(data), &raw) != nil || raw.Name == "" {
		return tsPackage{}, false
	}
	pkg := tsPackage{name: raw.Name, dir: dir}
	pkg.entries = append(exportEntries(raw.Exports), raw.Module, raw.Main, raw.Types)
	return pkg, true
}

// exportEntries lists the files the "." entry of a package's exports names, in a fixed order of
// conditions: exports may be a string, conditions, or subpaths with "." among them.
func exportEntries(raw json.RawMessage) []string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return []string{s}
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	if dot, ok := m["."]; ok {
		return exportEntries(dot)
	}
	var out []string
	for _, cond := range []string{"source", "types", "import", "module", "default", "require", "node"} {
		if c, ok := m[cond]; ok {
			out = append(out, exportEntries(c)...)
		}
	}
	return out
}

// file returns the file of the repository a path names, or "".
func (p *tsProject) file(base string) string {
	base = path.Clean(base)
	exts := extensions["typescript"]
	for _, ext := range append([]string{""}, exts...) {
		if p.exists[base+ext] {
			return base + ext
		}
	}
	for _, ext := range exts {
		if p.exists[base+"/index"+ext] {
			return base + "/index" + ext
		}
	}
	// ESM TypeScript names the file it compiles to: './a.js' loads a.ts.
	for js, tss := range map[string][]string{".js": {".ts", ".tsx"}, ".jsx": {".tsx"}, ".mjs": {".mts"}, ".cjs": {".cts"}} {
		if stem, ok := strings.CutSuffix(base, js); ok {
			for _, ext := range tss {
				if p.exists[stem+ext] {
					return stem + ext
				}
			}
		}
	}
	return ""
}

// lookup resolves a path of the repository: to a TypeScript file, to a directory hadome does not
// read, to another file that exists (a stylesheet, a JSON file), or to nothing.
func (p *tsProject) lookup(base string) (string, tsKind) {
	if f := p.file(base); f != "" {
		return f, tsFound
	}
	if inUnreadDir(base) {
		return path.Clean(base), tsUnread
	}
	if st, err := fs.Stat(p.fsys, path.Clean(base)); err == nil && !st.IsDir() {
		return path.Clean(base), tsFound
	}
	return path.Clean(base), tsMissing
}

// resolve turns a specifier written in the file from into what it names. For tsFound, tsMissing and
// tsUnread the result is a path of the repository; for tsExternal, the package's name.
func (p *tsProject) resolve(from, spec string) (string, tsKind) {
	switch {
	case strings.HasPrefix(spec, "."):
		return p.lookup(path.Join(path.Dir(from), spec))
	case strings.HasPrefix(spec, "/"):
		return p.lookup(strings.TrimPrefix(spec, "/"))
	}
	if c := p.config(path.Dir(from)); c != nil {
		if targets, ok := c.match(spec); ok {
			for _, t := range targets {
				if f, kind := p.lookup(t); kind == tsFound {
					return f, kind
				}
			}
			return p.lookup(targets[0])
		}
		if c.hasBase {
			if f := p.file(path.Join(c.baseURL, spec)); f != "" {
				return f, tsFound
			}
		}
	}
	if pkg, rest, ok := p.pkg(spec); ok {
		return p.inPackage(pkg, rest)
	}
	parts := strings.SplitN(spec, "/", 3)
	if strings.HasPrefix(spec, "@") && len(parts) > 1 {
		return parts[0] + "/" + parts[1], tsExternal
	}
	return parts[0], tsExternal
}

// pkg finds the repository's package a specifier names, and the path after its name.
func (p *tsProject) pkg(spec string) (tsPackage, string, bool) {
	for _, pkg := range p.packages {
		if spec == pkg.name {
			return pkg, "", true
		}
		if rest, ok := strings.CutPrefix(spec, pkg.name+"/"); ok {
			return pkg, rest, true
		}
	}
	return tsPackage{}, "", false
}

// inPackage resolves a path inside a package of the repository to its source.
func (p *tsProject) inPackage(pkg tsPackage, rest string) (string, tsKind) {
	var tries []string
	if rest != "" {
		tries = []string{path.Join(pkg.dir, rest), path.Join(pkg.dir, "src", rest)}
	} else {
		tries = []string{path.Join(pkg.dir, "src/index"), path.Join(pkg.dir, "index")}
		for _, e := range pkg.entries {
			if e == "" {
				continue
			}
			e = path.Clean(e)
			tries = append(tries, path.Join(pkg.dir, e))
			for _, out := range []string{"dist/", "lib/", "build/", "out/"} {
				if built, ok := strings.CutPrefix(e, out); ok {
					tries = append(tries, path.Join(pkg.dir, "src", strings.TrimSuffix(built, path.Ext(built))))
				}
			}
		}
	}
	for _, t := range tries {
		if f := p.file(t); f != "" {
			return f, tsFound
		}
	}
	return tries[0], tsMissing
}
