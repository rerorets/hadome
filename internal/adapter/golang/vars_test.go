package golang

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
)

// viewpoint: behaviour
// target: golang.Adapter.Analyze
// A test reaches the code through the package-level variables of its file, built by or declared as
// a repository type. A local of the same name hides one.
func TestGoTestsReachTheCodeThroughPackageVariables(t *testing.T) {
	tree := fstest.MapFS{
		"go.mod": {Data: []byte("module example.com/app\n")},
		"store/store.go": {Data: []byte(`package store

// Store keeps rows.
type Store struct{ rows []string }

// New makes a Store.
func New() *Store { return &Store{} }

// Add adds a row.
func (s *Store) Add(row string) int { s.rows = append(s.rows, row); return len(s.rows) }
`)},
		"store/store_test.go": {Data: []byte(`package store_test

import (
	"testing"

	"example.com/app/store"
)

var built = store.New()

var declared *store.Store

var plain = 3

func TestBuilt(t *testing.T) {
	if built.Add("x") != 1 {
		t.Fatal("want 1")
	}
}

func TestDeclared(t *testing.T) {
	if declared.Add("x") != 1 {
		t.Fatal("want 1")
	}
}

func TestShadowed(t *testing.T) {
	built := fake()
	if built.Add("x") != 1 {
		t.Fatal("want 1")
	}
}

func TestPlain(t *testing.T) {
	if plain.Add("x") != 1 {
		t.Fatal("want 1")
	}
}
`)},
	}
	a, err := Adapter{}.Analyze(engine.Version{FS: tree}, config.Config{}, []string{"store/store.go", "store/store_test.go"})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, ts := range a.Tests {
		var calls []string
		for _, c := range ts.Calls {
			calls = append(calls, fmt.Sprintf("%s×%d", c.Target, c.Times))
		}
		got = append(got, fmt.Sprintf("%s calls=%v unresolved=%d", ts.Name, calls, ts.Unresolved))
	}
	sort.Strings(got)
	want := strings.Join([]string{
		"TestBuilt calls=[example.com/app/store.Store.Add×1] unresolved=0",
		"TestDeclared calls=[example.com/app/store.Store.Add×1] unresolved=0",
		"TestPlain calls=[] unresolved=1",
		"TestShadowed calls=[] unresolved=1",
	}, "\n")
	if g := strings.Join(got, "\n"); g != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", g, want)
	}
}
