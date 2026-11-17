//go:build cgo

package treesitter

import "testing"

// viewpoint: behaviour
// target: the TypeScript reader
// A helper declared in a describe block belongs to that block: when sibling blocks each declare a
// helper of the same name, a test calls the one of its own block, whether it calls it, renders it
// or reaches it through another helper, and whether the file declares its own before or after the
// blocks. A test outside every such block calls the file's own.
func TestATestCallsTheHelperOfItsOwnDescribeBlock(t *testing.T) {
	expect(t, calls(t, "typescript",
		"src/math.ts", "export function double(n: number) { return n * 2 }\nexport function triple(n: number) { return n * 3 }\nexport function half(n: number) { return n / 2 }\n",
		"src/ui/Widget.tsx", "export function Wide() { return <main /> }\nexport function Narrow() { return <span /> }\n",
		"src/math.test.tsx", `import { render } from '@testing-library/react'
import { double, triple, half } from './math'
import { Wide, Narrow } from './ui/Widget'

describe('double', () => {
  const run = (n: number) => double(n)
  function twice(n: number) { return run(run(n)) }
  const mount = () => render(<Wide />)
  test('direct', () => { run(2) })
  test('nested', () => { twice(2) })
  test('render', () => { mount() })
})

describe('triple', () => {
  const run = (n: number) => triple(n)
  function twice(n: number) { return run(run(n)) }
  const Mount = () => <Narrow />
  const mount = () => render(<Mount />)
  test('direct', () => { run(2) })
  test('nested', () => { twice(2) })
  test('render', () => { mount() })
})

test('outside', () => { run(2) })

function run(n: number) { return half(n) }
`), `double direct calls=[src.double] unresolved=0
double nested calls=[src.double] unresolved=0
double render calls=[src/ui.Wide] unresolved=0
outside calls=[src.half] unresolved=0
triple direct calls=[src.triple] unresolved=0
triple nested calls=[src.triple] unresolved=0
triple render calls=[src/ui.Narrow] unresolved=0`)
}

// viewpoint: behaviour
// target: the TypeScript reader
// A loader declared in a describe block belongs to that block too: names taken from what it returns
// come from the module it loads, not from the module a loader of the same name in another block loads.
func TestATestUsesTheLoaderOfItsOwnDescribeBlock(t *testing.T) {
	expect(t, calls(t, "typescript",
		"src/mail/send.ts", "export function send() {}\n",
		"src/sms/send.ts", "export function send() {}\n",
		"src/notify.test.ts", `describe('mail', () => {
  const load = async () => await import('./mail/send')
  const sender = async () => (await import('./mail/send')).send
  test('module', async () => { const { send } = await load(); send() })
  test('function', async () => { const send = await sender(); send() })
})

describe('sms', () => {
  const load = async () => await import('./sms/send')
  const sender = async () => (await import('./sms/send')).send
  test('module', async () => { const { send } = await load(); send() })
  test('function', async () => { const send = await sender(); send() })
})
`), `mail function calls=[src/mail.send] unresolved=0
mail module calls=[src/mail.send] unresolved=0
sms function calls=[src/sms.send] unresolved=0
sms module calls=[src/sms.send] unresolved=0`)
}
