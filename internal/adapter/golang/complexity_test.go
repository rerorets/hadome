package golang

import (
	"fmt"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
)

// viewpoint: behaviour
// target: golang.Adapter.Analyze
// A function literal is counted on its own: the function that holds it counts only its own
// branches, and each literal, also one nested in another, is listed with its line and complexity.
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
`)},
	}
	a, err := Adapter{}.Analyze(engine.Version{FS: tree}, config.Config{}, []string{"store/store.go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Symbols) != 1 {
		t.Fatalf("got %d symbols, want 1", len(a.Symbols))
	}
	s := a.Symbols[0]
	got := fmt.Sprintf("cx=%d nested=%v", s.Complexity, s.Nested)
	if want := "cx=3 nested=[{6 4} {10 2}]"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}
