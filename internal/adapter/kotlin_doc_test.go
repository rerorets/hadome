//go:build cgo

package adapter_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/adapter"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/rules/comments"
)

// viewpoint: external contract
// target: comments/missing-doc through engine.Engine.Check
// A KDoc comment above a top-level Kotlin declaration documents it when an annotation with
// arguments stands on its own line between them, as it does without the annotation. The grammar
// splits such an annotation off the declaration after a declaration that ends in an expression, so
// each file starts with one.
func TestKotlinDocAboveAnAnnotationWithArguments(t *testing.T) {
	kt := func(src string) fstest.MapFS {
		return fstest.MapFS{"src/main/kotlin/app/Store.kt": {Data: []byte("package app\n\n/** Two. */\nfun two() = 2\n\n" + src)}}
	}
	cases := []struct {
		name, head string
		fails      bool
	}{
		{"a class with a doc", "/** Keeps rows. */\n@Suppress(\"unused\")\nclass Store\n", false},
		{"a function with a doc", "/** Appends a row. */\n@JvmName(\"add\")\nfun append(row: String) {}\n", false},
		{"a class without one", "@Suppress(\"unused\")\nclass Store\n", true},
		{"a doc for something else, a blank line above", "/** Keeps rows. */\n\n@Suppress(\"unused\")\nclass Store\n", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := &engine.Engine{Analyzer: adapter.New(), Rules: comments.All()}
			r, err := e.Check(trees{base: kt(""), head: kt(tc.head)})
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, f := range r.Regressions {
				got = append(got, f.Rule)
			}
			if fails := strings.Contains(strings.Join(got, ","), "comments/missing-doc"); fails != tc.fails {
				t.Errorf("missing-doc reported: %t, want %t (regressions %v)", fails, tc.fails, got)
			}
		})
	}
}
