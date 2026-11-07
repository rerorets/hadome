//go:build cgo

package treesitter

import (
	"strings"
	"testing"
)

// lines keeps the lines of a summary that start with one of prefixes.
func lines(summary string, prefixes ...string) string {
	var out []string
	for _, l := range strings.Split(summary, "\n") {
		for _, p := range prefixes {
			if strings.HasPrefix(l, p) {
				out = append(out, l)
				break
			}
		}
	}
	return strings.Join(out, "\n")
}

// viewpoint: behaviour
// target: the TypeScript reader
// An alias is read from the tsconfig.json nearest the importing file, with what it extends: in a
// repository of several applications, each resolves its own aliases, and the root's do not apply.
func TestTypeScriptAliasesComeFromTheNearestTsconfig(t *testing.T) {
	a := analyze(t, "typescript", tree(
		"tsconfig.json", `{ "compilerOptions": { "baseUrl": ".", "paths": { "@/*": ["elsewhere/*"] } } }`,
		"tsconfig.base.json", `{
  // shared by every application
  "compilerOptions": { "paths": { "#lib/*": ["libs/lib/src/*"], }, },
}`,
		"apps/api/tsconfig.json", `{ "extends": "../../tsconfig.base.json" }`,
		"apps/web/tsconfig.json", `{ "extends": "../../tsconfig.base", "compilerOptions": { "baseUrl": ".", "paths": { "@/*": ["src/*"] } } }`,
		"libs/lib/src/sum.ts", "export function sum(a: number, b: number) { return a + b }\n",
		"apps/web/src/store.ts", "export function append(row: string) { return row }\n",
		"apps/api/src/sum.test.ts", "import { sum } from '#lib/sum'\n\nit('sums', () => { expect(sum(1, 2)).toBe(3) })\n",
		"apps/web/src/store.test.ts", "import { append } from '@/store'\n\nit('appends', () => { expect(append('a')).toBe('a') })\n",
	))
	expect(t, lines(summary(a), "test "), `
test "appends" calls=[apps/web/src.append×1/1] hazards=[] skip=false:"" unresolved=0
test "sums" calls=[libs/lib/src.sum×1/1] hazards=[] skip=false:"" unresolved=0
`)
}

// viewpoint: behaviour
// target: the TypeScript reader
// A Vite project keeps its aliases in tsconfig.app.json, which the root tsconfig.json only
// references; and ESM TypeScript imports './a.js' to load a.ts.
func TestTypeScriptAliasesOfAReferencedConfigAndJSSpecifiers(t *testing.T) {
	a := analyze(t, "typescript", tree(
		"tsconfig.json", `{ "files": [], "references": [{ "path": "./tsconfig.node.json" }, { "path": "./tsconfig.app.json" }] }`,
		"tsconfig.node.json", `{ "compilerOptions": { "composite": true } }`,
		"tsconfig.app.json", `{ "compilerOptions": { "paths": { "~/*": ["./src/*"] } } }`,
		"src/lib/rows.ts", "export function rows() { return [] }\n",
		"src/lib/cells.ts", "export function cells() { return [] }\n",
		"src/view.test.ts", "import { rows } from '~/lib/rows'\nimport { cells } from './lib/cells.js'\n\nit('lists', () => { expect(rows()).toEqual(cells()) })\n",
	))
	expect(t, lines(summary(a), "test "), `
test "lists" calls=[src/lib.cells×1/0 src/lib.rows×1/0] hazards=[] skip=false:"" unresolved=0
`)
}

// viewpoint: behaviour
// target: the TypeScript reader
// An import that looks like the repository's own (a relative path, an alias) but names no file
// is a call hadome cannot identify, not a call of an outside package: the test is unresolved,
// not one that guards nothing.
func TestTypeScriptMissingLocalImportsAreUnresolved(t *testing.T) {
	a := analyze(t, "typescript", tree(
		"tsconfig.json", `{ "compilerOptions": { "paths": { "@/*": ["src/*"] } } }`,
		"src/rows.ts", "export function rows() { return [] }\n",
		"src/rows.test.ts", `import { gone } from './gone'
import * as old from '@/old/rows'
import { chunk } from 'lodash'
import { rows } from './rows'

it('relative', () => { expect(gone()).toBe(1) })
it('alias', () => { expect(old.rows()).toEqual([]) })
it('outside', () => { expect(chunk([1], 1)).toEqual([[1]]) })
it('found', () => { expect(rows()).toEqual([]) })
`,
	))
	expect(t, lines(summary(a), "test "), `
test "alias" calls=[] hazards=[] skip=false:"" unresolved=1
test "found" calls=[src.rows×1/0] hazards=[] skip=false:"" unresolved=0
test "outside" calls=[] hazards=[] skip=false:"" unresolved=0
test "relative" calls=[] hazards=[] skip=false:"" unresolved=1
`)
}

