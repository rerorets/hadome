//go:build cgo

package treesitter

import (
	"fmt"
	"strings"
	"testing"
)

// viewpoint: behaviour
// target: the TypeScript reader
// A test reaches the code through what the file sets up outside it: helpers held by a const or an
// object, with calls in their callbacks, and variables of a describe block declared with a class or
// built in beforeEach. A local of the same name shadows the outer variable, and a name the file
// gives two classes stays unidentified. A variable of a known class wins over an object helper of
// the same name elsewhere in the file. The beforeEach runs before every test of the block, so each of
// them calls the constructors it calls.
func TestTestsReachTheCodeThroughWhatTheFileSetsUp(t *testing.T) {
	expect(t, calls(t, "typescript",
		"src/store.ts", "export class Store {\n  add(row: string): number { return row.length }\n}\nexport class Other {\n  add(row: string): number { return 0 }\n}\nexport function double(n: number): number { return n * 2 }\n",
		"src/store.test.ts", `import assert from 'node:assert/strict'
import { test } from 'node:test'
import { Store, Other, double } from './store'

const viaCallback = (n: number) => [n].map((x) => double(x))
const helpers = { twice: (n: number) => double(n), thrice(n: number) { return double(n) + n } }

describe('setup', () => {
  let typed: Store
  let built = new Store()
  let assigned: Store | undefined
  let mixed = new Store()
  beforeEach(() => { typed = new Store(); assigned = new Store(); mixed = new Other() })

  test('callback', () => { assert.deepEqual(viaCallback(2), [4]) })
  test('object arrow', () => { assert.equal(helpers.twice(2), 4) })
  test('object method', () => { assert.equal(helpers.thrice(2), 6) })
  test('typed', () => { assert.equal(typed.add('x'), 1) })
  test('built', () => { assert.equal(built.add('x'), 1) })
  test('assigned', () => { assert.equal(assigned!.add('x'), 1) })
  test('shadowed', () => { const typed = { add: (s: string) => 1 }; assert.equal(typed.add('x'), 1) })
  test('mixed', () => { assert.equal(mixed.add('x'), 1) })
})
`), `setup assigned calls=[src.Other src.Store src.Store.add] unresolved=0
setup built calls=[src.Other src.Store src.Store.add] unresolved=0
setup callback calls=[src.Other src.Store src.double] unresolved=0
setup mixed calls=[src.Other src.Store] unresolved=0
setup object arrow calls=[src.Other src.Store src.double] unresolved=0
setup object method calls=[src.Other src.Store src.double] unresolved=0
setup shadowed calls=[src.Other src.Store] unresolved=0
setup typed calls=[src.Other src.Store src.Store.add] unresolved=0`)
}

// viewpoint: behaviour
// target: the TypeScript reader
// Helpers are followed each time they are called, so a helper called twice counts its calls twice,
// and helpers that call each other are followed once per chain, not forever. A function expression
// an object holds is a helper too, and an annotation alone types an outer variable.
func TestHelpersAreFollowedPerCall(t *testing.T) {
	a := analyze(t, "typescript", tree(
		"src/store.ts", "export class Store {\n  add(row: string): number { return row.length }\n}\nexport function double(n: number): number { return n * 2 }\n",
		"src/store.test.ts", `import { Store, double } from './store'

function ping(n: number): number { return n > 0 ? pong(n - 1) : double(n) }
function pong(n: number): number { return ping(n) }
const helpers = { quad: function (n: number) { return double(double(n)) } }
const make = () => ({}) as Store

describe('follow', () => {
  let injected: Store
  beforeEach(() => { injected = make() })

  it('twice', () => { expect(ping(0) + ping(0)).toBe(0) })
  it('expression', () => { expect(helpers.quad(1)).toBe(4) })
  it('annotated', () => { expect(injected.add('x')).toBe(1) })
})
`))
	got := map[string]string{}
	for _, ts := range a.Tests {
		var calls []string
		for _, c := range ts.Calls {
			calls = append(calls, fmt.Sprintf("%s×%d", c.Target, c.Times))
		}
		got[ts.Name] = strings.Join(calls, " ")
	}
	want := map[string]string{
		"follow twice":      "src.double×2",
		"follow expression": "src.double×2",
		"follow annotated":  "src.Store.add×1",
	}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s: got %q, want %q", name, got[name], w)
		}
	}
}

