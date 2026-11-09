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
// target: golang.walker.visit
// A call in any clause of a statement is the test's, once: a for statement's init, a range's
// expression, an if's init, a switch's init and tag, a type switch's init, assignment and cases, a
// select's case. Only a loop's condition, post statement and body run more than once.
func TestGoCallsInEveryClauseOfAStatement(t *testing.T) {
	tree := fstest.MapFS{
		"go.mod": {Data: []byte("module example.com/app\n")},
		"store/store.go": {Data: []byte("// Package store keeps rows.\npackage store\n\n" +
			"// A starts.\nfunc A() int { return 0 }\n\n// B lists.\nfunc B() []int { return nil }\n\n" +
			"// C checks.\nfunc C() int { return 0 }\n\n// D picks.\nfunc D() int { return 0 }\n\n" +
			"// E maps.\nfunc E(n int) int { return n }\n\n// F acts.\nfunc F() {}\n\n" +
			"// L counts.\nfunc L() int { return 0 }\n\n// M boxes.\nfunc M(n int) any { return n }\n\n// N ends.\nfunc N() {}\n\n// H waits.\nfunc H() {}\n\n// K steps.\nfunc K() int { return 1 }\n")},
		"store/store_test.go": {Data: []byte(`package store_test

import (
	"testing"

	"example.com/app/store"
)

func TestClauses(t *testing.T) {
	for i := store.A(); i < store.K(); i++ {
	}
	for range store.B() {
	}
	if x := store.C(); x > 0 {
		t.Fatal("x")
	}
	switch y := store.D(); store.E(y) {
	case 1:
		store.F()
	}
	switch z := store.L(); v := store.M(z).(type) {
	default:
		_ = v
		store.N()
	}
	select {
	default:
		store.H()
	}
}
`)},
	}
	a, err := Adapter{}.Analyze(engine.Version{FS: tree}, config.Config{}, []string{"store/store.go", "store/store_test.go"})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range a.Tests[0].Calls {
		got = append(got, fmt.Sprintf("%s×%d", c.Target[strings.LastIndex(c.Target, ".")+1:], c.Times))
	}
	sort.Strings(got)
	want := "A×1 B×1 C×1 D×1 E×1 F×1 H×1 K×2 L×1 M×1 N×1"
	if strings.Join(got, " ") != want {
		t.Errorf("calls %v, want %s", got, want)
	}
}
