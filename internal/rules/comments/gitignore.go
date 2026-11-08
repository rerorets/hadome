package comments

import (
	"io/fs"
	"path"
	"regexp"
	"strings"
)

// ignoreRule is one pattern line of a .gitignore file, matched against a path relative to the
// directory of that file.
type ignoreRule struct {
	re      *regexp.Regexp
	negate  bool
	dirOnly bool
}

// gitignore answers whether a version's .gitignore files ignore a path. It reads only the
// .gitignore files inside the version: .git/info/exclude and the user's core.excludesFile differ
// between machines, and the result must not.
type gitignore struct {
	fsys  fs.FS
	rules map[string][]ignoreRule // by directory, "" for the root
}

func newGitignore(fsys fs.FS) *gitignore {
	return &gitignore{fsys: fsys, rules: map[string][]ignoreRule{}}
}

// ignored reports whether the file at p, slash-separated and relative to the root, is ignored. As
// in git, a file in an ignored directory is ignored whatever the patterns say about the file.
func (g *gitignore) ignored(p string) bool {
	parts := strings.Split(p, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	for i := 1; i <= len(parts); i++ {
		if g.matches(parts[:i], i < len(parts)) {
			return true
		}
	}
	return false
}

// matches applies the .gitignore files from the root down to the parent of the path made of
// parts. A deeper file overrides a shallower one and a later line overrides an earlier one, so the
// last matching rule decides.
func (g *gitignore) matches(parts []string, isDir bool) bool {
	result := false
	for d := 0; d < len(parts); d++ {
		dir := strings.Join(parts[:d], "/")
		rel := strings.Join(parts[d:], "/")
		for _, r := range g.load(dir) {
			if r.dirOnly && !isDir {
				continue
			}
			if r.re.MatchString(rel) {
				result = !r.negate
			}
		}
	}
	return result
}

func (g *gitignore) load(dir string) []ignoreRule {
	if rules, ok := g.rules[dir]; ok {
		return rules
	}
	var rules []ignoreRule
	if b, err := fs.ReadFile(g.fsys, path.Join(dir, ".gitignore")); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			if r, ok := parseIgnoreLine(l); ok {
				rules = append(rules, r)
			}
		}
	}
	g.rules[dir] = rules
	return rules
}

// parseIgnoreLine reads one line of a .gitignore file. It reports false for blank lines, comments
// and patterns it cannot read.
func parseIgnoreLine(l string) (ignoreRule, bool) {
	l = strings.TrimSuffix(l, "\r")
	for strings.HasSuffix(l, " ") && !strings.HasSuffix(l, "\\ ") {
		l = l[:len(l)-1]
	}
	if l == "" || strings.HasPrefix(l, "#") || loneBackslashAtEnd(l) {
		return ignoreRule{}, false
	}
	var r ignoreRule
	if strings.HasPrefix(l, "!") {
		r.negate, l = true, l[1:]
	}
	if strings.HasSuffix(l, "/") {
		r.dirOnly, l = true, strings.TrimSuffix(l, "/")
	}
	anchored := strings.Contains(l, "/")
	l = strings.TrimPrefix(l, "/")
	if l == "" {
		return ignoreRule{}, false
	}
	body := globRegexp(l)
	if !anchored {
		body = "(?:.*/)?" + body
	}
	re, err := regexp.Compile("^" + body + "$")
	if err != nil {
		return ignoreRule{}, false
	}
	r.re = re
	return r, true
}

// loneBackslashAtEnd reports whether l ends in a backslash that escapes nothing. git never matches
// such a pattern.
func loneBackslashAtEnd(l string) bool {
	for strings.HasSuffix(l, `\\`) {
		l = l[:len(l)-2]
	}
	return strings.HasSuffix(l, `\`)
}

// globRegexp translates a slash-separated gitignore pattern. A segment that is exactly ** matches
// zero or more directories; elsewhere * and ? stop at a slash.
func globRegexp(pattern string) string {
	segs := strings.Split(pattern, "/")
	var b strings.Builder
	for i, s := range segs {
		last := i == len(segs)-1
		if s == "**" {
			if last {
				b.WriteString(".*")
			} else {
				b.WriteString("(?:.*/)?")
			}
			continue
		}
		b.WriteString(segmentRegexp(s))
		if !last {
			b.WriteString("/")
		}
	}
	return b.String()
}

func segmentRegexp(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '*':
			b.WriteString("[^/]*")
		case '?':
			b.WriteString("[^/]")
		case '\\': // never last: parseIgnoreLine drops a pattern ending in a lone backslash
			i++
			b.WriteString(regexp.QuoteMeta(s[i : i+1]))
		case '[':
			end := strings.IndexByte(s[i+1:], ']')
			if end < 0 {
				b.WriteString(`\[`)
				continue
			}
			class := s[i+1 : i+1+end]
			i += end + 1
			if strings.HasPrefix(class, "!") || strings.HasPrefix(class, "^") {
				b.WriteString("[^/" + class[1:] + "]")
			} else {
				b.WriteString("[" + class + "]")
			}
		default:
			b.WriteString(regexp.QuoteMeta(s[i : i+1]))
		}
	}
	return b.String()
}
