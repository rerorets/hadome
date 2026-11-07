//go:build cgo

package tests_test

import (
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/adapter"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/rules/tests"
)

// viewpoint: external contract
// target: tests/no-sleep through engine.Engine.Check
// A call written over several lines shows its first line and "…" in the message, never the whole
// text with its line breaks.
func TestAMultilineHazardShowsItsFirstLine(t *testing.T) {
	tree := func(body string) fstest.MapFS {
		return fstest.MapFS{
			"src/store.ts": {Data: []byte("/** Appends a row. */\nexport function append(row: string) {}\n")},
			"src/store.test.ts": {Data: []byte("import { append } from './store'\n\nit('appends', () => {\n  append('a')\n" +
				body + "})\n")},
		}
	}
	e := &engine.Engine{Analyzer: adapter.New(), Rules: tests.All()}
	r, err := e.Check(trees{base: tree(""), head: tree("  setTimeout(() => {\n    append('b')\n  }, 10)\n")})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range r.Regressions {
		if f.Rule == "tests/no-sleep" {
			if got, want := f.Message.In("en"), "test waits by sleeping: setTimeout(() => { …"; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
			return
		}
	}
	t.Fatalf("no tests/no-sleep among %+v", r.Regressions)
}
