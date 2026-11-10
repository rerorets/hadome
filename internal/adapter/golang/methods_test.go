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

// callsByTest analyzes the tree and lists each test's calls and unresolved calls, sorted by test.
func callsByTest(t *testing.T, tree fstest.MapFS) string {
	t.Helper()
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
		var calls []string
		for _, c := range ts.Calls {
			calls = append(calls, fmt.Sprintf("%s×%d", c.Target, c.Times))
		}
		got = append(got, fmt.Sprintf("%s calls=%v unresolved=%d", ts.Name, calls, ts.Unresolved))
	}
	sort.Strings(got)
	return strings.Join(got, "\n")
}

const storeSource = `package store

// Config configures a Store.
type Config struct{ Max int }

// Default is the default Config.
var Default = Config{Max: 3}

// Validate reports whether c is usable.
func (c Config) Validate() error { return nil }

// WithDefaults fills zero fields.
func (c Config) WithDefaults() Config { return c }

// Store holds entries.
type Store struct{ n int }

// Append adds one entry.
func (s *Store) Append() int { s.n++; return s.n }

// Entry is one entry.
type Entry struct{ Kind string }

// Done reports whether e is done.
func (e Entry) Done() bool { return e.Kind == "done" }

type limiter struct{ rate int }

func (l *limiter) batch() int { return l.rate }

func double(x int) int { return x * 2 }
`

// viewpoint: behaviour
// target: golang.Adapter.Analyze
// A method call reaches its target when the receiver's type is declared anywhere the adapter reads:
// a package-level variable of the code, a literal in parentheses, a helper's declared result, the
// element of a ranged slice or map, a field of a declared struct, a method's declared result, or a
// var declaration. A local declared with := hides a package-level variable of the same name.
func TestGoMethodCallsReachTheirTargetThroughDeclaredTypes(t *testing.T) {
	tree := fstest.MapFS{
		"go.mod":         {Data: []byte("module example.com/app\n")},
		"store/store.go": {Data: []byte(storeSource)},
		"store/helpers_test.go": {Data: []byte(`package store

import "testing"

type fixture struct{ s *Store }

func newStore(t *testing.T) *Store {
	t.Helper()
	return &Store{}
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	return fixture{s: &Store{}}
}

func fake() interface{ Validate() error } { return nil }
`)},
		"store/store_test.go": {Data: []byte(`package store

import "testing"

func TestPackageVar(t *testing.T) { _ = Default.Validate() }

func TestLocalFromPackageVar(t *testing.T) {
	c := Default
	_ = c.Validate()
}

func TestParenLiteral(t *testing.T) {
	_ = (Config{}).Validate()
	_ = (&Config{}).Validate()
}

func TestHelperChain(t *testing.T) { _ = newStore(t).Append() }

func TestHelperLocal(t *testing.T) {
	s := newStore(t)
	_ = s.Append()
}

func TestTableField(t *testing.T) {
	cases := []struct {
		e    Entry
		want bool
	}{{Entry{Kind: "done"}, true}}
	for _, c := range cases {
		if c.e.Done() != c.want {
			t.Fatal("x")
		}
	}
}

func TestTableElem(t *testing.T) {
	for _, e := range []Entry{{Kind: "done"}} {
		_ = e.Done()
	}
}

func TestNamedTable(t *testing.T) {
	type tc struct{ s *Store }
	for _, c := range []tc{{s: &Store{}}} {
		_ = c.s.Append()
	}
}

func TestMapRange(t *testing.T) {
	m := map[string]Entry{"a": {}}
	for _, e := range m {
		_ = e.Done()
	}
}

func TestFieldChain(t *testing.T) {
	f := newFixture(t)
	_ = f.s.Append()
}

func TestMethodResult(t *testing.T) { _ = Default.WithDefaults().Validate() }

func TestVarDecl(t *testing.T) {
	var c Config
	_ = c.Validate()
}

func TestShadowedPackageVar(t *testing.T) {
	Default := fake()
	_ = Default.Validate()
}
`)},
	}
	want := strings.Join([]string{
		"TestFieldChain calls=[example.com/app/store.Store.Append×1] unresolved=0",
		"TestHelperChain calls=[example.com/app/store.Store.Append×1] unresolved=0",
		"TestHelperLocal calls=[example.com/app/store.Store.Append×1] unresolved=0",
		"TestLocalFromPackageVar calls=[example.com/app/store.Config.Validate×1] unresolved=0",
		// A call inside a loop counts as two: it may run more than once.
		"TestMapRange calls=[example.com/app/store.Entry.Done×2] unresolved=0",
		"TestMethodResult calls=[example.com/app/store.Config.Validate×1 example.com/app/store.Config.WithDefaults×1] unresolved=0",
		"TestNamedTable calls=[example.com/app/store.Store.Append×2] unresolved=0",
		"TestPackageVar calls=[example.com/app/store.Config.Validate×1] unresolved=0",
		"TestParenLiteral calls=[example.com/app/store.Config.Validate×2] unresolved=0",
		"TestShadowedPackageVar calls=[] unresolved=1",
		"TestTableElem calls=[example.com/app/store.Entry.Done×2] unresolved=0",
		"TestTableField calls=[example.com/app/store.Entry.Done×2] unresolved=0",
		"TestVarDecl calls=[example.com/app/store.Config.Validate×1] unresolved=0",
	}, "\n")
	if got := callsByTest(t, tree); got != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", got, want)
	}
}

