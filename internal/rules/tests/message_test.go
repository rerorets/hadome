package tests_test

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
	"unicode/utf8"

	"github.com/rerorets/hadome/internal/adapter"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/rules/tests"
)

type trees struct{ base, head fs.FS }

func (s trees) Base() (engine.Version, error)       { return engine.Version{Label: "base", FS: s.base}, nil }
func (s trees) Head() (engine.Version, error)       { return engine.Version{Label: "head", FS: s.head}, nil }
func (s trees) Renames() (map[string]string, error) { return nil, nil }

// viewpoint: external contract
// target: tests/no-sleep through engine.Engine.Check
// A message is one line that names the call: a long call shows its start and "…".
func TestAHazardMessageIsOneShortLine(t *testing.T) {
	module := func(body string) fstest.MapFS {
		return fstest.MapFS{
			"go.mod":         {Data: []byte("module example.com/app\n")},
			"store/store.go": {Data: []byte("package store\n\n// Append adds a row.\nfunc Append(row string) {}\n")},
			"store/store_test.go": {Data: []byte("package store_test\n\nimport (\n\t\"testing\"\n\t\"time\"\n\n\t\"example.com/app/store\"\n)\n\n" +
				"func TestAppend(t *testing.T) {\n\tstore.Append(\"a\")\n" + body + "}\n")},
		}
	}
	for _, tc := range []struct{ name, call, start string }{
		{"one long line", "\ttime.Sleep(time.Duration(len(\"" + strings.Repeat("a", 120) + "\")) * time.Millisecond)\n", "time.Sleep(time.Duration(len("},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := &engine.Engine{Analyzer: adapter.New(), Rules: tests.All()}
			r, err := e.Check(trees{base: module(""), head: module(tc.call)})
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range r.Regressions {
				if f.Rule != "tests/no-sleep" {
					continue
				}
				msg := f.Message.In("en")
				evidence := strings.TrimPrefix(msg, "test waits by sleeping: ")
				if strings.Contains(msg, "\n") || utf8.RuneCountInString(evidence) > 82 ||
					!strings.HasPrefix(evidence, tc.start) || !strings.HasSuffix(evidence, "…") {
					t.Errorf("want one line starting %q and ending with …, at most about 80 characters, got %q", tc.start, msg)
				}
				return
			}
			t.Fatalf("no tests/no-sleep among %+v", r.Regressions)
		})
	}
}

// viewpoint: external contract
// target: tests/scope-shrunk through engine.Engine.Check
// Every rule shows a symbol the way configuration names it (store.Append), as size/ and
// comments/missing-doc do, not with the unit's whole path.
func TestScopeShrunkNamesTheSymbolAsConfigurationDoes(t *testing.T) {
	product := fstest.MapFS{
		"go.mod":         {Data: []byte("module example.com/app\n")},
		"store/store.go": {Data: []byte("package store\n\n// Append adds a row.\nfunc Append(row string) {}\n")},
	}
	base := fstest.MapFS{"store/store_test.go": {Data: []byte("package store_test\n\nimport (\n\t\"testing\"\n\n\t\"example.com/app/store\"\n)\n\n" +
		"func TestAppend(t *testing.T) {\n\tstore.Append(\"a\")\n}\n")}}
	for p, f := range product {
		base[p] = f
	}
	e := &engine.Engine{Analyzer: adapter.New(), Rules: tests.All()}
	r, err := e.Check(trees{base: base, head: product})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range r.Regressions {
		if f.Rule == "tests/scope-shrunk" {
			msg := f.Message.In("en") + " " + f.Fix.In("en")
			if strings.Contains(msg, "example.com") || !strings.Contains(msg, "store.Append") {
				t.Errorf("want store.Append without the module path, got %q", msg)
			}
			return
		}
	}
	t.Fatalf("no tests/scope-shrunk among %+v", r.Regressions)
}
