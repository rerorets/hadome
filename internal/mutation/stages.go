package mutation

import (
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rerorets/hadome/internal/facts"
)

// picked are the tests a mutant's stages are made from.
type picked struct {
	calling  []facts.Test // the tests that call the broken function
	reaching []facts.Test // the tests that reach it (see reach)
	all      []facts.Test // every test of the version, skipped ones included: what stage 3 runs
}

// stages are the commands that judge a mutant, narrowest first and without repeats (docs/design/
// mutation.md, "Narrowing the tests run"):
//  1. the tests that call the broken function, in their packages
//  2. the tests that reach it: their packages whole, or by name in every package when they cannot
//     be narrowed by package; left out when that is all the tests
//  3. every test
//
// Each of stages 1 and 2 runs a part of the tests stage 3 runs, so they catch only what stage 3
// catches.
func stages(test []string, sep string, p picked) [][]string {
	var out [][]string
	add := func(cmd []string) {
		for _, c := range out {
			if slices.Equal(c, cmd) {
				return
			}
		}
		out = append(out, cmd)
	}
	if hasPackages(test) && len(p.calling) > 0 {
		if pkgs := goPackages(test, p.calling); len(pkgs) > 0 {
			add(fill(test, sep, names(p.calling), pkgs))
		}
	}
	if len(p.reaching) > 0 {
		pkgs := []string(nil)
		if hasPackages(test) {
			pkgs = goPackages(test, p.reaching)
		}
		switch {
		case len(pkgs) > 0:
			if !slices.Equal(pkgs, goPackages(test, p.all)) {
				add(fill(test, sep, nil, pkgs))
			}
		case !covers(names(p.reaching), names(p.all)):
			add(fill(test, sep, names(p.reaching), nil))
		}
	}
	add(fill(test, sep, nil, nil))
	return out
}

// names are the tests' names, each once, sorted.
func names(tests []facts.Test) []string {
	var out []string
	for _, t := range tests {
		if !slices.Contains(out, t.Name) {
			out = append(out, t.Name)
		}
	}
	slices.Sort(out)
	return out
}

// covers reports whether some holds every one of all: names that pick out every test.
func covers(some, all []string) bool {
	for _, n := range all {
		if !slices.Contains(some, n) {
			return false
		}
	}
	return true
}

// fill puts the tests and the packages into the command. No tests takes {tests} out: the element
// that holds it, and the flag before it. No packages puts every package in place of {packages}.
func fill(test []string, sep string, tests, pkgs []string) []string {
	var out []string
	for i, a := range test {
		switch {
		case a == packagesHole && len(pkgs) == 0:
			out = append(out, allPackages)
		case a == packagesHole:
			out = append(out, pkgs...)
		case strings.Contains(a, testsHole) && len(tests) == 0:
			if i > 0 && strings.HasPrefix(test[i-1], "-") && len(out) > 0 {
				out = out[:len(out)-1]
			}
		case strings.Contains(a, testsHole):
			out = append(out, strings.ReplaceAll(a, testsHole, strings.Join(tests, sep)))
		default:
			out = append(out, a)
		}
	}
	return out
}

// hasPackages reports whether the command narrows by package.
func hasPackages(test []string) bool { return slices.Contains(test, packagesHole) }

// goPackages are the Go packages of the tests, as go test names them from the directory it runs in:
// the repository root, or the directory of -C in the command. It returns nil when a test is not a Go
// test or is outside that directory: narrowing by package would leave it out.
func goPackages(test []string, tests []facts.Test) []string {
	root, ok := goDir(test)
	if !ok {
		return nil
	}
	var out []string
	for _, t := range tests {
		if !strings.HasSuffix(t.Path, "_test.go") {
			return nil
		}
		dir := path.Dir(t.Path)
		switch {
		case root == ".":
		case dir == root:
			dir = "."
		case strings.HasPrefix(dir, root+"/"):
			dir = strings.TrimPrefix(dir, root+"/")
		default:
			return nil
		}
		pkg := "./" + dir
		if dir == "." {
			pkg = "."
		}
		if !slices.Contains(out, pkg) {
			out = append(out, pkg)
		}
	}
	slices.Sort(out)
	return out
}

// goDir is the directory, relative to the root of the copy, that the command's -C makes go run in.
// It is not ok when that directory cannot be seen from the root.
func goDir(test []string) (string, bool) {
	dir := "."
	for i, a := range test {
		var v string
		switch {
		case a == "-C" && i+1 < len(test):
			v = test[i+1]
		case strings.HasPrefix(a, "-C="):
			v = strings.TrimPrefix(a, "-C=")
		default:
			continue
		}
		if filepath.IsAbs(v) || path.IsAbs(filepath.ToSlash(v)) {
			return "", false
		}
		if dir = path.Join(dir, filepath.ToSlash(v)); dir == ".." || strings.HasPrefix(dir, "../") {
			return "", false
		}
	}
	return dir, true
}
