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

// textsOf analyzes a module with one product file and one test file, and lists each test's strings
// as "<test> <path>:<line> <value> holes=<holes> sink=<sink> dynamic=<dynamic> ×<times>".
func textsOf(t *testing.T, c config.Analysis, product, test string) []string {
	t.Helper()
	tree := fstest.MapFS{
		"go.mod":              {Data: []byte("module example.com/app\n")},
		"store/store.go":      {Data: []byte(product)},
		"store/store_test.go": {Data: []byte(test)},
	}
	a, err := Adapter{}.Analyze(reuse.Lone(tree), c, []string{"store/store.go", "store/store_test.go"})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, ts := range a.Tests {
		for _, x := range ts.Texts {
			got = append(got, fmt.Sprintf("%s %s:%d %q holes=%v sink=%s dynamic=%t ×%d", ts.Name, x.Path, x.Line, x.Value, x.Holes, x.Sink, x.Dynamic, x.Times))
		}
	}
	sort.Strings(got)
	return got
}

func checkTexts(t *testing.T, got, want []string) {
	t.Helper()
	if g, w := strings.Join(got, "\n"), strings.Join(want, "\n"); g != w {
		t.Errorf("got:\n%s\n\nwant:\n%s", g, w)
	}
}

const textsProduct = `package store

// Query is the SQL that reads rows.
const Query = "SELECT * FROM store.rows"

// Label is not shown to tests.
var Label = "product " + "label"

// Append adds a row.
func Append() string { return "INSERT INTO store.rows VALUES (1)" }
`

// viewpoint: behaviour
// target: golang.Adapter.Analyze
// A test hands over the strings in its body, in the helpers it follows and in the package's
// constants it reads. Literals joined with + are one string, with a hole where a value that is not a
// literal is joined. A string in a loop counts twice. Strings of product code are never handed over.
func TestGoTestsHandOverTheirStrings(t *testing.T) {
	got := textsOf(t, config.Analysis{}, textsProduct, `package store

import "testing"

const local = "SELECT 1"

func run(sql string) {}

func helper() { run("SELECT store.count()") }

func TestStrings(t *testing.T) {
	name := "x"
	run("SELECT " + "store.f(" + name + ")")
	helper()
	run(local)
	run(Query)
	for range 3 {
		run("DELETE FROM store.rows")
	}
}
`)
	checkTexts(t, got, []string{
		`TestStrings store/store.go:4 "SELECT * FROM store.rows" holes=[] sink= dynamic=false ×1`,
		`TestStrings store/store_test.go:12 "x" holes=[] sink= dynamic=false ×1`,
		`TestStrings store/store_test.go:13 "SELECT store.f()" holes=[15] sink= dynamic=false ×1`,
		`TestStrings store/store_test.go:18 "DELETE FROM store.rows" holes=[] sink= dynamic=false ×2`,
		`TestStrings store/store_test.go:5 "SELECT 1" holes=[] sink= dynamic=false ×1`,
		`TestStrings store/store_test.go:9 "SELECT store.count()" holes=[] sink= dynamic=false ×1`,
	})
}

// viewpoint: behaviour
// target: golang.Adapter.Analyze
// A string passed to a database client's method that takes SQL is marked as SQL: a literal, a
// constant, a local variable holding a literal, a helper's parameter given a literal, and the format
// of fmt.Sprintf. A value that is not a known string is handed over as a dynamic string; the empty
// string the helper build returns is still one of the test's strings. A method of the same name on a
// value of the repository's own type is not a database client.
func TestGoTestsMarkTheStringsPassedToSQL(t *testing.T) {
	got := textsOf(t, config.Analysis{}, `package store

// Store keeps rows.
type Store struct{}

// Exec runs a command of the store, not SQL.
func (s *Store) Exec(cmd string) {}
`, `package store

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
)

func build(id int) string { return "" }

func count(db *sql.DB, q string) { db.QueryRow(q) }

func TestSQL(t *testing.T) {
	var db *sql.DB
	ctx := context.Background()
	db.Exec("DELETE FROM store.rows")
	q := "SELECT store.f(1)"
	db.QueryContext(ctx, q)
	count(db, "SELECT store.g()")
	db.Query(fmt.Sprintf("SELECT * FROM %s", "store.rows"))
	db.Query(build(1))
	s := &Store{}
	s.Exec("not sql")
}
`)
	checkTexts(t, got, []string{
		`TestSQL store/store_test.go:10 "" holes=[] sink= dynamic=false ×1`,
		`TestSQL store/store_test.go:17 "DELETE FROM store.rows" holes=[] sink=sql dynamic=false ×1`,
		`TestSQL store/store_test.go:18 "SELECT store.f(1)" holes=[] sink=sql dynamic=false ×1`,
		`TestSQL store/store_test.go:20 "SELECT store.g()" holes=[] sink=sql dynamic=false ×1`,
		`TestSQL store/store_test.go:21 "SELECT * FROM %s" holes=[] sink=sql dynamic=false ×1`,
		`TestSQL store/store_test.go:21 "store.rows" holes=[] sink= dynamic=false ×1`,
		`TestSQL store/store_test.go:22 "" holes=[] sink=sql dynamic=true ×1`,
		`TestSQL store/store_test.go:24 "not sql" holes=[] sink= dynamic=false ×1`,
	})
}

// viewpoint: behaviour
// target: golang.Adapter.Analyze
// A call configuration names in rules.tests.sql-sinks takes SQL, by its name or as written, without
// a database client imported. Without the entry the same call takes no SQL.
func TestGoTestsMarkTheStringsPassedToConfiguredSinks(t *testing.T) {
	test := `package store

import "testing"

type runner struct{}

func (runner) Run(sql string) {}

func TestRun(t *testing.T) {
	var r runner
	r.Run("SELECT store.f()")
}
`
	want := func(sink string) []string {
		return []string{fmt.Sprintf(`TestRun store/store_test.go:11 "SELECT store.f()" holes=[] sink=%s dynamic=false ×1`, sink)}
	}
	checkTexts(t, textsOf(t, config.Analysis{}, textsProduct, test), want(""))
	for _, entry := range []string{"Run", "r.Run"} {
		c := config.Analysis{SQLSinks: map[string][]string{"go": {entry}}}
		checkTexts(t, textsOf(t, c, textsProduct, test), want(facts.TextSinkSQL))
	}
}