// viewpoint: behaviour
// target: the TypeScript reader
// A test runs the setup of the variables it uses that are declared outside it, at the top of the
// file or in a describe block: the initializer, a value assigned in a hook, and the statements there
// that use the variable (register(app)). It also runs the hooks of its describe blocks, the default
// values of the helpers it calls, and the getters it reads. A value set up once counts once however
// often the test uses it, a function an object holds counts only when called, and a local of the
// same name hides the variable.
func TestTestsRunTheSetupOfTheValuesTheyUse(t *testing.T) {
	expect(t, tsTestFacts(t,
		"src/store.ts", `export function createStore() { return { items: [] as number[] } }
export function register(app: object) {}
export function summarize(xs: number[]) { return { total: 0 } }
export function render(n: number) { return n }
export const words = { a: 1 }
`,
		"src/store.test.ts", `import { describe, it, beforeEach, beforeAll, expect } from 'vitest'
import { createStore, register, summarize, render, words } from './store'

const shared = createStore()
const KEYS = Object.keys(words)
const fixture = { get value() { return summarize([1]) }, later: () => render(2) }

function make(s = createStore()) { return s }

it('file level', () => { expect(shared.items).toEqual([]); expect(shared.items).toEqual([]) })
it('reads a constant', () => { expect(KEYS).toEqual(['a']) })
it('getter', () => { expect(fixture.value.total).toBe(0) })
it('default parameter', () => { expect(make().items).toEqual([]) })
it('shadowed', () => { const shared = { items: [] }; expect(shared.items).toEqual([]) })
it('shorthand', () => { expect({ shared }).toBeTruthy() })

describe('hooked', () => {
  let untyped
  beforeEach(() => { untyped = createStore() })
  it('untyped', () => { expect(untyped.items).toEqual([]) })
})

describe('describe body', () => {
  const app = { routes: {} }
  register(app)
  const s = summarize([1, 2])
  beforeAll(() => { render(1) })
  it('registers', () => { expect(app.routes).toEqual({}) })
  it('computes', () => { expect(s.total).toBe(0) })
})
`),
		`default parameter calls=[src.createStore×1] reads=0 asserts=1 hazards=[]
describe body computes calls=[src.render×1 src.summarize×1] reads=0 asserts=1 hazards=[]
describe body registers calls=[src.register×1 src.render×1] reads=0 asserts=1 hazards=[]
file level calls=[src.createStore×1] reads=0 asserts=2 hazards=[]
getter calls=[src.summarize×1] reads=0 asserts=1 hazards=[]
hooked untyped calls=[src.createStore×1] reads=0 asserts=1 hazards=[]
reads a constant calls=[] reads=1 asserts=1 hazards=[]
shadowed calls=[] reads=0 asserts=1 hazards=[]
shorthand calls=[src.createStore×1] reads=0 asserts=1 hazards=[]`)
}

// viewpoint: behaviour
// target: the TypeScript reader
// Names outside the tests taken from a module loaded with import() reach the module: destructured
// at the top of the file, or assigned in beforeAll, also through a type assertion. A bare await
// import() at the top of the file runs the module for every test, and the rows of describe.each
// run with each test inside it.
func TestTestsReachModulesLoadedOutsideThem(t *testing.T) {
	expect(t, tsTestFacts(t,
		"src/store.ts", "export function add(n: number) { return n }\nexport function sub(n: number) { return n }\n",
		"src/routes/setup.ts", "export const ready = true\n",
		"src/store.test.ts", `import { describe, it, beforeAll, expect } from 'vitest'

const { add } = await import('./store')
let mod: typeof import('./store')
let typed: { sub(n: number): number }
beforeAll(async () => {
  mod = await import('./store')
  typed = (await import('./store')) as { sub(n: number): number }
})

it('destructured', () => { expect(add(1)).toBe(1) })
it('assigned', () => { expect(mod.sub(1)).toBe(1) })
it('asserted', () => { expect(typed.sub(1)).toBe(1) })
`,
		"src/routes/routes.test.ts", `import { describe, it, expect } from 'vitest'
import { add } from '../store'

await import('./setup')

describe.each([[add(1)]])('row %s', (n) => {
  it('is one', () => { expect(n).toBe(1) })
})
`),
		`asserted calls=[src.sub×1] reads=0 asserts=1 hazards=[]
assigned calls=[src.sub×1] reads=0 asserts=1 hazards=[]
destructured calls=[src.add×1] reads=0 asserts=1 hazards=[]
row %s is one calls=[src.add×1 src/routes×1] reads=0 asserts=1 hazards=[]`)
}

// viewpoint: behaviour
// target: the TypeScript reader
// A component the test file declares and renders as an element is followed like a helper, so the
// components of the repository it renders count, whether declared with function or held by a const.
func TestComponentsTheFileDeclaresAreFollowed(t *testing.T) {
	expect(t, calls(t, "typescript",
		"src/ui/Field.tsx", "export function Field() { return <input /> }\n",
		"src/ui/Field.test.tsx", `import { render } from '@testing-library/react'
import { Field } from './Field'

function Harness() { return <form><Field /></form> }
const Wrapper = () => <Field />

it('function', () => { render(<Harness />) })
it('const', () => { render(<Wrapper />) })
`), `const calls=[src/ui.Field] unresolved=0
function calls=[src/ui.Field] unresolved=0`)
}

// viewpoint: behaviour
// target: the TypeScript reader
// A helper of a test support file that uses a value the support file sets up at its top runs that
// setup, and a require() in a describe block reaches the module like one at the top of the file.
func TestSetupsReachThroughSupportFilesAndRequire(t *testing.T) {
	expect(t, tsTestFacts(t,
		"src/app.ts", "export function createStore() { return {} }\nexport function seed() {}\n",
		"test/helpers.ts", `import { createStore } from '../src/app'
const shared = createStore()
export function useShared() { return shared }
`,
		"src/app.test.ts", `import { useShared } from '../test/helpers'

it('support', () => { expect(useShared()).toBeTruthy() })
describe('required', () => {
  const app = require('./app')
  it('seeds', () => { expect(app.seed()).toBeUndefined() })
})
`),
		`required seeds calls=[src.seed×1] reads=0 asserts=1 hazards=[]
support calls=[src.createStore×1] reads=0 asserts=1 hazards=[]`)
}