// pairSource declares a function and a method that return two values of different types.
const pairSource = `
// Split returns the Config and a Store built from it.
func (c Config) Split() (Config, *Store) { return c, &Store{} }

// Both returns a Config and a Store.
func Both() (Config, *Store) { return Default, &Store{} }
`

// viewpoint: behaviour
// target: golang.Adapter.Analyze
// Each name an assignment or a var declaration sets takes the type of its own value; with one call
// on the right, each name takes the call's result in its position, whether the call is of a
// function or of a method. A package-level variable of the code declared with a type, and one of
// another test file set with =, are typed too.
func TestGoAssignmentsTypeEachName(t *testing.T) {
	tree := fstest.MapFS{
		"go.mod":         {Data: []byte("module example.com/app\n")},
		"store/store.go": {Data: []byte(storeSource + "\n// Shared is set by the program.\nvar Shared *Store\n" + pairSource)},
		"store/helpers_test.go": {Data: []byte(`package store

import "testing"

var shared *Store

func newStore(t *testing.T) *Store { return &Store{} }

func openStore() (*Store, error) { return &Store{}, nil }
`)},
		"store/store_test.go": {Data: []byte(`package store

import "testing"

func TestPairs(t *testing.T) {
	c, s := Default, newStore(t)
	_ = c.Validate()
	_ = s.Append()
}

func TestVarPairs(t *testing.T) {
	var c, s = Default, newStore(t)
	_ = c.Validate()
	_ = s.Append()
}

func TestFirstResult(t *testing.T) {
	s, err := openStore()
	_ = s.Append()
	_ = err.Error()
}

func TestVarFirstResult(t *testing.T) {
	var s, err = openStore()
	_ = s.Append()
	_ = err.Error()
}

func TestFunctionResults(t *testing.T) {
	c, s := Both()
	_ = c.Validate()
	_ = s.Append()
}

func TestMethodResults(t *testing.T) {
	c, s := Default.Split()
	_ = c.Validate()
	_ = s.Append()
}

func TestVarMethodResults(t *testing.T) {
	var c, s = Default.Split()
	_ = c.Validate()
	_ = s.Append()
}

func TestDeclaredPackageVar(t *testing.T) { _ = Shared.Append() }

func TestAssignedPackageVar(t *testing.T) {
	shared = newStore(t)
	_ = shared.Append()
}
`)},
	}
	want := strings.Join([]string{
		"TestAssignedPackageVar calls=[example.com/app/store.Store.Append×1] unresolved=0",
		"TestDeclaredPackageVar calls=[example.com/app/store.Store.Append×1] unresolved=0",
		// error is not a type of the repository, so err.Error() names no target.
		"TestFirstResult calls=[example.com/app/store.Store.Append×1] unresolved=1",
		"TestFunctionResults calls=[example.com/app/store.Both×1 example.com/app/store.Config.Validate×1 example.com/app/store.Store.Append×1] unresolved=0",
		"TestMethodResults calls=[example.com/app/store.Config.Split×1 example.com/app/store.Config.Validate×1 example.com/app/store.Store.Append×1] unresolved=0",
		"TestPairs calls=[example.com/app/store.Config.Validate×1 example.com/app/store.Store.Append×1] unresolved=0",
		"TestVarFirstResult calls=[example.com/app/store.Store.Append×1] unresolved=1",
		"TestVarMethodResults calls=[example.com/app/store.Config.Split×1 example.com/app/store.Config.Validate×1 example.com/app/store.Store.Append×1] unresolved=0",
		"TestVarPairs calls=[example.com/app/store.Config.Validate×1 example.com/app/store.Store.Append×1] unresolved=0",
	}, "\n")
	if got := callsByTest(t, tree); got != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", got, want)
	}
}

