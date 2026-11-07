//go:build cgo

package adapter_test

import (
	"fmt"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/adapter"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/rules/comments"
)

// brokenReferences returns where comments/broken-reference reports regressions in head, as
// "path:line ref".
func brokenReferences(t *testing.T, head fstest.MapFS) []string {
	t.Helper()
	e := &engine.Engine{Analyzer: adapter.New(), Rules: comments.All()}
	r, err := e.Check(versions{base: fstest.MapFS{}, head: head})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range r.Regressions {
		if f.Rule == "comments/broken-reference" {
			got = append(got, fmt.Sprintf("%s:%d %s", f.File, f.Line, f.Anchor))
		}
	}
	return got
}

// viewpoint: behaviour
// target: comments/broken-reference through the adapters
// A reference inside a block comment is reported on the line it is written on, whichever
// language's adapter read the comment.
func TestBrokenReferenceInABlockCommentIsOnItsOwnLine(t *testing.T) {
	block := "/**\n * Rows are kept as\n * docs/design/rows.md says.\n */\n"
	cases := []struct{ path, src string }{
		{"store/store.go", "package store\n\n" + block + "func Append() {}\n"},
		{"src/store.ts", "\n\n" + block + "export function append() {}\n"},
		{"modules/network/main.tf", "\n\n" + block + "variable \"cidr\" {\n  description = \"The range.\"\n}\n"},
	}
	for _, c := range cases {
		head := fstest.MapFS{"go.mod": {Data: []byte("module example.com/app\n")}, c.path: {Data: []byte(c.src)}}
		got := brokenReferences(t, head)
		want := c.path + ":5 docs/design/rows.md"
		if len(got) != 1 || got[0] != want {
			t.Errorf("%s: got %v, want [%s]", c.path, got, want)
		}
	}
}

// viewpoint: behaviour
// target: comments/broken-reference through the adapters
// A reference right after Japanese punctuation is still a reference.
func TestBrokenReferenceAfterJapanesePunctuationIsFound(t *testing.T) {
	src := "package store\n\n// 設計（docs/a.md）・docs/b.md、docs/c.md「docs/d.md」を見る。\nfunc Append() {}\n"
	got := brokenReferences(t, fstest.MapFS{"go.mod": {Data: []byte("module example.com/app\n")}, "store/store.go": {Data: []byte(src)}})
	want := []string{"store/store.go:3 docs/a.md", "store/store.go:3 docs/b.md", "store/store.go:3 docs/c.md", "store/store.go:3 docs/d.md"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
