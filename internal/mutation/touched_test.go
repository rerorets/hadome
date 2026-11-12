package mutation

import (
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
)

// version is a version whose files each hold Go functions, one per line, and the facts that list
// every one of them as a function with id: what an adapter reports for functions of the same name in
// files split by build tags, or for a package's init functions.
func version(id string, files map[string]string) (*facts.Analysis, engine.Version) {
	tree := fstest.MapFS{}
	a := &facts.Analysis{}
	for path, src := range files {
		tree[path] = &fstest.MapFile{Data: []byte(src)}
		a.Files = append(a.Files, facts.File{Path: path, Role: facts.RoleSource})
		for i, line := range strings.Split(src, "\n") {
			if strings.HasPrefix(line, "func ") {
				a.Symbols = append(a.Symbols, facts.Symbol{ID: id, Kind: facts.SymbolFunction, Path: path, Line: i + 1, EndLine: i + 1})
			}
		}
	}
	return a, engine.Version{FS: tree}
}

// places are where the selected functions are.
func places(syms []facts.Symbol) []string {
	var out []string
	for _, s := range syms {
		out = append(out, s.Path+":"+strconv.Itoa(s.Line))
	}
	slices.Sort(out)
	return out
}

// viewpoint: behaviour
// target: mutation.touched
// Design "Where to break": functions sharing an ID are matched with base by their body too. With
// functions of the same name in files split by build tags, and with two init functions in a file,
// none is selected while none changed, and changing one selects that one alone. Each base function
// matches one head function: a copy of an unchanged function is new.
func TestFunctionsSharingAnIDAreMatchedByTheirBody(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		id         string
		base, head map[string]string
		want       []string
	}{
		{"build tags, unchanged", "app.run",
			map[string]string{"run_unix.go": "package app\nfunc run() { unix() }\n", "run_windows.go": "package app\nfunc run() { windows() }\n"},
			map[string]string{"run_unix.go": "package app\nfunc run() { unix() }\n", "run_windows.go": "package app\nfunc run() { windows() }\n"},
			nil},
		{"build tags, one changed", "app.run",
			map[string]string{"run_unix.go": "package app\nfunc run() { unix() }\n", "run_windows.go": "package app\nfunc run() { windows() }\n"},
			map[string]string{"run_unix.go": "package app\nfunc run() { unix(); again() }\n", "run_windows.go": "package app\nfunc run() { windows() }\n"},
			[]string{"run_unix.go:2"}},
		{"two init functions, unchanged", "app.init",
			map[string]string{"app.go": "package app\nfunc init() { first() }\nfunc init() { second() }\n"},
			map[string]string{"app.go": "package app\nfunc init() { first() }\nfunc init() { second() }\n"},
			nil},
		{"two init functions, the second changed", "app.init",
			map[string]string{"app.go": "package app\nfunc init() { first() }\nfunc init() { second() }\n"},
			map[string]string{"app.go": "package app\nfunc init() { first() }\nfunc init() { second(); third() }\n"},
			[]string{"app.go:3"}},
		{"an init function copied", "app.init",
			map[string]string{"app.go": "package app\nfunc init() { first() }\n"},
			map[string]string{"app.go": "package app\nfunc init() { first() }\nfunc init() { first() }\n"},
			[]string{"app.go:3"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ba, bv := version(tc.id, tc.base)
			ha, hv := version(tc.id, tc.head)
			if got := places(touched(ba, ha, bv, hv)); !slices.Equal(got, tc.want) {
				t.Errorf("selected %v, want %v", got, tc.want)
			}
		})
	}
}

// viewpoint: behaviour
// target: mutation.touched
// Design "Where to break": an unchanged function is selected when a test the change rewrote calls
// it, and not when the test is as it was. A test whose body hadome has no fingerprint of counts as
// rewritten, since whether it changed cannot be told.
func TestFunctionsCalledByRewrittenTestsAreSelected(t *testing.T) {
	t.Parallel()
	files := map[string]string{"app.go": "package app\nfunc run() { work() }\n"}
	for _, tc := range []struct {
		name               string
		baseBody, headBody string
		want               []string
	}{
		{"the test as it was", "check run", "check run", nil},
		{"the test rewritten", "check run", "check run less", []string{"app.go:2"}},
		{"a test without a fingerprint", "", "", []string{"app.go:2"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ba, bv := version("app.run", files)
			ha, hv := version("app.run", files)
			ba.Tests = []facts.Test{{Name: "TestRun", Path: "app_test.go", Body: tc.baseBody, Calls: []facts.Call{{Target: "app.run"}}}}
			ha.Tests = []facts.Test{{Name: "TestRun", Path: "app_test.go", Body: tc.headBody, Calls: []facts.Call{{Target: "app.run"}}}}
			if got := places(touched(ba, ha, bv, hv)); !slices.Equal(got, tc.want) {
				t.Errorf("selected %v, want %v", got, tc.want)
			}
		})
	}
}
