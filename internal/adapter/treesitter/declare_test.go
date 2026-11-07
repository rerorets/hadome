//go:build cgo

package treesitter

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// tsTestFacts renders each TypeScript test with what it calls, how many assertions it makes, its
// hazards and its skip: never runs (skip), runs only in some cases (skipIf), with the reason.
func tsTestFacts(t *testing.T, files ...string) string {
	t.Helper()
	var out []string
	for _, ts := range analyze(t, "typescript", tree(files...)).Tests {
		var targets, hz []string
		for _, c := range ts.Calls {
			targets = append(targets, fmt.Sprintf("%s×%d", c.Target, c.Times))
		}
		for _, h := range ts.Hazards {
			hz = append(hz, string(h.Category))
		}
		skip := ""
		switch {
		case ts.Skip.Skipped:
			skip = fmt.Sprintf(" skip=%q", ts.Skip.Reason)
		case ts.Skip.Conditional:
			skip = fmt.Sprintf(" skipIf=%q", ts.Skip.Reason)
		}
		out = append(out, fmt.Sprintf("%s calls=%v reads=%d asserts=%d hazards=%v%s", ts.Name, targets, ts.Reads, ts.Asserts, hz, skip))
	}
	sort.Strings(out)
	return strings.Join(out, "\n")
}

const tsFmt = "export function fmt(n: number): string { return String(n) }\nexport function pad(s: string): string { return ' ' + s }\n"

// viewpoint: behaviour
// target: the TypeScript reader
// A test declared through a function that takes a table or a condition first is a test: it.each
// and test.each with an array or a tagged template, test.concurrent.each, and describe.each, whose
// name stays in the names of its tests. The table runs with the test, so what it calls counts.
// it.skipIf and it.runIf skip the test only in some cases, and the skip takes its reason from the
// comment on its line, like any other skip.
func TestCurriedDeclarationsAreTests(t *testing.T) {
	expect(t, tsTestFacts(t,
		"src/fmt.ts", tsFmt,
		"src/fmt.test.ts", "import { describe, it, test, expect } from 'vitest'\n"+
			"import { fmt, pad } from './fmt'\n\n"+
			"describe('fmt', () => {\n"+
			"  it.each([[1, '1']])('array %s', (n, s) => { expect(fmt(n)).toBe(s) })\n"+
			"  test.each`\n    n    | s\n    ${1} | ${pad('1')}\n  `('template $n', ({ n, s }) => { expect(fmt(n)).toBe(s) })\n"+
			"  test.concurrent.each([[2]])('concurrent %s', async (n) => { expect(fmt(n)).toBe('2') })\n"+
			"  describe.each([[3]])('row %s', (n) => {\n"+
			"    it('formats', () => { expect(fmt(n)).toBe('3') })\n"+
			"  })\n"+
			"  // not on windows\n"+
			"  it.skipIf(process.platform === 'win32')('posix only', () => { expect(fmt(4)).toBe('4') })\n"+
			"  it.runIf(process.env.CI)('ci only', () => { expect(fmt(5)).toBe('5') })\n"+
			"})\n"),
		`fmt array %s calls=[src.fmt×1] reads=0 asserts=1 hazards=[]
fmt ci only calls=[src.fmt×1] reads=0 asserts=1 hazards=[] skipIf=""
fmt concurrent %s calls=[src.fmt×1] reads=0 asserts=1 hazards=[]
fmt posix only calls=[src.fmt×1] reads=0 asserts=1 hazards=[] skipIf="not on windows"
fmt row %s formats calls=[src.fmt×1] reads=0 asserts=1 hazards=[]
fmt template $n calls=[src.fmt×1 src.pad×1] reads=0 asserts=1 hazards=[]`)
}

// viewpoint: behaviour
// target: the TypeScript reader
// The function of a test or a describe block is its last argument that is a function: vitest takes
// options between the name and the function, and jest takes a timeout after it. A function of the
// file passed by name is followed.
func TestTheBodyIsTheLastFunctionArgument(t *testing.T) {
	expect(t, tsTestFacts(t,
		"src/fmt.ts", tsFmt,
		"src/fmt.test.ts", `import { describe, it, expect } from 'vitest'
import { fmt } from './fmt'

function formatsThree() { expect(fmt(3)).toBe('3') }

describe('fmt', { timeout: 100 }, () => {
  it('with options', { retry: 2 }, () => { expect(fmt(1)).toBe('1') })
  it('with a timeout', () => { expect(fmt(2)).toBe('2') }, 5000)
  it('by name', formatsThree)
})
`),
		`fmt by name calls=[src.fmt×1] reads=0 asserts=1 hazards=[]
fmt with a timeout calls=[src.fmt×1] reads=0 asserts=1 hazards=[]
fmt with options calls=[src.fmt×1] reads=0 asserts=1 hazards=[]`)
}

