//go:build cgo

package tests

import (
	"testing/fstest"

	"github.com/rerorets/hadome/internal/engine"
)

func init() {
	languageNoMockCases = tsNoMockCases()
	languagePurityCases = tsPurityCases()
}

// tsTree is a TypeScript package with the given files.
func tsTree(files ...string) fstest.MapFS {
	t := fstest.MapFS{"package.json": {Data: []byte(`{"name":"app"}`)}}
	for i := 0; i+1 < len(files); i += 2 {
		t[files[i]] = &fstest.MapFile{Data: []byte(files[i+1])}
	}
	return t
}

// Two units that import each other: src/ui/a.ts and src/lib/b.ts.
const (
	cycleA       = "import { b } from '../lib/b';\nexport const a = () => b();\n"
	cycleB       = "import { a } from '../ui/a';\nexport const b = () => (a ? 1 : 0);\n"
	cycleBReads  = "import { a } from '../ui/a';\nimport { readFileSync } from 'node:fs';\nexport const b = () => (a ? readFileSync('x').length : 0);\n"
	cycleBFetchs = "import { a } from '../ui/a';\nexport const b = async () => (a ? (await fetch('/rows')).status : 0);\n"
)

func tsPurityCases() []engine.Case {
	typesOnly := "import type { Row } from '../lib/b';\nimport { type Size } from '../lib/b';\nexport const a = (r: Row, s: Size) => [r, s];\n"
	rows := "export type Row = { id: number };\nexport type Size = number;\nexport const b = () => 1;\n"
	rowsRead := "import { readFileSync } from 'node:fs';\nexport type Row = { id: number };\nexport type Size = number;\nexport const b = () => readFileSync('x').length;\n"
	limits := "export const MAX = 3;\n"
	auth := "import { Pool } from 'pg';\ndeclare global { var pool: Pool }\n"
	clamp := "import { MAX } from '../types/limits';\nexport const clamp = (n: number) => Math.min(n, MAX);\n"
	return []engine.Case{
		{Name: "a unit that comes to import a directory whose declaration file imports a database client stays pure",
			Base: tsTree("src/types/limits.ts", limits, "src/types/auth.d.ts", auth, "src/calc/clamp.ts", "export const clamp = (n: number) => n;\n"),
			Head: tsTree("src/types/limits.ts", limits, "src/types/auth.d.ts", auth, "src/calc/clamp.ts", clamp)},
		{Name: "an import cycle that reaches input or output, compared with itself, passes",
			Base: tsTree("src/ui/a.ts", cycleA, "src/lib/b.ts", cycleBReads), Head: tsTree("src/ui/a.ts", cycleA, "src/lib/b.ts", cycleBReads)},
		{Name: "every unit of an import cycle loses its purity when one of them reaches input or output",
			Base: tsTree("src/ui/a.ts", cycleA, "src/lib/b.ts", cycleB), Head: tsTree("src/ui/a.ts", cycleA, "src/lib/b.ts", cycleBReads),
			WantReview: []string{"tests/purity-lost", "tests/purity-lost"}},
		{Name: "calling the global fetch makes a unit, and the units that import it, lose their purity",
			Base: tsTree("src/ui/a.ts", cycleA, "src/lib/b.ts", cycleB), Head: tsTree("src/ui/a.ts", cycleA, "src/lib/b.ts", cycleBFetchs),
			WantReview: []string{"tests/purity-lost", "tests/purity-lost"}},
		{Name: "a unit that imports only types from a unit that loses its purity stays pure",
			Base: tsTree("src/ui/a.ts", typesOnly, "src/lib/b.ts", rows), Head: tsTree("src/ui/a.ts", typesOnly, "src/lib/b.ts", rowsRead),
			WantReview: []string{"tests/purity-lost"}},
	}
}

