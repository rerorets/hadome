package facts

import (
	"go/build/constraint"
	"strings"
)

// Build conditions of Go files (File.Build), and whether two files can be built together
// (docs/design/adapters.md, "Build conditions").

// goos and goarch are the operating systems and architectures a Go file name or build condition
// may name. Two values of one are never true at once.
var (
	goos = set("aix", "android", "darwin", "dragonfly", "freebsd", "hurd", "illumos", "ios", "js", "linux", "nacl",
		"netbsd", "openbsd", "plan9", "solaris", "wasip1", "windows", "zos")
	goarch = set("386", "amd64", "amd64p32", "arm", "arm64", "arm64be", "armbe", "loong64", "mips", "mips64",
		"mips64le", "mips64p32", "mips64p32le", "mipsle", "ppc", "ppc64", "ppc64le", "riscv", "riscv64", "s390",
		"s390x", "sparc", "sparc64", "wasm")
)

// maxBuildTags is how many tags two conditions may name before every set of them is no longer
// tried, and the files are taken as able to be built together.
const maxBuildTags = 12

func set(names ...string) map[string]bool {
	out := map[string]bool{}
	for _, n := range names {
		out[n] = true
	}
	return out
}

// GoBuild returns File.Build of a Go file: expr is its //go:build expression, "" for none, and name
// its base name, whose _GOOS, _GOARCH or _GOOS_GOARCH ending, before _test, is a condition too.
func GoBuild(name, expr string) string {
	var parts []string
	if expr != "" {
		parts = append(parts, "("+expr+")")
	}
	elems := strings.Split(strings.TrimSuffix(strings.TrimSuffix(name, ".go"), "_test"), "_")
	n := len(elems)
	switch {
	case n > 2 && goos[elems[n-2]] && goarch[elems[n-1]]:
		parts = append(parts, elems[n-2], elems[n-1])
	case n > 1 && (goos[elems[n-1]] || goarch[elems[n-1]]):
		parts = append(parts, elems[n-1])
	}
	return strings.Join(parts, " && ")
}

// BuildTogether reports whether two files with these conditions can be in one build: whether some
// set of tags, with at most one GOOS and one GOARCH true, satisfies both. An empty condition is
// always satisfied. Too many tags, or a condition that does not parse, count as able to.
func BuildTogether(a, b string) bool {
	if a == "" || b == "" {
		return true
	}
	ea, errA := constraint.Parse("//go:build " + a)
	eb, errB := constraint.Parse("//go:build " + b)
	if errA != nil || errB != nil {
		return true
	}
	var tags []string
	seen := map[string]bool{}
	collectTags(ea, &tags, seen)
	collectTags(eb, &tags, seen)
	if len(tags) > maxBuildTags {
		return true
	}
	return satisfied(tags, map[string]bool{}, func(has func(string) bool) bool { return ea.Eval(has) && eb.Eval(has) })
}

// satisfied reports whether some setting of tags, after those already in on, with at most one GOOS
// and one GOARCH true, makes ok true. Each tag is tried false, then true when that keeps the limit.
func satisfied(tags []string, on map[string]bool, ok func(func(string) bool) bool) bool {
	if len(tags) == 0 {
		return ok(func(t string) bool { return on[t] })
	}
	t, rest := tags[0], tags[1:]
	on[t] = false
	if satisfied(rest, on, ok) {
		return true
	}
	for o, v := range on {
		if v && (goos[t] && goos[o] || goarch[t] && goarch[o]) {
			return false
		}
	}
	on[t] = true
	found := satisfied(rest, on, ok)
	on[t] = false
	return found
}

func collectTags(e constraint.Expr, into *[]string, seen map[string]bool) {
	switch e := e.(type) {
	case *constraint.TagExpr:
		if !seen[e.Tag] {
			seen[e.Tag] = true
			*into = append(*into, e.Tag)
		}
	case *constraint.NotExpr:
		collectTags(e.X, into, seen)
	case *constraint.AndExpr:
		collectTags(e.X, into, seen)
		collectTags(e.Y, into, seen)
	case *constraint.OrExpr:
		collectTags(e.X, into, seen)
		collectTags(e.Y, into, seen)
	}
}
