//go:build cgo

package treesitter

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/adapter/reuse"
	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/facts"
)

// analyze runs one language over a tree and returns its facts.
func analyze(t *testing.T, name string, tree fstest.MapFS) *facts.Analysis {
	t.Helper()
	return analyzeWith(t, name, tree, config.Analysis{})
}

// analyzeWith runs one language over a tree with a configuration.
func analyzeWith(t *testing.T, name string, tree fstest.MapFS, c config.Analysis) *facts.Analysis {
	t.Helper()
	var l *lang
	for _, c := range Languages() {
		if c.name == name {
			l = c
		}
	}
	var paths []string
	for p := range tree {
		if l.Claims(p) {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	a, err := l.Analyze(reuse.Lone(tree), c, paths)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// summary renders facts as sorted lines, so a test states what it expects in one readable block.
func summary(a *facts.Analysis) string {
	var out []string
	for _, f := range a.Files {
		out = append(out, fmt.Sprintf("file %s %s unit=%s at=%s", f.Path, f.Role, f.Unit, f.UnitPath))
	}
	for _, im := range a.Imports {
		out = append(out, fmt.Sprintf("import %s -> %s internal=%t io=%t", im.FromUnit, im.ToUnit, im.Internal, im.IO))
	}
	for _, s := range a.Symbols {
		out = append(out, fmt.Sprintf("symbol %s %s exported=%t doc=%t params=%d cx=%d methods=%d scoped=%t",
			s.Kind, s.ID, s.Exported, s.Doc, s.Params, s.Complexity, s.Methods, s.Scoped))
	}
	for _, ts := range a.Tests {
		var calls, hz []string
		for _, c := range ts.Calls {
			calls = append(calls, fmt.Sprintf("%s×%d/%d", c.Target, c.Times, c.Scopes))
		}
		for _, h := range ts.Hazards {
			hz = append(hz, string(h.Category))
		}
		out = append(out, fmt.Sprintf("test %q calls=[%s] hazards=[%s] skip=%t:%q unresolved=%d", ts.Name,
			strings.Join(calls, " "), strings.Join(hz, " "), ts.Skip.Skipped, ts.Skip.Reason, ts.Unresolved))
	}
	sort.Strings(out)
	return strings.Join(out, "\n")
}

func tree(files ...string) fstest.MapFS {
	t := fstest.MapFS{}
	for i := 0; i+1 < len(files); i += 2 {
		t[files[i]] = &fstest.MapFile{Data: []byte(files[i+1])}
	}
	return t
}

func expect(t *testing.T, got, want string) {
	t.Helper()
	want = strings.TrimSpace(want)
	if got != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", got, want)
	}
}

// viewpoint: behaviour
// target: the TypeScript reader
// The facts rules see, from a small project: units, imports through relative paths and aliases,
// exported and documented symbols, and tests with their calls, hazards and skips.
func TestTypeScriptFacts(t *testing.T) {
	a := analyze(t, "typescript", tree(
		"tsconfig.json", `{ // aliases
  "compilerOptions": { "baseUrl": ".", "paths": { "@/*": ["src/*"] } } }`,
		"src/store/store.ts", `/** Store keeps rows. */
export class Store {
  /** add appends a row. */
  add(row: string): void { if (row && row.length) { this.rows.push(row) } }
  private secret() {}
}
export function createPost(userId: string, body: string) { return body }
export const listPosts = (tenantId: string) => []
function helper() {}
export interface Repo { get(): void; put(): void }
`,
		"src/api/api.ts", `import { createPost } from '@/store/store'
import fs from 'node:fs'
import React from 'react'
const x = require('../store/store')
`,
		"src/store/store.test.ts", `import { createPost, Store } from './store'
import * as st from './store'

describe('posts', () => {
  it('creates once', () => {
    createPost('u1', 'a')
    createPost('u1', 'a')
    const s = new Store()
    s.add('x')
    expect(Date.now()).toBe(Date.now())
  })
  // flaky on CI
  it.skip('later', () => { st.listPosts('t1'); st.listPosts('t2') })
  test('loop', () => {
    for (const x of [1, 2]) { createPost(x, 'b') }
    expect(1).toBe(1)
    const thing = make()
    thing.run()
  })
})
`))
	expect(t, summary(a), `
file src/api/api.ts source unit=src/api at=src/api
file src/store/store.test.ts test unit=src/store at=src/store
file src/store/store.ts source unit=src/store at=src/store
import src/api -> node:fs internal=false io=true
import src/api -> react internal=false io=false
import src/api -> src/store internal=true io=false
import src/api -> src/store internal=true io=false
symbol function src/store.createPost exported=true doc=false params=2 cx=1 methods=0 scoped=true
symbol function src/store.helper exported=false doc=false params=0 cx=1 methods=0 scoped=false
symbol function src/store.listPosts exported=true doc=false params=1 cx=1 methods=0 scoped=true
symbol interface src/store.Repo exported=true doc=false params=0 cx=0 methods=2 scoped=false
symbol method src/store.Store.add exported=true doc=true params=1 cx=3 methods=0 scoped=false
symbol method src/store.Store.secret exported=false doc=false params=0 cx=1 methods=0 scoped=false
symbol type src/store.Store exported=true doc=true params=0 cx=0 methods=0 scoped=false
test "posts creates once" calls=[src/store.Store×1/0 src/store.Store.add×1/1 src/store.createPost×2/1] hazards=[clock] skip=false:"" unresolved=0
test "posts later" calls=[src/store.listPosts×2/2] hazards=[] skip=true:"flaky on CI" unresolved=0
test "posts loop" calls=[src/store.createPost×2/1] hazards=[always-true] skip=false:"" unresolved=1
`)
}

// viewpoint: behaviour
// target: the TypeScript reader
// Bun's test runner mocks a module with mock.module; a test of pure code using it is as unsafe as
// one using jest.mock or vi.mock.
func TestBunModuleMocksAreMockHazards(t *testing.T) {
	a := analyze(t, "typescript", tree(
		"src/store.ts", "export function add(a: number, b: number) { return a + b }\n",
		"src/store.test.ts", `import { test, expect, mock } from "bun:test"
import { add } from "./store"
test("adds", () => {
  mock.module("./other", () => ({}))
  expect(add(1, 2)).toBe(3)
})
`))
	if len(a.Tests) != 1 {
		t.Fatalf("want one test, got %d", len(a.Tests))
	}
	var categories []string
	for _, h := range a.Tests[0].Hazards {
		categories = append(categories, string(h.Category))
	}
	if !slices.Contains(categories, string(facts.HazardMock)) {
		t.Errorf("want a mock hazard, got %v", categories)
	}
}

// viewpoint: behaviour
// target: the TypeScript reader
// A .ts file may use the `<T>x` type assertion and a .tsx or JavaScript file may use JSX. Each is
// read with a grammar that accepts it, so the functions around either come out whole.
func TestTypeAssertionsAndJSXEachReadWhole(t *testing.T) {
	a := analyze(t, "typescript", tree(
		"src/token.ts", `export function token(env: Record<string, unknown>): string {
  const raw = <string>env.TOKEN
  if (raw === "") { return "none" }
  return raw
}
export function size(s: string): number { return s.length }
`,
		"src/view.tsx", `export function View(p: { on: boolean }) {
  if (p.on) { return <img src="/a.svg" /> }
  return <span>off</span>
}
`,
		"src/legacy.jsx", `export function Old(p) {
  if (p.on) { return <b>on</b> }
  return null
}
`))
	var syms []string
	for _, line := range strings.Split(summary(a), "\n") {
		if strings.HasPrefix(line, "symbol ") {
			syms = append(syms, line)
		}
	}
	expect(t, strings.Join(syms, "\n"), `
symbol function src.Old exported=true doc=false params=1 cx=2 methods=0 scoped=false
symbol function src.View exported=true doc=false params=1 cx=2 methods=0 scoped=false
symbol function src.size exported=true doc=false params=1 cx=1 methods=0 scoped=false
symbol function src.token exported=true doc=false params=1 cx=2 methods=0 scoped=false
`)
}
