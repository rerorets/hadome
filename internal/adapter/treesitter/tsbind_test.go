//go:build cgo

package treesitter

import "testing"

// viewpoint: behaviour
// target: the TypeScript reader
// A method called on a value the test builds itself, from literals, built-ins or an external
// library, is not a call of the repository, also through a local helper that receives it. So
// tests that call nothing of the repository all come out alike, with no unidentified call. A
// method of a value the repository returns, or of a parameter the test receives, stays
// unidentified.
func TestMethodsOfValuesTheTestBuildsAreNotUnresolved(t *testing.T) {
	expect(t, calls(t, "typescript",
		"worker/queue.ts", "export function drain(n: number) { return n }\nexport function open() { return { close() {} } }\n",
		"worker/queue.test.mjs", `import { execFileSync } from 'node:child_process'
import userEvent from '@testing-library/user-event'
import { open } from './queue'

function point(n) { return { n } }
function many() { const out = []; for (let i = 0; i < 3; i++) out.push(point(i)); return out }

it('reduce', () => { const xs = [1, 2]; expect(xs.reduce((s, x) => s + x, 0)).toBe(3) })
it('max', () => { const xs = [1, 2]; expect(Math.max(...xs)).toBe(2) })
it('nested', () => { const o = { xs: [] }; o.xs.push(1); expect(o.xs.length).toBe(1) })
it('parsed', () => {
  const out = JSON.parse(execFileSync('node', ['run.mjs']))
  const pick = (arr, k) => arr.find((x) => x.k === k)
  expect(pick(out.items, 'a').n).toBe(1)
})
it('helper', () => { expect(many().length).toBe(3) })
it('external', async () => { const user = userEvent.setup(); await user.click() })
it('product', () => { const c = open(); c.close() })
it('fixture', ({ task }) => { task.run() })
`), `external calls=[] unresolved=0
fixture calls=[] unresolved=1
helper calls=[] unresolved=0
max calls=[] unresolved=0
nested calls=[] unresolved=0
parsed calls=[] unresolved=0
product calls=[worker.open] unresolved=1
reduce calls=[] unresolved=0`)
}

// viewpoint: behaviour
// target: the TypeScript reader
// A test support file contributes the tests it declares at its top, as mocha reads test/*.js, but
// not those a function of it declares for the file that calls it.
func TestSupportFilesDeclareNoTestsInTheirHelpers(t *testing.T) {
	expect(t, calls(t, "typescript",
		"src/calc.ts", "export function twice(n: number) { return n * 2 }\n",
		"tests/helpers/gate.ts", `export function describeIfReady() {
  return (name: string) => describe(name, () => { it('needs the server', () => { throw new Error('x') }) })
}
export const gated = () => it('gated', () => {})
`,
		"test/calc.js", `const { twice } = require('../src/calc')
describe('calc', () => {
  it('twice', () => { expect(twice(1)).toBe(2) })
  ;[1, 2].forEach((n) => it('each row', () => { expect(twice(n)).toBe(n * 2) }))
})
`), `calc each row calls=[src.twice] unresolved=0
calc twice calls=[src.twice] unresolved=0`)
}

// viewpoint: behaviour
// target: the TypeScript reader
// A helper's parameters hold what the call passes in their position, whatever they are named: a
// module loaded with import(), a function of the repository, or a module object. A parameter that
// shares a name with an outer module but receives something else does not reach that module.
func TestHelperParametersHoldTheArgumentsByPosition(t *testing.T) {
	expect(t, calls(t, "typescript",
		"src/api/route.ts", "export async function GET(id: string) { return id }\nexport function append(n: number) { return n }\n",
		"src/api/route.test.ts", `import { describe, it, expect, beforeAll } from 'vitest'
import { append } from './route'
import * as api from './route'
type Route = { GET: (id: string) => Promise<string> }
describe('route', () => {
  let route: Route
  beforeAll(async () => { route = await import('./route') })
  async function call(r: Route, id: string) { return r.GET(id) }
  function apply(fn: (n: number) => number) { return fn(1) }
  function other(route: { GET(): void }) { route.GET() }
  it('renamed', async () => { await expect(call(route, 'a')).resolves.toBe('a') })
  it('namespace', async () => { await expect(call(api, 'a')).resolves.toBe('a') })
  it('function', () => { expect(apply(append)).toBe(1) })
  it('shadowed', () => { other({ GET() {} }) })
})
`), `route function calls=[src/api.append] unresolved=0
route namespace calls=[src/api.GET] unresolved=0
route renamed calls=[src/api.GET] unresolved=0
route shadowed calls=[] unresolved=0`)
}

// viewpoint: behaviour
// target: the TypeScript reader
// A helper that loads a module with import() and returns one of its functions names that function:
// what a test takes from its result and calls is that function. A helper that returns a different
// function on different paths names none.
func TestLoadersReturningAFunctionNameIt(t *testing.T) {
	expect(t, calls(t, "typescript",
		"src/store/index.ts", "export function append(n: number) { return n }\nexport function remove(n: number) { return n }\n",
		"src/store/index.test.ts", `async function load() { const m = await import('./index'); return m.append }
async function pick() { const { remove } = await import('./index'); return remove }
const direct = async () => (await import('./index')).append
async function either(a: boolean) { const m = await import('./index'); if (a) return m.append; return m.remove }

it('member', async () => { const fn = await load(); expect(fn(1)).toBe(1) })
it('destructured', async () => { const fn = await pick(); expect(fn(1)).toBe(1) })
it('arrow', async () => { const fn = await direct(); expect(fn(1)).toBe(1) })
it('either', async () => { const fn = await either(true); expect(fn(1)).toBe(1) })
`), `arrow calls=[src/store.append] unresolved=0
destructured calls=[src/store.remove] unresolved=0
either calls=[] unresolved=0
member calls=[src/store.append] unresolved=0`)
}

// viewpoint: behaviour
// target: the TypeScript reader
// The parameters of a describe.each or it.each callback hold the functions and components of the
// repository in the table's rows, by position or by key, so a test that calls one calls each of
// them. A row value the repository does not hold binds nothing.
func TestEachParametersHoldTheTableRows(t *testing.T) {
	expect(t, calls(t, "typescript",
		"src/ops.ts", "export function append(n: number) { return n }\nexport function remove(n: number) { return n }\n",
		"src/ui/Badge.tsx", "export function Badge() { return <b /> }\nexport function Chip() { return <i /> }\n",
		"src/ops.test.tsx", `import { append, remove } from './ops'
import { Badge, Chip } from './ui/Badge'

describe.each([[append, 1], [remove, 2]])('%s', (fn, n) => {
  it('runs', () => { expect(fn(n)).toBe(n) })
})
describe.each([{ op: append }, { op: remove }])('$op', ({ op }) => {
  it('keyed', () => { expect(op(1)).toBe(1) })
})
it.each([Badge, Chip])('renders %s', (Comp) => { render(<Comp />) })
it.each([[(n: number) => n]])('inline', (fn) => { expect(fn(1)).toBe(1) })
`), `$op keyed calls=[src.append src.remove] unresolved=0
%s runs calls=[src.append src.remove] unresolved=0
inline calls=[] unresolved=0
renders %s calls=[src/ui.Badge src/ui.Chip] unresolved=0`)
}
