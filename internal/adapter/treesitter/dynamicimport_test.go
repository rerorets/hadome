//go:build cgo

package treesitter

import "testing"

// viewpoint: behaviour
// target: the TypeScript reader
// A test that loads a module of the repository with import(), directly or through a helper of the
// file that loads a fresh copy, calls that module's functions through the names it takes. A helper
// that loads two modules, or a module named by something other than a string, stays unidentified.
func TestNamesFromImportCallsReachTheModule(t *testing.T) {
	expect(t, calls(t, "typescript",
		"src/mail.ts", "export function send(to: string): boolean { return to !== '' }\n",
		"src/log.ts", "export function write(s: string): void {}\n",
		"src/mail.test.ts", `async function fresh() {
  vi.resetModules()
  return await import('./mail')
}

const both = async () => [await import('./mail'), await import('./log')]

describe('send', () => {
  it('direct', async () => {
    const { send } = await import('./mail')
    expect(send('a')).toBe(true)
  })
  it('renamed', async () => {
    const { send: deliver } = await import('./mail')
    expect(deliver('a')).toBe(true)
  })
  it('module', async () => {
    const mail = await fresh()
    expect(mail.send('a')).toBe(true)
  })
  it('helper', async () => {
    const { send } = await fresh()
    expect(send('a')).toBe(true)
  })
  it('two', async () => {
    const [mail] = await both()
    expect(mail.send('a')).toBe(true)
  })
})
`), `send direct calls=[src.send] unresolved=0
send helper calls=[src.send] unresolved=0
send module calls=[src.send] unresolved=0
send renamed calls=[src.send] unresolved=0
send two calls=[] unresolved=1`)
}

// viewpoint: behaviour
// target: the TypeScript reader
// A module of the repository loaded with import() and assigned to a variable inside the test, rather
// than declared with it, is reached through that variable too.
func TestAModuleAssignedInTheTestReachesTheModule(t *testing.T) {
	expect(t, calls(t, "typescript",
		"src/mail.ts", "export function send(to: string): boolean { return to !== '' }\n",
		"src/mail.test.ts", `it('assigned', async () => {
  let mail
  mail = await import('./mail')
  expect(mail.send('a')).toBe(true)
})
`), "assigned calls=[src.send] unresolved=0")
}
