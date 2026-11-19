//go:build cgo

package treesitter

import (
	"strings"
	"testing"

	"github.com/rerorets/hadome/internal/config"
)

// fuzzFiles are, per language, a product file and a test file that the fuzzed source is read as.
var fuzzFiles = []struct{ lang, product, test string }{
	{"typescript", "src/a.ts", "src/a.test.ts"},
	{"python", "app/a.py", "tests/test_a.py"},
	{"java", "src/main/java/a/A.java", "src/test/java/a/ATest.java"},
	{"kotlin", "src/main/kotlin/a/A.kt", "src/test/kotlin/a/ATest.kt"},
	{"rust", "src/lib.rs", "tests/a.rs"},
}

// viewpoint: input
// target: the tree-sitter readers
// Any bytes, read as a product file and as a test file of any of the five languages, are analyzed
// without failing, and every line the facts name lies within the file. The grammars are C, so the
// CI job runs this with AddressSanitizer as well.
func FuzzTreeSitterReaders(f *testing.F) {
	seeds := []string{
		"",
		"export function add(a: number, b: number) { return a + b }\n",
		"import { add } from './a'\ntest('adds', () => { expect(add(1, 2)).toBe(3) })\n",
		"def add(a, b):\n    return a + b\n\ndef test_add():\n    assert add(1, 2) == 3\n",
		"package a;\nclass A { int add(int x, int y) { return x + y; } }\n",
		"package a\nclass ATest { @Test fun adds() { assertEquals(3, A().add(1, 2)) } }\n",
		"pub fn add(a: i32, b: i32) -> i32 { a + b }\n#[test]\nfn adds() { assert_eq!(add(1, 2), 3); }\n",
		"class {\n\x00\xff /* unterminated",
	}
	for i, s := range seeds {
		f.Add(uint8(i), s)
	}
	f.Fuzz(func(t *testing.T, which uint8, src string) {
		ff := fuzzFiles[int(which)%len(fuzzFiles)]
		a := analyzeWith(t, ff.lang, tree(ff.product, src, ff.test, src), config.Analysis{})
		// A position's row is at most the number of newlines before it.
		last := strings.Count(src, "\n") + 1
		within := func(what, path string, line, end int) {
			if line < 1 || line > last || end > last || (end != 0 && end < line) {
				t.Errorf("%s in %s at lines %d-%d, outside the %d lines of the file", what, path, line, end, last)
			}
		}
		for _, c := range a.Comments {
			within("comment", c.Path, c.Line, c.EndLine)
		}
		for _, s := range a.Symbols {
			within("symbol "+s.ID, s.Path, s.Line, s.EndLine)
		}
		for _, i := range a.Imports {
			within("import", i.Path, i.Line, 0)
		}
		for _, x := range a.Tests {
			within("test "+x.Name, x.Path, x.Line, 0)
		}
	})
}
