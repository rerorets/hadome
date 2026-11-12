package mutation

import (
	"slices"
	"testing"

	"github.com/rerorets/hadome/internal/facts"
)

// facts of a small version: store is imported by api, api by worker/queue, and other stands apart;
// dyn imports something decided at run time.
func reachFacts() *facts.Analysis {
	file := func(p, unit string) facts.File { return facts.File{Path: p, Unit: unit} }
	sym := func(id, p string) facts.Symbol { return facts.Symbol{ID: id, Path: p, Kind: facts.SymbolFunction} }
	calls := func(name, p string, targets ...string) facts.Test {
		t := facts.Test{Name: name, Path: p}
		for _, tg := range targets {
			t.Calls = append(t.Calls, facts.Call{Target: tg})
		}
		return t
	}
	skipped := calls("TestSkipped", "api/skip_test.go", "api.Handle")
	skipped.Skip.Skipped = true
	return &facts.Analysis{
		Files: []facts.File{
			file("store/store.go", "store"), file("store/store_test.go", "store"),
			file("api/api.go", "api"), file("api/api_test.go", "api"), file("api/skip_test.go", "api"),
			file("worker/queue/queue.go", "worker/queue"), file("worker/queue/queue_test.go", "worker/queue"),
			file("other/other.go", "other"), file("other/other_test.go", "other"),
			file("dyn/dyn.go", "dyn"), file("dyn/dyn_test.go", "dyn"),
		},
		Symbols: []facts.Symbol{
			sym("store.Append", "store/store.go"), sym("store.Drop", "store/store.go"),
			sym("api.Handle", "api/api.go"), sym("worker/queue.Run", "worker/queue/queue.go"),
			sym("other.One", "other/other.go"), sym("dyn.Load", "dyn/dyn.go"),
		},
		Imports: []facts.Import{
			{Path: "api/api.go", FromUnit: "api", ToUnit: "store", Internal: true},
			{Path: "worker/queue/queue.go", FromUnit: "worker/queue", ToUnit: "api", Internal: true},
			{Path: "other/other.go", FromUnit: "other", ToUnit: "fmt"},
			{Path: "dyn/dyn.go", FromUnit: "dyn", Unknown: true, Text: "load(name)"},
		},
		Tests: []facts.Test{
			calls("TestInside", "store/store_test.go"),
			calls("TestHandle", "api/api_test.go", "api.Handle"),
			skipped,
			calls("TestRun", "worker/queue/queue_test.go", "worker/queue.Run"),
			calls("TestOne", "other/other_test.go", "other.One"),
			calls("TestLoad", "dyn/dyn_test.go", "dyn.Load"),
		},
	}
}

func testNames(ts []facts.Test) []string {
	var out []string
	for _, t := range ts {
		out = append(out, t.Name)
	}
	return out
}

// viewpoint: behaviour
// target: mutation.reach
// The tests that reach a function are the ones that are not skipped and call a function of a unit
// that imports the function's unit, through any number of units, or are in such a unit. A unit that
// does not import it, or imports something decided at run time, does not reach it.
func TestTheTestsThatReachAFunctionFollowImportsBackwards(t *testing.T) {
	r := reaching(reachFacts())
	for _, tc := range []struct {
		fn   string
		want []string
	}{
		{"store.Append", []string{"TestHandle", "TestInside", "TestRun"}},
		{"api.Handle", []string{"TestHandle", "TestRun"}},
		{"worker/queue.Run", []string{"TestRun"}},
		{"other.One", []string{"TestOne"}},
		{"dyn.Load", []string{"TestLoad"}},
		{"fmt.Println", nil},
	} {
		if got := testNames(r.of(tc.fn)); !slices.Equal(got, tc.want) {
			t.Errorf("%s: want %v, got %v", tc.fn, tc.want, got)
		}
	}
}

// viewpoint: invariant
// target: mutation.stages
// Stage 2 is one command per unit: every function of a unit gets the same one, whatever tests call
// each function directly.
func TestStageTwoIsOnePerUnit(t *testing.T) {
	a := reachFacts()
	a.Tests = append(a.Tests, facts.Test{Name: "TestDrop", Path: "api/drop_test.go", Calls: []facts.Call{{Target: "store.Drop"}}})
	a.Files = append(a.Files, facts.File{Path: "api/drop_test.go", Unit: "api"})
	pick := picker(a)
	test := []string{"go", "test", "{packages}", "-run", "^({tests})$"}
	append1, drop := stages(test, "|", pick("store.Append")), stages(test, "|", pick("store.Drop"))
	stage2 := func(cmds [][]string) []string {
		for _, c := range cmds {
			if !slices.Contains(c, "-run") && !slices.Contains(c, "./...") {
				return c
			}
		}
		return nil
	}
	if got, want := stage2(append1), []string{"go", "test", "./api", "./store", "./worker/queue"}; !slices.Equal(got, want) {
		t.Errorf("store.Append's stage 2 %q, want %q", got, want)
	}
	if !slices.Equal(stage2(append1), stage2(drop)) {
		t.Errorf("two functions of store got stage 2 %q and %q", stage2(append1), stage2(drop))
	}
}
