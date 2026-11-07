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

// viewpoint: external contract
// target: tests/purity-lost through engine.Engine.Check
// A pure unit that starts to import a build output hadome does not read is no longer known to be
// pure. The review says what the import names, not that it performs input or output.
func TestPurityLostSaysTheImportNamesADirectoryNotRead(t *testing.T) {
	tree := func(src string) fstest.MapFS {
		return fstest.MapFS{
			"src/run.ts":     {Data: []byte(src)},
			"dist/client.js": {Data: []byte("export const client = () => 1;\n")},
		}
	}
	e := &engine.Engine{Analyzer: adapter.New(), Rules: tests.All()}
	r, err := e.Check(trees{base: tree("export const run = () => 1;\n"),
		head: tree("import { client } from '../dist/client';\nexport const run = () => client();\n")})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range append(r.Regressions, r.Reviews...) {
		got = append(got, f.Rule+" "+f.Message.In("en"))
	}
	want := "tests/purity-lost src is no longer pure: it imports ../dist/client, inside a directory hadome does not read"
	if len(got) != 1 || got[0] != want {
		t.Errorf("want [%s], got %q", want, got)
	}
}
