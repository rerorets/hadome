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

// supportTree is a module with a store package, test support packages for it, a product package
// whose name merely ends in test, and a test that reaches the store through the support packages.
func supportTree() fstest.MapFS {
	return fstest.MapFS{
		"go.mod": {Data: []byte("module example.com/app\n")},
		"store/store.go": {Data: []byte(`// Package store keeps rows.
package store

// Store keeps rows.
type Store struct{ rows []string }

// Open makes a Store.
func Open() *Store { return &Store{} }

// Seed adds a row.
func Seed(s *Store) { s.rows = append(s.rows, "seed") }

// Append adds a row.
func (s *Store) Append(row string) int { s.rows = append(s.rows, row); return len(s.rows) }
`)},
		"store/storetest/storetest.go": {Data: []byte(`package storetest

import (
	"testing"
	"time"

	"example.com/app/store"
)

func New(t *testing.T) *store.Store {
	s := store.Open()
	seed(s)
	return s
}

func seed(s *store.Store) {
	time.Sleep(time.Millisecond)
	store.Seed(s)
}

type Fake struct{}

func (Fake) Rows() int { return 0 }
`)},
		"internal/testutil/wait.go": {Data: []byte(`package testutil

import "example.com/app/store"

func Fresh() *store.Store { return store.Open() }
`)},
		"internal/latest/latest.go": {Data: []byte(`package latest

func Pick() int { return 1 }
`)},
		"internal/clocktest/clock.go": {Data: []byte(`package clocktest

func Now() int { return 0 }
`)},
		"store/store_test.go": {Data: []byte(`package store_test

import (
	"testing"

	"example.com/app/internal/testutil"
	"example.com/app/store/storetest"
)

func TestAppend(t *testing.T) {
	s := storetest.New(t)
	if s.Append("x") != 2 {
		t.Fatal("want 2")
	}
}

func TestFresh(t *testing.T) {
	if testutil.Fresh().Append("x") != 1 {
		t.Fatal("want 1")
	}
}
`)},
	}
}

func analyzeSupport(t *testing.T, c config.Config) *facts.Analysis {
	t.Helper()
	tree := supportTree()
	var paths []string
	for p := range tree {
		if strings.HasSuffix(p, ".go") {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	a, err := Adapter{}.Analyze(engine.Version{FS: tree}, c, paths)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// viewpoint: behaviour
// target: golang.Adapter.Analyze
// A test support package (store/storetest beside store, internal/testutil) is test code: its
// functions are no symbols, so neither targets nor asked for doc comments. A test that calls one is
// followed into it, through the package's own imports and other functions, so it targets the
// product code the helper calls, and the helper's sleep is the test's. A package whose name merely
// ends in test, without a package of the rest of its name (latest, clocktest), stays product code.
func TestGoTestSupportPackagesAreFollowedNotTargeted(t *testing.T) {
	a := analyzeSupport(t, config.Config{})
	var got []string
	for _, f := range a.Files {
		got = append(got, fmt.Sprintf("file %s %s", f.Path, f.Role))
	}
	for _, s := range a.Symbols {
		got = append(got, "symbol "+s.ID)
	}
	for _, ts := range a.Tests {
		var calls, hazards []string
		for _, c := range ts.Calls {
			calls = append(calls, fmt.Sprintf("%s×%d", c.Target, c.Times))
		}
		for _, h := range ts.Hazards {
			hazards = append(hazards, fmt.Sprintf("%s@%s:%d", h.Category, h.Path, h.Line))
		}
		got = append(got, fmt.Sprintf("test %s calls=%v hazards=%v unresolved=%d", ts.Name, calls, hazards, ts.Unresolved))
	}
	sort.Strings(got)
	want := strings.Join([]string{
		"file internal/clocktest/clock.go source",
		"file internal/latest/latest.go source",
		"file internal/testutil/wait.go test",
		"file store/store.go source",
		"file store/store_test.go test",
		"file store/storetest/storetest.go test",
		"symbol example.com/app/internal/clocktest.Now",
		"symbol example.com/app/internal/latest.Pick",
		"symbol example.com/app/store.Open",
		"symbol example.com/app/store.Seed",
		"symbol example.com/app/store.Store",
		"symbol example.com/app/store.Store.Append",
		"test TestAppend calls=[example.com/app/store.Open×1 example.com/app/store.Seed×1 example.com/app/store.Store.Append×1] hazards=[sleep@store/storetest/storetest.go:17] unresolved=0",
		"test TestFresh calls=[example.com/app/store.Open×1 example.com/app/store.Store.Append×1] hazards=[] unresolved=0",
	}, "\n")
	if g := strings.Join(got, "\n"); g != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", g, want)
	}
}

// viewpoint: behaviour
// target: golang.Adapter.Analyze with files.product
// files.product keeps a package that would be test support as product code: its functions are
// symbols again and tests target them. It never makes a _test.go file product code.
func TestGoProductFilesAreNotTestSupport(t *testing.T) {
	a := analyzeSupport(t, config.Config{ProductFiles: []string{"internal/testutil/**", "store/store_test.go"}})
	var got []string
	for _, f := range a.Files {
		if strings.HasPrefix(f.Path, "internal/testutil/") || f.Path == "store/store_test.go" {
			got = append(got, fmt.Sprintf("file %s %s", f.Path, f.Role))
		}
	}
	for _, ts := range a.Tests {
		if ts.Name == "TestFresh" {
			for _, c := range ts.Calls {
				got = append(got, "TestFresh calls "+c.Target)
			}
		}
	}
	sort.Strings(got)
	want := strings.Join([]string{
		"TestFresh calls example.com/app/internal/testutil.Fresh",
		"TestFresh calls example.com/app/store.Store.Append",
		"file internal/testutil/wait.go source",
		"file store/store_test.go test",
	}, "\n")
	if g := strings.Join(got, "\n"); g != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", g, want)
	}
}
