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
// the same name elsewhere in the file.
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
`), `setup assigned calls=[src.Store.add] unresolved=0
setup built calls=[src.Store.add] unresolved=0
setup callback calls=[src.double] unresolved=0
setup mixed calls=[] unresolved=0
setup object arrow calls=[src.double] unresolved=0
setup object method calls=[src.double] unresolved=0
setup shadowed calls=[] unresolved=0
setup typed calls=[src.Store.add] unresolved=0`)
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
