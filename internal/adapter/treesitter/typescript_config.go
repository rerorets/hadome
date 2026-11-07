//go:build cgo

package treesitter

import (
	"encoding/json"
	"io/fs"
	"path"
	"strings"
)

// How the TypeScript reader reads a tsconfig.json: what it extends, its baseUrl and paths, and the
// configs a solution-style one references.

// config returns the config of the tsconfig.json nearest to a directory, or nil.
func (p *tsProject) config(dir string) *tsConfig {
	if c, ok := p.configs[dir]; ok {
		return c
	}
	var c *tsConfig
	name := path.Join(dir, "tsconfig.json")
	if st, err := fs.Stat(p.fsys, name); err == nil && !st.IsDir() {
		c = p.load(name, 0)
		if c != nil && !c.hasPaths {
			c = p.referenced(name, c)
		}
	} else if dir != "." {
		c = p.config(path.Dir(dir))
	}
	p.configs[dir] = c
	return c
}

// tsRaw is the part of a tsconfig.json resolving needs.
type tsRaw struct {
	Extends         json.RawMessage `json:"extends"`
	CompilerOptions struct {
		BaseURL *string             `json:"baseUrl"`
		Paths   map[string][]string `json:"paths"`
	} `json:"compilerOptions"`
	References []struct {
		Path string `json:"path"`
	} `json:"references"`
}

func (p *tsProject) raw(name string) (tsRaw, bool) {
	var raw tsRaw
	data, err := fs.ReadFile(p.fsys, name)
	if err != nil || json.Unmarshal(jsonc(data), &raw) != nil {
		return raw, false
	}
	return raw, true
}

// load reads a config file and what it extends; depth stops a cycle of extends.
func (p *tsProject) load(name string, depth int) *tsConfig {
	raw, ok := p.raw(name)
	if !ok || depth > 8 {
		return nil
	}
	c := &tsConfig{}
	var parents []string
	var one string
	if json.Unmarshal(raw.Extends, &one) == nil {
		parents = []string{one}
	} else {
		_ = json.Unmarshal(raw.Extends, &parents) // absent or malformed: nothing to extend
	}
	for _, e := range parents {
		if file := p.extended(path.Dir(name), e); file != "" {
			if parent := p.load(file, depth+1); parent != nil {
				c.merge(parent)
			}
		}
	}
	dir := path.Dir(name)
	if b := raw.CompilerOptions.BaseURL; b != nil {
		c.baseURL, c.hasBase = path.Join(dir, *b), true
	}
	if raw.CompilerOptions.Paths != nil {
		c.paths, c.pathsDir, c.hasPaths = raw.CompilerOptions.Paths, dir, true
	}
	return c
}

func (c *tsConfig) merge(from *tsConfig) {
	if from.hasBase {
		c.baseURL, c.hasBase = from.baseURL, true
	}
	if from.hasPaths {
		c.paths, c.pathsDir, c.hasPaths = from.paths, from.pathsDir, true
	}
}

// extended finds the file an extends entry names: a relative path, or a file inside a package of
// the repository. A package from node_modules is not read.
func (p *tsProject) extended(dir, spec string) string {
	file := ""
	if strings.HasPrefix(spec, ".") {
		file = path.Join(dir, spec)
	} else if pkg, rest, ok := p.pkg(spec); ok && rest != "" {
		file = path.Join(pkg.dir, rest)
	}
	if file != "" && !strings.HasSuffix(file, ".json") {
		file += ".json"
	}
	return file
}

// referenced takes the aliases of the first config a solution-style tsconfig.json references
// that has some (Vite's tsconfig.app.json).
func (p *tsProject) referenced(name string, c *tsConfig) *tsConfig {
	raw, _ := p.raw(name)
	for _, ref := range raw.References {
		file := path.Join(path.Dir(name), ref.Path)
		if !strings.HasSuffix(file, ".json") {
			file = path.Join(file, "tsconfig.json")
		}
		if r := p.load(file, 1); r != nil && r.hasPaths {
			return r
		}
	}
	return c
}

// match returns the destinations of the alias that matches spec, with the matched part put in
// place of the wildcard. The alias with the longest prefix wins, as in the TypeScript compiler.
func (c *tsConfig) match(spec string) ([]string, bool) {
	if !c.hasPaths {
		return nil, false
	}
	base := c.pathsDir
	if c.hasBase {
		base = c.baseURL
	}
	best, bestLen, star := "", -1, ""
	for alias := range c.paths {
		n, s, ok := aliasMatch(alias, spec)
		if ok && (n > bestLen || (n == bestLen && alias < best)) {
			best, bestLen, star = alias, n, s
		}
	}
	if bestLen < 0 || len(c.paths[best]) == 0 {
		return nil, false
	}
	var out []string
	for _, t := range c.paths[best] {
		out = append(out, path.Join(base, strings.Replace(t, "*", star, 1)))
	}
	return out, true
}

// aliasMatch reports whether an alias ("@/*", or one without a wildcard) matches spec, how long
// its prefix is, and what the wildcard stands for.
func aliasMatch(alias, spec string) (int, string, bool) {
	prefix, suffix, wild := strings.Cut(alias, "*")
	if !wild {
		return len(alias), "", spec == alias
	}
	if !strings.HasPrefix(spec, prefix) || !strings.HasSuffix(spec, suffix) || len(spec) < len(prefix)+len(suffix) {
		return 0, "", false
	}
	return len(prefix), spec[len(prefix) : len(spec)-len(suffix)], true
}

// jsonc turns JSON with comments and trailing commas, as tsconfig.json allows, into JSON.
func jsonc(data []byte) []byte {
	return withoutTrailingCommas(withoutComments(data))
}

// withoutComments drops the // and /* */ comments of JSON text.
func withoutComments(data []byte) []byte {
	var out []byte
	scanJSON(data, func(i int, c byte) int {
		switch {
		case c == '/' && i+1 < len(data) && data[i+1] == '/':
			for i < len(data) && data[i] != '\n' {
				i++
			}
			return i
		case c == '/' && i+1 < len(data) && data[i+1] == '*':
			end := strings.Index(string(data[i+2:]), "*/")
			if end < 0 {
				return len(data)
			}
			return i + 2 + end + 2
		}
		out = append(out, c)
		return i + 1
	}, func(c byte) { out = append(out, c) })
	return out
}

// withoutTrailingCommas drops a comma that only whitespace separates from the } or ] after it.
func withoutTrailingCommas(data []byte) []byte {
	var out []byte
	scanJSON(data, func(i int, c byte) int {
		if c == ',' {
			rest := strings.TrimLeft(string(data[i+1:]), " \t\r\n")
			if strings.HasPrefix(rest, "}") || strings.HasPrefix(rest, "]") {
				return i + 1
			}
		}
		out = append(out, c)
		return i + 1
	}, func(c byte) { out = append(out, c) })
	return out
}

// scanJSON walks JSON text: each byte inside a string, quotes included, goes to str; any other
// byte goes to code, which returns where to go on.
func scanJSON(data []byte, code func(i int, c byte) int, str func(c byte)) {
	for i := 0; i < len(data); {
		if data[i] != '"' {
			i = code(i, data[i])
			continue
		}
		str(data[i])
		for i++; i < len(data); i++ {
			str(data[i])
			if data[i] == '\\' && i+1 < len(data) {
				i++
				str(data[i])
			} else if data[i] == '"' {
				i++
				break
			}
		}
	}
}
