package golang

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/adapter/reuse"
	"github.com/rerorets/hadome/internal/config"
)

// viewpoint: behaviour
// target: golang.Adapter.Analyze
// A package's TestMain runs before each of its tests, so the product code it calls, directly or
// through helpers and test support packages, is a target of every test in its directory, whichever
// of the two test packages the test is in. Its clock reads and sleeps are those tests' hazards; its
// mocks and its own unresolved calls are not, and its parameter hides a package of the same name. A
// test in another directory gets nothing from it, nor from a function named TestMain that does not
// take a *testing.M alone.
func TestGoTestMainSetupIsGivenToEveryTestOfThePackage(t *testing.T) {
	tree := fstest.MapFS{
		"go.mod": {Data: []byte("module example.com/app\n")},
		"db/db.go": {Data: []byte(`package db

// DB is a connection pool.
type DB struct{}

// Open opens the pool.
func Open() *DB { return &DB{} }

// Migrate brings the schema up to date.
func Migrate() {}

// Run runs the pending jobs.
func Run() int { return 0 }

// Ping checks the pool.
func (d *DB) Ping() error { return nil }
`)},
		"db/dbtest/dbtest.go": {Data: []byte(`package dbtest

import "example.com/app/db"

var pool *db.DB

func Setup() { pool = db.Open() }

func Pool() *db.DB { return pool }
`)},
		"db/main_test.go": {Data: []byte(`package db_test

import (
	"os"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"example.com/app/db"
	"example.com/app/db/dbtest"
)

func TestMain(db *testing.M) {
	dbtest.Setup()
	prepare()
	os.Exit(db.Run())
}

func prepare() {
	_ = time.Now()
	time.Sleep(0)
	_ = gomock.NewController(nil)
	db.Migrate()
}
`)},
		"db/pool_test.go": {Data: []byte(`package db_test

import (
	"testing"

	"example.com/app/db/dbtest"
)

func TestPool(t *testing.T) {
	if dbtest.Pool() == nil {
		t.Fatal("want a pool")
	}
}
`)},
		"db/internal_test.go": {Data: []byte(`package db

import "testing"

func TestPing(t *testing.T) {
	if (&DB{}).Ping() != nil {
		t.Fatal("want no error")
	}
}
`)},
		"queue/queue_test.go": {Data: []byte(`package queue_test

import (
	"testing"

	"example.com/app/db"
)

func TestElsewhere(t *testing.T) {
	if db.Open() == nil {
		t.Fatal("want a pool")
	}
}

func TestMain(m *testing.M, extra int) { db.Migrate() }
`)},
	}
	var paths []string
	for p := range tree {
		if strings.HasSuffix(p, ".go") {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	a, err := Adapter{}.Analyze(reuse.Lone(tree), config.Analysis{}, paths)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, ts := range a.Tests {
		var calls, hazards []string
		for _, c := range ts.Calls {
			calls = append(calls, fmt.Sprintf("%s×%d", c.Target, c.Times))
		}
		for _, h := range ts.Hazards {
			hazards = append(hazards, fmt.Sprintf("%s@%s:%d", h.Category, h.Path, h.Line))
		}
		got = append(got, fmt.Sprintf("%s calls=%v hazards=%v unresolved=%d", ts.Name, calls, hazards, ts.Unresolved))
	}
	sort.Strings(got)
	want := strings.Join([]string{
		"TestElsewhere calls=[example.com/app/db.Open×1] hazards=[] unresolved=0",
		"TestPing calls=[example.com/app/db.DB.Ping×1 example.com/app/db.Migrate×1 example.com/app/db.Open×1] hazards=[clock@db/main_test.go:20 sleep@db/main_test.go:21] unresolved=0",
		"TestPool calls=[example.com/app/db.Migrate×1 example.com/app/db.Open×1] hazards=[clock@db/main_test.go:20 sleep@db/main_test.go:21] unresolved=0",
	}, "\n")
	if g := strings.Join(got, "\n"); g != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", g, want)
	}
}
