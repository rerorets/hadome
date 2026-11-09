//go:build cgo

package tests

import (
	"testing/fstest"

	"github.com/rerorets/hadome/internal/engine"
)

func init() {
	languageDuplicateCases = append(tableCases(), nameOnlyCases()...)
}

// nameOnlyCases are parameterised Python tests whose ids only name their runs.
func nameOnlyCases() []engine.Case {
	tree := func(tests string) fstest.MapFS {
		return fstest.MapFS{
			"src/app/__init__.py": {Data: []byte("")},
			"src/app/calc.py":     {Data: []byte("def add(a, b):\n    return a + b\n")},
			"tests/test_calc.py":  {Data: []byte("import pytest\nfrom app.calc import add\n" + tests)},
		}
	}
	test := func(name, decorators string) string {
		return "\n\n" + decorators + "def " + name + "(a):\n    assert add(a, 0) == a\n"
	}
	base := test("test_small", "@pytest.mark.parametrize(\"a\", [1], ids=[\"small\"])\n")
	return []engine.Case{
		{Name: "a copy that differs only in the ids of its runs fails", WantFail: []string{"tests/duplicate"},
			Base: tree(base),
			Head: tree(base + test("test_tiny", "@pytest.mark.parametrize(\"a\", [1], ids=[\"tiny\"])\n"))},
		{Name: "a copy with a time limit passes",
			Base: tree(base),
			Head: tree(base + test("test_small_in_time", "@pytest.mark.parametrize(\"a\", [1], ids=[\"small\"])\n@pytest.mark.timeout(5)\n"))},
	}
}

// tableCases are tests in describe.each blocks, whose tables are the tests' inputs.
func tableCases() []engine.Case {
	const add = "export const add = (a: number, b: number) => a + b;\n"
	suite := func(table string) string {
		return "\ndescribe.each(" + table + ")('adds %i', (a, b) => {\n  it('sums', () => { expect(add(a, b)).toBe(a + b) })\n})\n"
	}
	base := "import { add } from './add'\n" + suite("[[1, 2]]")
	return []engine.Case{
		{Name: "a describe.each block with the same test and another table passes",
			Base: tsTree("src/add.ts", add, "src/add.test.ts", base),
			Head: tsTree("src/add.ts", add, "src/add.test.ts", base+suite("[[5, 6]]"))},
		{Name: "a describe.each block with the same test and the same table fails", WantFail: []string{"tests/duplicate"},
			Base: tsTree("src/add.ts", add, "src/add.test.ts", base),
			Head: tsTree("src/add.ts", add, "src/add.test.ts", base+suite("[[1, 2]]"))},
	}
}
