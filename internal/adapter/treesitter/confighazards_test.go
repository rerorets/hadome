//go:build cgo

package treesitter

import (
	"slices"
	"strings"
	"testing"

	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/facts"
)

// viewpoint: behaviour
// target: treesitter.tables.with
// Configuration adds to the hazard tables by category: a module under mock makes a test that
// imports it a mock, and a function under any other category is a call of that hazard.
func TestConfiguredHazardsGoToTheirCategory(t *testing.T) {
	c := config.Analysis{Hazards: map[string][]string{"mock": {"fakelib"}, "sleep": {"pause"}}}
	a := analyzeWith(t, "typescript", tree(
		"src/store.ts", "export function add(a: number, b: number) { return a + b }\n",
		"src/store.test.ts", `import { stub } from "fakelib"
import { add } from "./store"
test("adds", () => {
  pause(1)
  expect(add(1, 2)).toBe(3)
})
`), c)
	if len(a.Tests) != 1 {
		t.Fatalf("want one test, got %d", len(a.Tests))
	}
	var got []string
	for _, h := range a.Tests[0].Hazards {
		got = append(got, string(h.Category))
	}
	for _, want := range []facts.HazardCategory{facts.HazardMock, facts.HazardSleep} {
		if !slices.Contains(got, string(want)) {
			t.Errorf("hazards %s, want %s among them", strings.Join(got, ","), want)
		}
	}
}

// viewpoint: behaviour
// target: the TypeScript reader's test strings
// A name a test declares hides the file's constant of the same name, and a name declared in an
// inner block hides the outer one's string: neither string is read through the name, so the
// call that takes SQL gets a value that is not a string.
func TestADeclaredNameHidesTheOneOutside(t *testing.T) {
	got := texts(t, "typescript", tree(
		"src/store.ts", "export function build() { return 1 }\n",
		"src/store.test.ts", `import { build } from './store';

const Q = 'SELECT hidden';

it('declares Q', () => {
  const Q = build();
  run(Q);
});

it('reads Q', () => {
  run(Q);
});

it('inner block', () => {
  let q = 'SELECT outer';
  {
    const q = build();
    pool.query(q);
  }
});
`), config.Analysis{})
	expect(t, got, strings.Join([]string{
		`inner block src/store.test.ts:15 "SELECT outer" x1`,
		`inner block src/store.test.ts:18 "" sql dynamic x1`,
		`reads Q src/store.test.ts:3 "SELECT hidden" x1`,
	}, "\n"))
}
