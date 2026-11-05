package golang

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
)

// viewpoint: behaviour
// target: golang.Adapter.Analyze
// The facts rules see from a small module: files and units, imports, symbols, and a test's calls,
// scopes, hazards and declarations, including what a helper and a loop contribute.
func TestGoFacts(t *testing.T) {
	tree := fstest.MapFS{
		"go.mod": {Data: []byte("module example.com/app\n")},
		"store/store.go": {Data: []byte(`package store

import "database/sql"

// Rows returns one user's rows.
func Rows(db *sql.DB, userID string) []string { if db == nil || userID == "" { return nil }; return nil }

func Append() {}
`)},
		"store/broken.go": {Data: []byte("package store\n\nfunc (\n")},
		"store/store_test.go": {Data: []byte(`package store_test

import (
	"testing"
	"time"

	"example.com/app/store"
)

func read(user string) { store.Rows(nil, user) }

// guards: rows-1
func TestRows(t *testing.T) {
	read("alice")
	read("bob")
	for range 3 {
		store.Append()
	}
	time.Sleep(time.Millisecond)
}
`)},
	}
	paths := []string{"store/broken.go", "store/store.go", "store/store_test.go"}
	a, err := Adapter{}.Analyze(engine.Version{FS: tree}, config.Config{}, paths)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range a.Files {
		got = append(got, fmt.Sprintf("file %s %s unit=%s at=%s unread=%t", f.Path, f.Role, f.Unit, f.UnitPath, f.Unread))
	}
	for _, im := range a.Imports {
		got = append(got, fmt.Sprintf("import %s -> %s io=%t", im.FromUnit, im.ToUnit, im.IO))
	}
	for _, s := range a.Symbols {
		got = append(got, fmt.Sprintf("symbol %s exported=%t doc=%t params=%d cx=%d scoped=%t", s.ID, s.Exported, s.Doc, s.Params, s.Complexity, s.Scoped))
	}
	for _, ts := range a.Tests {
		for _, c := range ts.Calls {
			got = append(got, fmt.Sprintf("call %s %s ×%d scopes=%d", ts.Name, c.Target, c.Times, c.Scopes))
		}
		for _, h := range ts.Hazards {
			got = append(got, fmt.Sprintf("hazard %s %s", ts.Name, h.Category))
		}
		got = append(got, fmt.Sprintf("declared %s %v", ts.Name, ts.Declared["guards"]))
	}
	sort.Strings(got)
	want := strings.Join([]string{
		"call TestRows example.com/app/store.Append ×2 scopes=0",
		"call TestRows example.com/app/store.Rows ×2 scopes=2",
		"declared TestRows [rows-1]",
		"file store/broken.go other unit= at= unread=true",
		"file store/store.go source unit=example.com/app/store at=store unread=false",
		"file store/store_test.go test unit=example.com/app/store at=store unread=false",
		"hazard TestRows sleep",
		"import example.com/app/store -> database/sql io=true",
		"symbol example.com/app/store.Append exported=true doc=false params=0 cx=1 scoped=false",
		"symbol example.com/app/store.Rows exported=true doc=true params=2 cx=3 scoped=true",
	}, "\n")
	if g := strings.Join(got, "\n"); g != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", g, want)
	}
}