// viewpoint: behaviour
// target: the TypeScript reader
// A package of the repository, imported by the name in its package.json, resolves to its source:
// src/index first, a dist entry mapped back to src, and a path after the name inside the package.
// Product code importing it depends on that source.
func TestTypeScriptWorkspacePackagesResolveToTheirSource(t *testing.T) {
	a := analyze(t, "typescript", tree(
		"package.json", `{ "name": "root", "workspaces": ["packages/*", "apps/*"] }`,
		"packages/shared/package.json", `{ "name": "@acme/shared", "main": "dist/index.js" }`,
		"packages/shared/src/index.ts", "export * from './money'\n",
		"packages/shared/src/money.ts", "export function cents(n: number) { return n * 100 }\n",
		"packages/util/package.json", `{ "name": "util", "exports": { ".": { "types": "./dist/util.d.ts", "import": "./dist/util.js" } } }`,
		"packages/util/src/util.ts", "export function twice(n: number) { return n * 2 }\n",
		"node_modules/left/package.json", `{ "name": "left" }`,
		"apps/web/src/price.ts", "import { cents } from '@acme/shared'\nexport const price = (n: number) => cents(n)\n",
		"apps/web/src/price.test.ts", `import { cents } from '@acme/shared'
import { cents as c2 } from '@acme/shared/money'
import { twice } from 'util'
import { gone } from '@acme/shared/gone'

it('cents', () => { expect(cents(1)).toBe(c2(1)) })
it('twice', () => { expect(twice(1)).toBe(2) })
it('gone', () => { expect(gone()).toBe(2) })
`,
	))
	expect(t, lines(summary(a), "test ", "import "), `
import apps/web/src -> packages/shared/src internal=true io=false
import packages/shared/src -> packages/shared/src internal=true io=false
test "cents" calls=[packages/shared/src.cents×2/1] hazards=[] skip=false:"" unresolved=0
test "gone" calls=[] hazards=[] skip=false:"" unresolved=1
test "twice" calls=[packages/util/src.twice×1/1] hazards=[] skip=false:"" unresolved=0
`)
}

// viewpoint: behaviour
// target: the TypeScript reader
// Two files of one directory that declare the same name are two targets, each named by its file;
// a name declared once keeps the directory's ID. An import through a barrel (export ... from) and a
// default export reach the file that declares the name.
func TestTypeScriptCollidingNamesAreTargetsOfTheirFiles(t *testing.T) {
	a := analyze(t, "typescript", tree(
		"src/store/a.ts", "export function load() { return 'a' }\nexport function only() { return 1 }\n",
		"src/store/b.ts", "export function load() { return 'b' }\n",
		"src/store/index.ts", "export { load } from './b'\n",
		"src/page.tsx", "export default function Page() { return <main /> }\n",
		"src/store/a.test.ts", "import { load, only } from './a'\n\nit('a', () => { expect(load() + only()).toBe('a1') })\n",
		"src/store/b.test.ts", "import { load } from './b'\n\nit('b', () => { expect(load()).toBe('b') })\n",
		"src/use.test.tsx", "import { load } from './store'\nimport Page from './page'\n\nit('barrel', () => { render(<Page />); expect(load()).toBe('b') })\n",
	))
	expect(t, lines(summary(a), "test ", "symbol "), `
symbol function src.Page exported=true doc=false params=0 cx=1 methods=0 scoped=false
symbol function src/store.only exported=true doc=false params=0 cx=1 methods=0 scoped=false
symbol function src/store/a.load exported=true doc=false params=0 cx=1 methods=0 scoped=false
symbol function src/store/b.load exported=true doc=false params=0 cx=1 methods=0 scoped=false
test "a" calls=[src/store.only×1/0 src/store/a.load×1/0] hazards=[] skip=false:"" unresolved=0
test "b" calls=[src/store/b.load×1/0] hazards=[] skip=false:"" unresolved=0
test "barrel" calls=[src.Page×1/0 src/store/b.load×1/0] hazards=[] skip=false:"" unresolved=0
`)
}