// viewpoint: behaviour
// target: golang.Adapter.Analyze
// A test reads a package-level variable of its package when it names it, but not when a field
// after a dot has the same name.
func TestGoFieldNamedLikeAVariableIsNotARead(t *testing.T) {
	tree := fstest.MapFS{
		"go.mod":         {Data: []byte("module example.com/app\n")},
		"store/store.go": {Data: []byte(storeSource)},
		"store/store_test.go": {Data: []byte(`package store

import "testing"

type holder struct{ Default int }

func TestVariable(t *testing.T) { _ = Default }

func TestField(t *testing.T) {
	h := holder{}
	_ = h.Default
}
`)},
	}
	a, err := Adapter{}.Analyze(reuse.Lone(tree), config.Analysis{}, []string{"store/store.go", "store/store_test.go"})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, ts := range a.Tests {
		got = append(got, fmt.Sprintf("%s reads=%d", ts.Name, ts.Reads))
	}
	sort.Strings(got)
	if g, want := strings.Join(got, " "), "TestField reads=0 TestVariable reads=1"; g != want {
		t.Errorf("got  %s\nwant %s", g, want)
	}
}

// viewpoint: behaviour
// target: golang.Adapter.Analyze
// A test in the package itself reaches its unexported methods, as it reaches its unexported
// functions. A test in the external _test package cannot call them, so such a call is not a target.
func TestGoUnexportedMethodsAreTargetsOfTheirOwnPackage(t *testing.T) {
	tree := fstest.MapFS{
		"go.mod":         {Data: []byte("module example.com/app\n")},
		"store/store.go": {Data: []byte(storeSource)},
		"store/internal_test.go": {Data: []byte(`package store

import "testing"

func TestUnexportedMethod(t *testing.T) {
	l := &limiter{rate: 2}
	_ = l.batch()
}

func TestUnexportedFunc(t *testing.T) { _ = double(2) }
`)},
		"store/external_test.go": {Data: []byte(`package store_test

import (
	"testing"

	"example.com/app/store"
)

func TestOtherPackageVar(t *testing.T) {
	c := store.Default
	_ = c.Validate()
	_ = store.Default.withDefaults()
}
`)},
	}
	want := strings.Join([]string{
		"TestOtherPackageVar calls=[example.com/app/store.Config.Validate×1] unresolved=1",
		"TestUnexportedFunc calls=[example.com/app/store.double×1] unresolved=0",
		"TestUnexportedMethod calls=[example.com/app/store.limiter.batch×1] unresolved=0",
	}, "\n")
	if got := callsByTest(t, tree); got != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", got, want)
	}
}