// viewpoint: behaviour
// target: golang.Adapter.Analyze
// A call into the repository that hadome cannot name counts as unresolved, so that a test is never
// taken for one that calls no code of the repository. A conversion, a builtin and the standard
// library count as neither.
func TestGoUnresolvedCalls(t *testing.T) {
	tree := fstest.MapFS{
		"go.mod":         {Data: []byte("module example.com/app\n")},
		"store/store.go": {Data: []byte("package store\n\ntype ID string\n\nvar Default = func() {}\n")},
		"store/out_test.go": {Data: []byte(`package store_test

import (
	"strings"
	"testing"

	"example.com/app/gen"
	"example.com/app/store"
)

func TestOut(t *testing.T) {
	store.Default()
	gen.Make()
	_ = store.ID("a")
	_ = strings.ToUpper("a")
}
`)},
		"store/in_test.go": {Data: []byte(`package store

import "testing"

func TestIn(t *testing.T) {
	Default()
	_ = len("a")
}
`)},
	}
	paths := []string{"store/in_test.go", "store/out_test.go", "store/store.go"}
	a, err := Adapter{}.Analyze(engine.Version{FS: tree}, config.Config{}, paths)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, ts := range a.Tests {
		got = append(got, fmt.Sprintf("%s calls=%d unresolved=%d", ts.Name, len(ts.Calls), ts.Unresolved))
	}
	sort.Strings(got)
	if g, want := strings.Join(got, "\n"), "TestIn calls=0 unresolved=1\nTestOut calls=0 unresolved=2"; g != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", g, want)
	}
}

// viewpoint: behaviour
// target: golang.Adapter.Analyze
// A use of a product constant, variable or function that is not a call counts as a read. A type,
// a field of a local value and a call do not.
func TestGoReads(t *testing.T) {
	tree := fstest.MapFS{
		"go.mod": {Data: []byte("module example.com/app\n")},
		"store/store.go": {Data: []byte(`package store

type Store struct{ Limit int }

const Limit = 10

var Names = []string{"a"}

func Less(a, b string) bool { return a < b }

func New() *Store { return &Store{} }
`)},
		"store/out_test.go": {Data: []byte(`package store_test

import (
	"sort"
	"testing"

	"example.com/app/store"
)

func TestConst(t *testing.T) { _ = store.Limit }

func TestVar(t *testing.T) { _ = len(store.Names) }

func TestPassed(t *testing.T) { sort.Slice([]string{}, func(i, j int) bool { return store.Less("a", "b") }); _ = store.Less }

func TestType(t *testing.T) {
	var s store.Store
	_ = s.Limit
}

func TestCall(t *testing.T) { store.New() }
`)},
		"store/in_test.go": {Data: []byte(`package store

import "testing"

func TestIn(t *testing.T) {
	var s Store
	_ = s.Limit
	_ = Limit
}
`)},
	}
	paths := []string{"store/in_test.go", "store/out_test.go", "store/store.go"}
	a, err := Adapter{}.Analyze(engine.Version{FS: tree}, config.Config{}, paths)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, ts := range a.Tests {
		got = append(got, fmt.Sprintf("%s reads=%d", ts.Name, ts.Reads))
	}
	sort.Strings(got)
	want := "TestCall reads=0\nTestConst reads=1\nTestIn reads=1\nTestPassed reads=1\nTestType reads=0\nTestVar reads=1"
	if g := strings.Join(got, "\n"); g != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", g, want)
	}
}

// viewpoint: behaviour
// target: golang.Adapter.Analyze
// Tests whose bodies differ only in names, comments and whitespace share a fingerprint. A test
// with one different literal does not.
func TestGoBodies(t *testing.T) {
	tree := fstest.MapFS{
		"go.mod":         {Data: []byte("module example.com/app\n")},
		"store/store.go": {Data: []byte("package store\n\nfunc Add(a, b int) int { return a + b }\n")},
		"store/store_test.go": {Data: []byte(`package store

import "testing"

func TestAdd(t *testing.T) {
	if Add(1, 2) != 3 {
		t.Fatal("want 3")
	}
}

// TestAddAgain is a copy.
func TestAddAgain(t *testing.T) {
	// the same check
	if Add(1,2)!=3 { t.Fatal("want 3") }
}

func TestAddOther(t *testing.T) {
	if Add(1, 3) != 4 {
		t.Fatal("want 3")
	}
}
`)},
	}
	a, err := Adapter{}.Analyze(engine.Version{FS: tree}, config.Config{}, []string{"store/store.go", "store/store_test.go"})
	if err != nil {
		t.Fatal(err)
	}
	if g, want := sameBodies(a.Tests), "TestAdd TestAddAgain\nTestAddOther"; g != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", g, want)
	}
}