func tsNoMockCases() []engine.Case {
	calc := "export const add = (a: number, b: number) => a + b;\n"
	fetches := "export const load = async () => (await fetch('/rows')).status;\n"
	test := func(src, mock string) fstest.MapFS {
		return tsTree("src/calc/calc.ts", src, "src/calc/calc.test.ts",
			"import { add } from './calc';\n\nit('adds', () => {\n"+mock+"  expect(add(1, 2)).toBe(3);\n});\n")
	}
	const spy = "  vi.spyOn(Math, 'max');\n"
	const stub = "  vi.stubGlobal('fetch', async () => ({ status: 3 }));\n"
	const each = "export const each = (xs: number[], f: (x: number) => void) => xs.forEach(f);\n" +
		"export const total = (src: { read(): number[] }) => src.read().reduce((a, b) => a + b, 0);\n"
	const callbacks = "import { each, total } from './each';\n\n" +
		"it('calls back', () => {\n  const f = vi.fn();\n  each([1], f);\n  expect(f).toHaveBeenCalledWith(1);\n});\n\n" +
		"it('sums', () => {\n  const read = jest.fn().mockImplementation(() => [1, 2]);\n  expect(total({ read })).toBe(3);\n});\n"
	drives := func(mock string) string {
		return "import request from 'supertest';\nimport { add } from './calc';\n\nit('serves the sum', async () => {\n" + mock +
			"  const res = await request('http://localhost').get('/sum');\n  expect(res.body.n).toBe(add(1, 2));\n});\n"
	}
	return append([]engine.Case{
		{Name: "vi.spyOn in a test of a pure TypeScript unit fails", Base: test(calc, ""), Head: test(calc, spy),
			WantFail: []string{"tests/no-mock"}},
		{Name: "jest.spyOn in a test of a pure TypeScript unit fails", Base: test(calc, ""), Head: test(calc, "  jest.spyOn(Math, 'max');\n"),
			WantFail: []string{"tests/no-mock"}},
		{Name: "vi.stubGlobal in a test of a pure TypeScript unit fails", Base: test(calc, ""), Head: test(calc, stub),
			WantFail: []string{"tests/no-mock"}},
		{Name: "vi.stubGlobal in a test of a unit that calls the global fetch passes",
			Base: test(calc+fetches, ""), Head: test(calc+fetches, stub)},
		{Name: "vi.fn and jest.fn passed to a pure TypeScript unit as a callback or a dependency pass",
			Base: tsTree("src/calc/each.ts", each), Head: tsTree("src/calc/each.ts", each, "src/calc/each.test.ts", callbacks)},
		{Name: "a mock in a test that drives the app passes, though the only target it calls is pure",
			Base: tsTree("src/calc/calc.ts", calc, "src/calc/calc.test.ts", drives("")),
			Head: tsTree("src/calc/calc.ts", calc, "src/calc/calc.test.ts", drives(spy))},
	}, fileMockCases()...)
}

// fileMockCases are mocks at the top of a test file, which the runner applies to every test of it.
func fileMockCases() []engine.Case {
	shared := func(test string) fstest.MapFS {
		return tsTree("src/calc/calc.ts", "export const add = (a: number, b: number) => a + b;\n",
			"src/api/load.ts", "export const load = async () => (await fetch('/rows')).status;\n",
			"src/api/load.test.ts", test)
	}
	const head = "import { add } from '../calc/calc';\nimport { load } from './load';\n\n"
	const mock = "vi.mock('fancy-lib', () => ({ preset: vi.fn() }));\n\n"
	const adds = "it('adds', () => {\n  expect(add(1, 2)).toBe(3);\n});\n"
	const loads = "it('loads', async () => {\n  expect(await load()).toBe(200);\n});\n"
	const spyAdds = "it('adds', () => {\n  vi.spyOn(Math, 'max');\n  expect(add(1, 2)).toBe(3);\n});\n"
	const served = "import request from 'supertest';\n"
	const serves = "it('serves', async () => {\n  expect((await request('http://localhost').get('/n')).status).toBe(200);\n});\n"
	return []engine.Case{
		{Name: "a file-level vi.mock in a file where another test drives the app passes",
			Base: shared(served + head + adds + serves), Head: shared(served + head + mock + adds + serves)},
		{Name: "a file-level vi.mock in a file whose tests call only pure units fails",
			Base: shared(head + adds), Head: shared(head + mock + adds), WantFail: []string{"tests/no-mock"}},
		{Name: "a file-level vi.mock in a file where another test calls a unit that is not pure passes",
			Base: shared(head + adds + loads), Head: shared(head + mock + adds + loads)},
		{Name: "a mock inside a test that calls only pure units fails, though another test of the file does not",
			Base: shared(head + adds + loads), Head: shared(head + spyAdds + loads), WantFail: []string{"tests/no-mock"}},
	}
}