// viewpoint: behaviour
// target: the TypeScript reader
// require('./x') named by a string binds names like an import, at the top of a test file and inside
// a test.
func TestTypeScriptRequireBindsLikeAnImport(t *testing.T) {
	a := analyze(t, "typescript", tree(
		"src/mail.js", "function send(to) { return to }\nexport { send }\nexport function list() { return [] }\n",
		"src/mail.test.js", `const { send } = require('./mail')
const mail = require('./mail')

it('top', () => { expect(send('a')).toBe(mail.send('a')) })
it('inside', () => {
  const { list: all } = require('./mail')
  expect(all()).toEqual([])
})
`,
	))
	expect(t, lines(summary(a), "test "), `
test "inside" calls=[src.list×1/0] hazards=[] skip=false:"" unresolved=0
test "top" calls=[src.send×2/1] hazards=[] skip=false:"" unresolved=0
`)
}

// viewpoint: behaviour
// target: the TypeScript reader
// Files under test/ and the like are test support, not product: they hold no targets, a test that
// calls one of their functions is followed into it and reaches the product code it calls, and a
// name they hold no function for is unresolved. A mocha test under test/ is still a test.
func TestTypeScriptTestSupportFilesAreFollowed(t *testing.T) {
	a := analyze(t, "typescript", tree(
		"src/app.ts", "export function buildApp() { return {} }\nexport function seed() {}\n",
		"test/helpers.ts", `import { buildApp, seed } from '../src/app'
export function createApp() { seed(); return buildApp() }
export const config = { port: 0 }
`,
		"test/index.ts", "export * from './helpers'\n",
		"test/app.ts", "import { seed } from '../src/app'\n\nit('seeds', () => { seed() })\n",
		"src/app.test.ts", `import { createApp, config } from '../test'
import * as h from '../test/helpers'

it('direct', () => { expect(createApp()).toBeTruthy() })
it('namespace', () => { expect(h.createApp()).toBeTruthy() })
it('value', () => { expect(config()).toBeTruthy() })
`,
	))
	expect(t, lines(summary(a), "test ", "file ", "symbol "), `
file src/app.test.ts test unit=src at=src
file src/app.ts source unit=src at=src
file test/app.ts test unit=test at=test
file test/helpers.ts test unit=test at=test
file test/index.ts test unit=test at=test
symbol function src.buildApp exported=true doc=false params=0 cx=1 methods=0 scoped=false
symbol function src.seed exported=true doc=false params=0 cx=1 methods=0 scoped=false
test "direct" calls=[src.buildApp×1/0 src.seed×1/0] hazards=[] skip=false:"" unresolved=0
test "namespace" calls=[src.buildApp×1/0 src.seed×1/0] hazards=[] skip=false:"" unresolved=0
test "seeds" calls=[src.seed×1/0] hazards=[] skip=false:"" unresolved=0
test "value" calls=[] hazards=[] skip=false:"" unresolved=1
`)
}

// viewpoint: behaviour
// target: the TypeScript reader
// Handing a request in memory to an application object drives the application: Hono's
// app.request('/path'), and app.fetch(new Request(...)). A request method of an outside client
// given an options object does not.
func TestTypeScriptInMemoryRequestsDriveTheApplication(t *testing.T) {
	a := analyze(t, "typescript", tree(
		"src/app.ts", "export const app = { request: (p: string) => p, fetch: (r: unknown) => r }\n",
		"src/app.test.ts", `import { app } from './app'
import axios from 'axios'

it('hono', async () => { expect((await app.request('/rows')).status).toBe(200) })
it('fetch', async () => { const res = await server.fetch(new Request('http://localhost/rows')); expect(res.ok).toBe(true) })
it('client', async () => { const c = axios.create(); expect(await c.request({ url: 'x' })).toBeTruthy() })
`,
	))
	var got []string
	for _, ts := range a.Tests {
		if ts.Drives {
			got = append(got, ts.Name)
		}
	}
	expect(t, strings.Join(got, "\n"), "hono\nfetch")
}