// sameBodies renders the tests grouped by Body, one sorted line per group. A test without a Body
// is shown as unknown.
func sameBodies(tests []facts.Test) string {
	groups := map[string][]string{}
	for _, ts := range tests {
		if ts.Body == "" {
			groups["?"+ts.Name] = []string{ts.Name + "=unknown"}
			continue
		}
		groups[ts.Body] = append(groups[ts.Body], ts.Name)
	}
	var lines []string
	for _, names := range groups {
		sort.Strings(names)
		lines = append(lines, strings.Join(names, " "))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// viewpoint: behaviour
// target: golang.Adapter.Analyze
// Failing the test through t, testify and configured assertions count as asserts, in the test and
// in its helpers. Handing t to code the adapter cannot read makes the count unknown.
func TestGoAsserts(t *testing.T) {
	tree := fstest.MapFS{
		"go.mod":         {Data: []byte("module example.com/app\n")},
		"store/store.go": {Data: []byte("package store\n\nfunc Add(a, b int) int { return a + b }\n")},
		"check/check.go": {Data: []byte("package check\n\nimport \"testing\"\n\nfunc Equal(t *testing.T, a, b int) {}\n\nfunc Same(t *testing.T, a, b int) {}\n")},
		"store/store_test.go": {Data: []byte(`package store

import (
	"testing"

	"github.com/stretchr/testify/require"

	"example.com/app/check"
)

func TestNone(t *testing.T) { Add(1, 2) }

func TestFatal(t *testing.T) {
	if Add(1, 2) != 3 {
		t.Fatalf("want 3")
	}
}

func TestTestify(t *testing.T) { require.Equal(t, 3, Add(1, 2)) }

func TestHelper(t *testing.T) { equal(t, Add(1, 2), 3) }

func TestSubtest(t *testing.T) {
	t.Run("one", func(t *testing.T) {
		if Add(1, 0) != 1 {
			t.Error("want 1")
		}
	})
}

func TestConfigured(t *testing.T) { check.Equal(t, Add(1, 2), 3) }

func TestUnreadable(t *testing.T) { check.Same(t, Add(1, 2), 3) }

func TestNamedSubtest(t *testing.T) { t.Run("one", runOne) }

func equal(tb testing.TB, a, b int) {
	if a != b {
		tb.Errorf("%d != %d", a, b)
	}
}
`)},
		"store/sub_test.go": {Data: []byte("package store\n\nimport \"testing\"\n\nvar runOne = func(t *testing.T) {}\n")},
	}
	c := config.Config{Assertions: []string{"example.com/app/check.Equal"}}
	paths := []string{"check/check.go", "store/store.go", "store/store_test.go", "store/sub_test.go"}
	a, err := Adapter{}.Analyze(engine.Version{FS: tree}, c, paths)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, ts := range a.Tests {
		got = append(got, fmt.Sprintf("%s asserts=%d unknown=%v", ts.Name, ts.Asserts, ts.AssertsUnknown))
	}
	sort.Strings(got)
	want := `TestConfigured asserts=1 unknown=false
TestFatal asserts=1 unknown=false
TestHelper asserts=1 unknown=false
TestNamedSubtest asserts=0 unknown=true
TestNone asserts=0 unknown=false
TestSubtest asserts=1 unknown=false
TestTestify asserts=1 unknown=false
TestUnreadable asserts=0 unknown=true`
	if g := strings.Join(got, "\n"); g != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", g, want)
	}
}

// viewpoint: behaviour
// target: golang.Adapter.Analyze with files.tests and files.generated
// files.generated makes a Go file generated without its header; files.tests never applies to Go,
// whose tests the compiler decides by name.
func TestGoRolesFromConfiguration(t *testing.T) {
	tree := fstest.MapFS{
		"go.mod":         {Data: []byte("module example.com/app\n")},
		"gen/client.go":  {Data: []byte("package gen\n\nfunc Call() {}\n")},
		"checks/a.go":    {Data: []byte("package checks\n\nfunc A() {}\n")},
		"store/store.go": {Data: []byte("package store\n\nfunc Save() {}\n")},
	}
	c := config.Config{GeneratedFiles: []string{"gen/**"}, TestFiles: []string{"checks/**"}}
	a, err := Adapter{}.Analyze(engine.Version{FS: tree}, c, []string{"checks/a.go", "gen/client.go", "store/store.go"})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range a.Files {
		got = append(got, fmt.Sprintf("%s %s", f.Path, f.Role))
	}
	for _, s := range a.Symbols {
		got = append(got, "symbol "+s.ID)
	}
	sort.Strings(got)
	want := "checks/a.go source\ngen/client.go generated\nstore/store.go source\nsymbol example.com/app/checks.A\nsymbol example.com/app/store.Save"
	if g := strings.Join(got, "\n"); g != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", g, want)
	}
}