// viewpoint: behaviour
// target: golang.Adapter.Analyze
// In the repository of a database library itself, its packages are the product's code: the result
// of one of their functions that hadome did not read is not taken for the library's known type, so
// a method called on it stays unresolved.
func TestGoDatabaseLibraryResultsInTheLibrarysOwnRepository(t *testing.T) {
	tree := fstest.MapFS{
		"go.mod": {Data: []byte("module github.com/jackc/pgx/v5\n")},
		"pgxpool/pool_test.go": {Data: []byte(`package pgxpool_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestConnect(t *testing.T) {
	conn, _ := pgx.Connect(context.Background(), "dsn")
	_ = conn.Close(context.Background())
}
`)},
	}
	if got, want := callsByTest(t, tree), "TestConnect calls=[] unresolved=2"; got != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", got, want)
	}
}

// viewpoint: behaviour
// target: golang.Adapter.Analyze
// A method of a value of another module's concrete type runs that module's code, so it is neither a
// target nor unresolved: a value declared with a pointer type, one made by a composite literal, and
// the results of the database libraries' constructors, transactions and queries. A value of another
// module's type that may be an interface (declared without a pointer, or a result that is one) still
// counts as unresolved, because it may hold a value of the product. So does a value of a package of
// the repository that hadome did not read, and an error a query returns beside its rows.
func TestGoMethodsOfExternalConcreteValuesAreNotUnresolved(t *testing.T) {
	tree := fstest.MapFS{
		"go.mod":         {Data: []byte("module example.com/app\n")},
		"store/store.go": {Data: []byte(storeSource)},
		"store/db_test.go": {Data: []byte(`package store

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jmoiron/sqlx"

	"example.com/app/gen"
)

type fixture struct{ db *sql.DB }

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	return nil
}

func TestHelperDB(t *testing.T) {
	db := openDB(t)
	_, _ = db.Exec("DELETE FROM entries")
	_ = (&Store{}).Append()
}

func TestOpenedDB(t *testing.T) {
	db, _ := sql.Open("driver", "dsn")
	tx, _ := db.Begin()
	rows, _ := tx.Query("SELECT 1")
	_ = rows.Close()
	_ = db.QueryRow("SELECT 1").Scan()
	stmt, _ := db.Prepare("SELECT 1")
	_ = stmt.Close()
	conn, _ := db.Conn(context.Background())
	_ = conn.Close()
	_ = tx.Commit()
}

func TestFieldAndParam(t *testing.T) {
	f := fixture{}
	_ = f.db.Ping()
	check := func(tx *sql.Tx) { _ = tx.Rollback() }
	check(nil)
}

func TestSqlx(t *testing.T) {
	db, _ := sqlx.Connect("driver", "dsn")
	tx := db.MustBegin()
	_ = tx.Commit()
}

func TestPgx(t *testing.T) {
	ctx := context.Background()
	pool, _ := pgxpool.New(ctx, "dsn")
	c, _ := pool.Acquire(ctx)
	c.Release()
	conn, _ := pgx.Connect(ctx, "dsn")
	_ = conn.Close(ctx)
}

func TestLiteral(t *testing.T) {
	buf := &bytes.Buffer{}
	_ = buf.String()
}

func TestInterfaces(t *testing.T) {
	var r io.Reader
	_, _ = r.Read(nil)
	ctx := context.Background()
	conn, _ := pgx.Connect(ctx, "dsn")
	tx, _ := conn.Begin(ctx)
	_ = tx.Commit(ctx)
	db, _ := sql.Open("driver", "dsn")
	_, err := db.Query("SELECT 1")
	_ = err.Error()
}

func TestUnreadPackage(t *testing.T) {
	var m *gen.Maker
	m.Make()
}
`)},
	}
	want := strings.Join([]string{
		"TestFieldAndParam calls=[] unresolved=0",
		"TestHelperDB calls=[example.com/app/store.Store.Append×1] unresolved=0",
		"TestInterfaces calls=[] unresolved=3",
		"TestLiteral calls=[] unresolved=0",
		"TestOpenedDB calls=[] unresolved=0",
		"TestPgx calls=[] unresolved=0",
		"TestSqlx calls=[] unresolved=0",
		"TestUnreadPackage calls=[] unresolved=1",
	}, "\n")
	if got := callsByTest(t, tree); got != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", got, want)
	}
}
