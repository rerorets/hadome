// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

package code

import (
	"fmt"
	"slices"
	"testing"

	"github.com/rerorets/hadome/internal/adapter"
	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
)

// viewpoint: behaviour
// target: code.All
// The code/ rules are the four the design names, and each passes its own self-test cases.
func TestRulesPassTheirCases(t *testing.T) {
	var ids []string
	for _, r := range All() {
		ids = append(ids, r.ID())
	}
	if want := []string{"code/config-literal", "code/swallowed-error", "code/duplicate", "code/diverged-copy"}; !slices.Equal(ids, want) {
		t.Errorf("got rules %v, want %v", ids, want)
	}
	e := &engine.Engine{Analyzer: adapter.New(), Rules: All(), Workdir: "."}
	for _, f := range e.SelfTest() {
		t.Errorf("%s: %s", f.Name, f.Err)
	}
}

// copyOf is a function with body body at path:line, calling calls.
func copyOf(id, path string, line int, body string, calls ...string) facts.Symbol {
	return facts.Symbol{ID: id, Path: path, Line: line, Kind: facts.SymbolFunction, Body: body, BodyTokens: 100, Calls: calls}
}

// version is a version holding the given functions, in Go files built under the given conditions,
// keyed by path.
func version(builds map[string]string, syms ...facts.Symbol) engine.Input {
	a := &facts.Analysis{Symbols: syms}
	seen := map[string]bool{}
	for _, s := range syms {
		if !seen[s.Path] {
			seen[s.Path] = true
			a.Files = append(a.Files, facts.File{Path: s.Path, Language: "go", Build: builds[s.Path]})
		}
	}
	return engine.Input{Facts: a, Config: config.Config{CodeSettings: config.CodeSettings{MinTokens: config.DefaultMinTokens}}}
}

func places(fs []engine.Finding) []string {
	var out []string
	for _, f := range fs {
		out = append(out, fmt.Sprintf("%s:%d", f.File, f.Line))
	}
	return out
}

// viewpoint: behaviour
// target: code.duplicate.FindAgainst
// A group of copies is reported at its first function in file and line order, whatever order the
// functions were read in, and groups come in the order of their first functions.
func TestDuplicateGroupsAreInFileAndLineOrder(t *testing.T) {
	head := version(nil,
		copyOf("app/b.Z", "b/z.go", 1, "A"), copyOf("app/b.Z2", "b/z.go", 2, "A"),
		copyOf("app/a.X", "a/x.go", 9, "B"), copyOf("app/a.Y", "a/x.go", 3, "B"),
		copyOf("app/a.W", "a/w.go", 7, "B", "q"))
	got, err := duplicate{}.FindAgainst(version(nil), head)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a/x.go:3", "b/z.go:1"}; !slices.Equal(places(got), want) {
		t.Errorf("got groups at %v, want %v", places(got), want)
	}
}

// viewpoint: behaviour
// target: code.duplicate.FindAgainst
// Functions with the same body in one file are copies whatever they call. In different files they
// are copies only when they call the same known functions, in any order, and neither calls a value of
// unknown type; in Go files never built together they are versions, not copies.
func TestWhichFunctionsAreCopies(t *testing.T) {
	cases := []struct {
		name   string
		builds map[string]string
		a, b   facts.Symbol
		copies bool
	}{
		{"one file, different calls", nil, copyOf("p.A", "p/a.go", 1, "X", "f"), copyOf("p.B", "p/a.go", 9, "X", "g"), true},
		{"two files, different calls", nil, copyOf("p.A", "p/a.go", 1, "X", "f"), copyOf("q.B", "q/b.go", 1, "X", "g"), false},
		{"two files, the same calls in another order", nil, copyOf("p.A", "p/a.go", 1, "X", "f", "g"),
			copyOf("q.B", "q/b.go", 1, "X", "g", "f"), true},
		{"two files built for different systems", map[string]string{"p/a_linux.go": "linux", "p/a_windows.go": "windows"},
			copyOf("p.A", "p/a_linux.go", 1, "X", "f"), copyOf("p.B", "p/a_windows.go", 1, "X", "f"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := duplicate{}.FindAgainst(version(c.builds), version(c.builds, c.a, c.b))
			if err != nil {
				t.Fatal(err)
			}
			if (len(got) == 1) != c.copies {
				t.Errorf("got %d groups, want copies %v", len(got), c.copies)
			}
		})
	}
	unknown := func(s facts.Symbol) facts.Symbol { s.Unfollowed = 1; return s }
	got, _ := duplicate{}.FindAgainst(version(nil), version(nil,
		unknown(copyOf("p.A", "p/a.go", 1, "X", "f")), unknown(copyOf("q.B", "q/b.go", 1, "X", "f"))))
	if len(got) != 0 {
		t.Errorf("functions in two files calling values of unknown type were taken for copies: %v", places(got))
	}
}