// viewpoint: behaviour
// target: the TypeScript reader
// Playwright's test.skip and test.fixme with a condition, called in a test or in a describe block,
// skip the test only in some cases and are not tests themselves. The reason is the description
// after the condition. With no arguments in a test they skip it always; a skip with a name and a
// function is still a skipped test.
func TestPlaywrightConditionalSkipsAreNotTests(t *testing.T) {
	expect(t, tsTestFacts(t,
		"src/fmt.ts", tsFmt,
		"src/fmt.spec.ts", `import { test, expect } from '@playwright/test'
import { fmt } from './fmt'

test.describe('page', () => {
  test.skip(({ browserName }) => browserName === 'webkit', 'no clipboard on webkit')

  test('copies', async () => { expect(fmt(1)).toBe('1') })
})

test('in body', async ({ browserName }) => {
  test.skip(browserName === 'firefox', 'flaky on firefox')
  expect(fmt(2)).toBe('2')
})

test('fixme in body', async () => {
  test.fixme()
  expect(fmt(3)).toBe('3')
})

test.skip('declared skipped', async () => { expect(fmt(4)).toBe('4') })
`),
		`declared skipped calls=[src.fmt×1] reads=0 asserts=1 hazards=[] skip=""
fixme in body calls=[src.fmt×1] reads=0 asserts=1 hazards=[] skip=""
in body calls=[src.fmt×1] reads=0 asserts=1 hazards=[] skipIf="flaky on firefox"
page copies calls=[src.fmt×1] reads=0 asserts=1 hazards=[] skipIf="no clipboard on webkit"`)
}

// viewpoint: behaviour
// target: the TypeScript reader
// expect(x) checks something only with a matcher after it: called, through .not or .resolves, or
// chai's assertion by property (expect(x).to.be.true). expect(x) alone and a matcher named but not
// called check nothing.
func TestExpectWithoutAMatcherIsNoAssertion(t *testing.T) {
	expect(t, tsTestFacts(t,
		"src/fmt.ts", tsFmt,
		"src/fmt.test.ts", `import { it, expect } from 'vitest'
import { fmt } from './fmt'

it('alone', () => { expect(fmt(1)); })
it('not called', () => { expect(fmt(1)).toBeTruthy; })
it('called', () => { expect(fmt(1)).toBeTruthy() })
it('negated', () => { expect(fmt(1)).not.toBe('2') })
it('resolves', async () => { await expect(Promise.resolve(fmt(1))).resolves.toBe('1') })
it('chai', () => { expect(fmt(1)).to.be.ok })
it('count', () => { expect.assertions(1); fmt(1) })
`),
		`alone calls=[src.fmt×1] reads=0 asserts=0 hazards=[]
called calls=[src.fmt×1] reads=0 asserts=1 hazards=[]
chai calls=[src.fmt×1] reads=0 asserts=1 hazards=[]
count calls=[src.fmt×1] reads=0 asserts=1 hazards=[]
negated calls=[src.fmt×1] reads=0 asserts=1 hazards=[]
not called calls=[src.fmt×1] reads=0 asserts=0 hazards=[]
resolves calls=[src.fmt×1] reads=0 asserts=1 hazards=[]`)
}

// viewpoint: behaviour
// target: the TypeScript reader
// After the test turns on fake timers, in its body, in a beforeEach or at the top of its describe
// block, Date and setTimeout read the fake clock and are no hazard. Before that, and after the
// real timers come back, they are.
func TestFakeTimersAreNoClockHazard(t *testing.T) {
	expect(t, tsTestFacts(t,
		"src/fmt.ts", tsFmt,
		"src/fmt.test.ts", `import { describe, it, expect, vi, beforeEach } from 'vitest'
import { fmt } from './fmt'

it('real', () => { expect(fmt(Date.now())).toBeTruthy() })
it('faked in the body', () => {
  vi.useFakeTimers().setSystemTime(new Date(0))
  expect(fmt(Date.now())).toBe('0')
  setTimeout(() => {}, 10)
})
it('faked then real', () => {
  vi.useFakeTimers()
  vi.useRealTimers()
  expect(fmt(new Date().getTime())).toBeTruthy()
})
describe('hook', () => {
  beforeEach(() => { vi.useFakeTimers() })
  it('faked', () => { expect(fmt(Date.now())).toBe('0') })
})
describe('block', () => {
  vi.setSystemTime(0)
  it('faked', () => { expect(fmt(new Date().getTime())).toBe('0') })
})
`),
		`block faked calls=[src.fmt×1] reads=0 asserts=1 hazards=[]
faked in the body calls=[src.fmt×1] reads=0 asserts=1 hazards=[]
faked then real calls=[src.fmt×1] reads=0 asserts=1 hazards=[clock]
hook faked calls=[src.fmt×1] reads=0 asserts=1 hazards=[]
real calls=[src.fmt×1] reads=0 asserts=1 hazards=[clock]`)
}
