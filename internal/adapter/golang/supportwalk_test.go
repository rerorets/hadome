package golang

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/adapter/reuse"
	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/facts"
)

// viewpoint: behaviour
// target: golang.walker.followSupport
// A helper of a support package is followed like the test's own code: what it calls through its
// package's unexported helpers is the test's, its assertions on the testing tool count, a string the
// test passes it reaches the SQL it runs, so do the string constants of its package, a second call
// walks it again, and a helper that calls itself is walked once per call from the test.
func TestGoSupportHelpersAreFollowed(t *testing.T) {
	tree := fstest.MapFS{
		"go.mod": {Data: []byte("module example.com/app\n")},
		"store/store.go": {Data: []byte("// Package store keeps rows.\npackage store\n\n// Seed adds a row.\nfunc Seed() {}\n\n" +
			"// Drop removes a row.\nfunc Drop() {}\n")},
		"store/storetest/storetest.go": {Data: []byte(`package storetest

import (
	"database/sql"
	"testing"

	"example.com/app/store"
)

var db *sql.DB

func Prepare() { seed() }

func seed() { store.Seed() }

func Check(tb testing.TB, n int) {
	if n < 0 {
		tb.Fatal("negative")
	}
}

func Run(query string) { db.Exec(query) }

const countSQL = "SELECT store.size()"

func Count() { db.Exec(countSQL) }

func Down(n int) {
	if n > 0 {
		Down(n - 1)
	}
	store.Drop()
}
`)},
		"app/app_test.go": {Data: []byte(`package app_test

import (
	"testing"

	"example.com/app/store/storetest"
)

func TestFollowed(t *testing.T) {
	storetest.Prepare()
	storetest.Prepare()
	storetest.Count()
	storetest.Check(t, 1)
	storetest.Run("SELECT store.count()")
	storetest.Down(2)
	storetest.Down(3)
}
`)},
	}
	paths := []string{"app/app_test.go", "store/store.go", "store/storetest/storetest.go"}
	a, err := Adapter{}.Analyze(reuse.Lone(tree), config.Analysis{}, paths)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Tests) != 1 {
		t.Fatalf("tests %+v", a.Tests)
	}
	ts := a.Tests[0]
	var calls []string
	for _, c := range ts.Calls {
		calls = append(calls, c.Target)
		if c.Target == "example.com/app/store.Seed" && c.Times != 2 {
			t.Errorf("store.Seed is called %d times, want once per call of Prepare", c.Times)
		}
	}
	sort.Strings(calls)
	if got, want := strings.Join(calls, " "), "example.com/app/store.Drop example.com/app/store.Seed"; got != want {
		t.Errorf("calls %s, want %s", got, want)
	}
	if ts.Asserts != 1 {
		t.Errorf("asserts %d, want the one t.Fatal in Check", ts.Asserts)
	}
	var sinks []string
	for _, x := range ts.Texts {
		if x.Sink == facts.TextSinkSQL {
			sinks = append(sinks, fmt.Sprintf("%s dynamic=%t", x.Value, x.Dynamic))
		}
	}
	sort.Strings(sinks)
	if got, want := strings.Join(sinks, "|"), "SELECT store.count() dynamic=false|SELECT store.size() dynamic=false"; got != want {
		t.Errorf("SQL %q, want %q", got, want)
	}
}

// viewpoint: behaviour
// target: golang.newTables
// Calling a mocking library, built in or configured, is a mock hazard.
func TestGoMockingLibrariesAreHazards(t *testing.T) {
	for _, lib := range []string{"github.com/golang/mock/gomock", "go.uber.org/mock/gomock", "github.com/stretchr/testify/mock",
		"github.com/vektra/mockery/v2/mocks", "example.com/app/fakes"} {
		tree := fstest.MapFS{
			"go.mod":         {Data: []byte("module example.com/app\n")},
			"store/store.go": {Data: []byte("// Package store keeps rows.\npackage store\n\n// Seed adds a row.\nfunc Seed() {}\n")},
			"store/store_test.go": {Data: []byte("package store\n\nimport (\n\t\"testing\"\n\n\tm \"" + lib + "\"\n)\n\n" +
				"func TestSeed(t *testing.T) {\n\tm.NewController(t)\n\tSeed()\n}\n")},
		}
		c := config.Analysis{Hazards: map[string][]string{"mock": {"example.com/app/fakes"}}}
		a, err := Adapter{}.Analyze(reuse.Lone(tree), c, []string{"store/store.go", "store/store_test.go"})
		if err != nil {
			t.Fatal(err)
		}
		mocked := false
		for _, h := range a.Tests[0].Hazards {
			mocked = mocked || h.Category == facts.HazardMock
		}
		if !mocked {
			t.Errorf("importing %s is no mock hazard: %+v", lib, a.Tests[0].Hazards)
		}
	}
}
