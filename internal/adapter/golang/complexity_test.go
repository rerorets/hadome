package golang

import (
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
)

// viewpoint: behaviour
// target: golang.Adapter.Analyze
// A function literal is counted on its own: the function that holds it counts only its own
// branches, and each literal, also one nested in another, is listed with its line and complexity,
// in a method as in a function.
func TestGoNestedComplexity(t *testing.T) {
	tree := fstest.MapFS{
		"go.mod": {Data: []byte("module example.com/app\n")},
		"store/store.go": {Data: []byte(`package store

func Outer(x int) func() int {
	if x > 0 { x++ }
	if x > 1 { x++ }
	return func() int {
		if x == 0 { return 0 }
		if x == 1 { return 1 }
		if x == 2 { return 2 }
		f := func() bool { return x > 0 && x < 9 }
		_ = f
		return x
	}
}

type T struct{}

func (T) M(x int) { f := func() { if x > 0 { x-- } }; f() }
`)},
	}
	a, err := Adapter{}.Analyze(engine.Version{FS: tree}, config.Config{}, []string{"store/store.go"})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range a.Symbols {
		got = append(got, fmt.Sprintf("%s %s cx=%d nested=%v", s.Kind, s.ID, s.Complexity, s.Nested))
	}
	want := []string{
		"function example.com/app/store.Outer cx=3 nested=[{6 4} {10 2}]",
		"type example.com/app/store.T cx=0 nested=[]",
		"method example.com/app/store.T.M cx=1 nested=[{18 2}]",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
