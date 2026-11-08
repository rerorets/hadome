package golang

import (
	"fmt"
	"io/fs"
	"os"
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

// dirSupportTree is a module whose tests reach the store through packages under test/ and e2e/, at
// the root and deeper, beside a product server under e2e/ and a product package named tests.
func dirSupportTree() fstest.MapFS {
	return fstest.MapFS{
		"go.mod": {Data: []byte("module example.com/app\n")},
		"store/store.go": {Data: []byte(`// Package store keeps rows.
package store

// Store keeps rows.
type Store struct{ rows []string }

// Open makes a Store.
func Open() *Store { return &Store{} }

// Append adds a row.
func (s *Store) Append(row string) int { s.rows = append(s.rows, row); return len(s.rows) }
`)},
		"test/helpers/helpers.go": {Data: []byte(`package helpers

import "example.com/app/store"

func NewStore() *store.Store { return store.Open() }
`)},
		"worker/e2e/client/client.go": {Data: []byte(`package client

func Hello() string { return "hi" }
`)},
		"e2e/server/server.go": {Data: []byte(`package server

func Serve() int { return 1 }
`)},
		"internal/rules/tests/tests.go": {Data: []byte(`package tests

func Check() int { return 1 }
`)},
		"store/store_test.go": {Data: []byte(`package store_test

import (
	"testing"

	"example.com/app/e2e/server"
	"example.com/app/internal/rules/tests"
	"example.com/app/test/helpers"
	"example.com/app/worker/e2e/client"
)

func TestAppend(t *testing.T) {
	if helpers.NewStore().Append("x") != 1 {
		t.Fatal("want 1")
	}
}

func TestHello(t *testing.T) {
	if client.Hello() != "hi" {
		t.Fatal("want hi")
	}
}

func TestServe(t *testing.T) {
	if server.Serve()+tests.Check() != 2 {
		t.Fatal("want 2")
	}
}
`)},
	}
}

// viewpoint: behaviour
// target: golang.Adapter.Analyze
// A package under test/ or e2e/, at any depth, is test support as testutil/ is: its functions are no
// symbols, and a test that calls one is followed into it, so a test calling only a helper that calls
// no product code targets nothing. A package under tests/ stays product code, since Go names a
// package after its directory. files.product keeps a product server under e2e/ product code.
func TestGoTestAndE2EDirectoriesAreTestSupport(t *testing.T) {
	tree := dirSupportTree()
	var paths []string
	for p := range tree {
		if strings.HasSuffix(p, ".go") {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	for _, c := range []struct {
		name string
		cfg  config.Config
		want []string
	}{
		{"default", config.Config{}, []string{
			"file e2e/server/server.go test",
			"file internal/rules/tests/tests.go source",
			"file store/store.go source",
			"file store/store_test.go test",
			"file test/helpers/helpers.go test",
			"file worker/e2e/client/client.go test",
			"symbol example.com/app/internal/rules/tests.Check",
			"symbol example.com/app/store.Open",
			"symbol example.com/app/store.Store",
			"symbol example.com/app/store.Store.Append",
			"test TestAppend calls=[example.com/app/store.Open example.com/app/store.Store.Append]",
			"test TestHello calls=[]",
			"test TestServe calls=[example.com/app/internal/rules/tests.Check]",
		}},
		{"files.product", config.Config{ProductFiles: []string{"e2e/server/**"}}, []string{
			"file e2e/server/server.go source",
			"file internal/rules/tests/tests.go source",
			"file store/store.go source",
			"file store/store_test.go test",
			"file test/helpers/helpers.go test",
			"file worker/e2e/client/client.go test",
			"symbol example.com/app/e2e/server.Serve",
			"symbol example.com/app/internal/rules/tests.Check",
			"symbol example.com/app/store.Open",
			"symbol example.com/app/store.Store",
			"symbol example.com/app/store.Store.Append",
			"test TestAppend calls=[example.com/app/store.Open example.com/app/store.Store.Append]",
			"test TestHello calls=[]",
			"test TestServe calls=[example.com/app/e2e/server.Serve example.com/app/internal/rules/tests.Check]",
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			a, err := Adapter{}.Analyze(engine.Version{FS: tree}, c.cfg, paths)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, f := range a.Files {
				got = append(got, fmt.Sprintf("file %s %s", f.Path, f.Role))
			}
			for _, s := range a.Symbols {
				got = append(got, "symbol "+s.ID)
			}
			for _, ts := range a.Tests {
				var calls []string
				for _, cl := range ts.Calls {
					calls = append(calls, cl.Target)
				}
				sort.Strings(calls)
				got = append(got, fmt.Sprintf("test %s calls=%v", ts.Name, calls))
			}
			sort.Strings(got)
			if g, w := strings.Join(got, "\n"), strings.Join(c.want, "\n"); g != w {
				t.Errorf("got:\n%s\n\nwant:\n%s", g, w)
			}
		})
	}
}

// viewpoint: behaviour
// target: golang.Adapter.Analyze on hadome's own internal/rules/tests
// hadome's own package named tests, the reason tests/ is no Go support directory, is product code:
// its files are source and its functions are symbols.
func TestHadomeRulesTestsPackageIsProduct(t *testing.T) {
	fsys := os.DirFS("../../..")
	paths, err := fs.Glob(fsys, "internal/rules/tests/*.go")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no files under internal/rules/tests: %v", err)
	}
	a, err := Adapter{}.Analyze(engine.Version{FS: fsys}, config.Config{}, paths)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range a.Files {
		want := facts.RoleSource
		if strings.HasSuffix(f.Path, "_test.go") {
			want = facts.RoleTest
		}
		if f.Role != want {
			t.Errorf("%s is %s, want %s", f.Path, f.Role, want)
		}
	}
	if len(a.Symbols) == 0 {
		t.Error("internal/rules/tests has no symbols")
	}
}

// viewpoint: behaviour
// target: golang.Adapter.Analyze
// A function of a test support package reads its parameters by the types they are declared with,
// through the imports of its own file, even when the test passes a nil, which has no type. A
// variadic parameter is a slice of its type.
func TestGoTestSupportParametersHaveTheirDeclaredTypes(t *testing.T) {
	tree := fstest.MapFS{
		"go.mod":         {Data: []byte("module example.com/app\n")},
		"store/store.go": {Data: []byte(storeSource)},
		"store/storetest/storetest.go": {Data: []byte(`package storetest

import (
	"testing"

	st "example.com/app/store"
)

func MustAppend(t *testing.T, s *st.Store) {
	t.Helper()
	_ = s.Append()
}

func AppendAll(t *testing.T, stores ...*st.Store) {
	for _, s := range stores {
		_ = s.Append()
	}
}
`)},
		"worker/queue_test.go": {Data: []byte(`package worker_test

import (
	"testing"

	"example.com/app/store/storetest"
)

func TestNil(t *testing.T) { storetest.MustAppend(t, nil) }

func TestAll(t *testing.T) { storetest.AppendAll(t, nil) }
`)},
	}
	want := strings.Join([]string{
		// A call inside a loop counts as two: it may run more than once.
		"TestAll calls=[example.com/app/store.Store.Append×2] unresolved=0",
		"TestNil calls=[example.com/app/store.Store.Append×1] unresolved=0",
	}, "\n")
	if got := callsByTest(t, tree); got != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", got, want)
	}
}

// viewpoint: behaviour
// target: golang.Adapter.Analyze
// A package named <name>test outside a directory called <name> is test support when it imports a
// package called <name>: by the name it gives the import, or by the last element of the import's
// path. One that imports no such package is product code.
func TestGoTestSupportPackagesAreKnownByTheirImports(t *testing.T) {
	tree := fstest.MapFS{
		"go.mod":         {Data: []byte("module example.com/app\n")},
		"store/store.go": {Data: []byte("// Package store keeps rows.\npackage store\n\n// Open opens.\nfunc Open() int { return 1 }\n")},
		"fakes/rowstest/rows.go": {Data: []byte(`package rowstest

import rows "example.com/app/store"

func New() int { return rows.Open() }
`)},
		"fakes/storetest/store.go": {Data: []byte(`package storetest

import s "example.com/app/store"

func New() int { return s.Open() }
`)},
		"fakes/clocktest/clock.go": {Data: []byte(`package clocktest

import "example.com/app/store"

func Now() int { return store.Open() }
`)},
	}
	var paths []string
	for p := range tree {
		if strings.HasSuffix(p, ".go") {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	a, err := Adapter{}.Analyze(engine.Version{FS: tree}, config.Config{}, paths)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range a.Files {
		got = append(got, fmt.Sprintf("%s %s", f.Path, f.Role))
	}
	sort.Strings(got)
	want := strings.Join([]string{
		"fakes/clocktest/clock.go source",
		"fakes/rowstest/rows.go test",
		"fakes/storetest/store.go test",
		"store/store.go source",
	}, "\n")
	if g := strings.Join(got, "\n"); g != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", g, want)
	}
}
