package config

import (
	"path"
	"strings"
)

// MatchesAny reports whether a slash-separated path matches one of the globs. A glob's elements are
// matched with path.Match, and "**" matches any number of directories.
func MatchesAny(globs []string, p string) bool {
	for _, g := range globs {
		if match(strings.Split(g, "/"), strings.Split(p, "/")) {
			return true
		}
	}
	return false
}

func match(glob, elems []string) bool {
	if len(glob) == 0 {
		return len(elems) == 0
	}
	if glob[0] == "**" {
		for i := 0; i <= len(elems); i++ {
			if match(glob[1:], elems[i:]) {
				return true
			}
		}
		return false
	}
	if len(elems) == 0 {
		return false
	}
	ok, err := path.Match(glob[0], elems[0])
	return err == nil && ok && match(glob[1:], elems[1:])
}

// validGlob reports whether every element of a glob is a pattern path.Match accepts. A glob it does
// not accept would match nothing, without a word.
func validGlob(g string) bool {
	for _, e := range strings.Split(g, "/") {
		if _, err := path.Match(e, ""); err != nil {
			return false
		}
	}
	return true
}
