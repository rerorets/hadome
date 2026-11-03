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
