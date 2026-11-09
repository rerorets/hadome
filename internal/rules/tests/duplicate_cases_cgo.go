//go:build cgo

package tests

import (
	"github.com/rerorets/hadome/internal/engine"
)

func init() {
	languageDuplicateCases = tableCases()
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
