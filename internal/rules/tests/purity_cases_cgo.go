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
	return []engine.Case{
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
	return []engine.Case{
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
	}
}