// viewpoint: behaviour
// target: golang.Adapter.Mutants
// The five kinds of break, made only in the functions asked for, with anchors that hold no line
// number: the same function one line lower gives the same anchors.
func TestGoMutants(t *testing.T) {
	const body = `func Add(a, b int) int {
	if a < 0 && !ok() {
		log()
		return a + b
	}
	return 0
}

func ok() bool { return true }

func log() {}

func Name() string { return "x" + "y" }
`
	mutants := func(src string, line int) []facts.Mutant {
		tree := fstest.MapFS{"go.mod": {Data: []byte("module example.com/app\n")}, "calc/calc.go": {Data: []byte(src)}}
		funcs := []facts.Symbol{{ID: "example.com/app/calc.Add", Kind: facts.SymbolFunction, Path: "calc/calc.go", Line: line}}
		ms, err := Adapter{}.Mutants(engine.Version{FS: tree}, funcs)
		if err != nil {
			t.Fatal(err)
		}
		return ms
	}
	ms := mutants("package calc\n\n"+body, 3)
	var got []string
	for _, m := range ms {
		got = append(got, fmt.Sprintf("%d %s: %s -> %s", m.Line, m.Operator, m.Original, m.Replacement))
	}
	want := strings.Join([]string{
		"4 flip a comparison: < -> >=",
		"4 negate a boolean: ! -> ",
		"5 remove a call statement: log() -> _ = 0",
		"6 return the zero value: a + b -> 0",
		"6 swap arithmetic: + -> -",
	}, "\n")
	if g := strings.Join(got, "\n"); g != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", g, want)
	}
	moved := mutants("package calc\n\n// Add adds.\n"+body, 4)
	for i := range ms {
		if i >= len(moved) || moved[i].Anchor != ms[i].Anchor {
			t.Fatalf("moving the function changed an anchor: %v vs %v", ms, moved)
		}
	}
}

// viewpoint: behaviour
// target: golang.Adapter.Mutants
// A file almost always declares more than functions: imports, constants, variables, types. The
// mutants of its functions come out all the same.
func TestGoMutantsInAFileWithOtherDeclarations(t *testing.T) {
	src := `package calc

import "strings"

const limit = 3

var names = []string{"a"}

type Row struct{ N int }

func Short(s string) bool { return len(strings.TrimSpace(s)) < limit }
`
	tree := fstest.MapFS{"go.mod": {Data: []byte("module example.com/app\n")}, "calc/calc.go": {Data: []byte(src)}}
	funcs := []facts.Symbol{{ID: "example.com/app/calc.Short", Kind: facts.SymbolFunction, Path: "calc/calc.go", Line: 11}}
	ms, err := Adapter{}.Mutants(engine.Version{FS: tree}, funcs)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, m := range ms {
		got = append(got, fmt.Sprintf("%d %s: %s -> %s", m.Line, m.Operator, m.Original, m.Replacement))
	}
	want := "11 return the zero value: len(strings.TrimSpace(s)) < limit -> false\n11 flip a comparison: < -> >="
	if g := strings.Join(got, "\n"); g != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", g, want)
	}
}
