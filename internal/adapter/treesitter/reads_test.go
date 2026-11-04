//go:build cgo

package treesitter

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"testing/fstest"
)

// reads renders each test's count of product values it reads, as sorted lines.
func reads(t *testing.T, name string, tr fstest.MapFS) string {
	t.Helper()
	var out []string
	for _, ts := range analyze(t, name, tr).Tests {
		out = append(out, fmt.Sprintf("%s reads=%d calls=%d", ts.Name, ts.Reads, len(ts.Calls)))
	}
	sort.Strings(out)
	return strings.Join(out, "\n")
}

// viewpoint: behaviour
// target: the readers of every tree-sitter language
// A product name used without being called counts as a read: a constant, a function passed on,
// a class compared against. A call is not a read, and a name that appears only in a type is
// neither, because types do not run.
func TestProductReads(t *testing.T) {
	t.Run("python", func(t *testing.T) {
		expect(t, reads(t, "python", tree(
			"src/app/__init__.py", "",
			"src/app/store.py", "MAX_ROWS = 10\n\n\nclass Store:\n    pass\n\n\ndef normalize(s):\n    return s\n",
			"tests/test_store.py", `from app.store import MAX_ROWS, Store, normalize
from app import store


def test_const():
    assert 3 <= MAX_ROWS


def test_module_const():
    assert 3 <= store.MAX_ROWS


def test_passed():
    assert list(map(normalize, ["a"])) == ["a"]


def test_isinstance():
    assert not isinstance(1, Store)


def test_annotation_only():
    s: Store = object()
    assert s


def test_call():
    assert normalize("a") == "a"
`)), `
test_annotation_only reads=0 calls=0
test_call reads=0 calls=1
test_const reads=1 calls=0
test_isinstance reads=1 calls=0
test_module_const reads=1 calls=0
test_passed reads=1 calls=0`)
	})
	t.Run("typescript", func(t *testing.T) {
		expect(t, reads(t, "typescript", tree(
			"src/store.ts", "export const MAX_ROWS = 10\nexport class Store {}\nexport function normalize(s: string) { return s }\n",
			"src/store.test.ts", `import { MAX_ROWS, Store, normalize } from './store'
import type { Store as S } from './store'

test('const', () => { expect(MAX_ROWS).toBe(10) })
test('passed', () => { expect(['a'].map(normalize)).toEqual(['a']) })
test('types only', () => { const s: Store = {} as S; expect(s).toBeTruthy() })
test('call', () => { expect(normalize('a')).toBe('a') })
`)), `
call reads=0 calls=1
const reads=1 calls=0
passed reads=1 calls=0
types only reads=0 calls=0`)
	})
	t.Run("java", func(t *testing.T) {
		expect(t, reads(t, "java", tree(
			"src/main/java/com/acme/Store.java", "package com.acme;\n\npublic class Store {\n  public static final int LIMIT = 10;\n  public static String normalize(String s) { return s; }\n}\n",
			"src/test/java/com/acme/StoreTest.java", `package com.acme;

import org.junit.jupiter.api.Test;

class StoreTest {
  @Test void limit() { assertEquals(10, Store.LIMIT); }
  @Test void reference() { assertEquals(1, Stream.of("a").map(Store::normalize).count()); }
  @Test void typeOnly() { Store s = null; assertNull(s); }
  @Test void call() { assertEquals("a", Store.normalize("a")); }
}
`)), `
StoreTest.call reads=0 calls=1
StoreTest.limit reads=1 calls=0
StoreTest.reference reads=1 calls=0
StoreTest.typeOnly reads=0 calls=0`)
	})
	t.Run("kotlin", func(t *testing.T) {
		expect(t, reads(t, "kotlin", tree(
			"src/main/kotlin/acme/Store.kt", "package acme\n\nval LIMIT = 10\n\nclass Store\n\nfun normalize(s: String) = s\n",
			"src/test/kotlin/acme/StoreTest.kt", `package acme

import org.junit.jupiter.api.Test

class StoreTest {
    @Test
    fun limit() { assertEquals(10, LIMIT) }

    @Test
    fun reference() { assertEquals(listOf("a"), listOf("a").map(::normalize)) }

    @Test
    fun typeOnly() { val s: Store? = null; assertNull(s) }

    @Test
    fun call() { assertEquals("a", normalize("a")) }
}
`)), `
StoreTest.call reads=0 calls=1
StoreTest.limit reads=1 calls=0
StoreTest.reference reads=1 calls=0
StoreTest.typeOnly reads=0 calls=0`)
	})
	t.Run("rust", func(t *testing.T) {
		expect(t, reads(t, "rust", tree(
			"Cargo.toml", "[package]\nname = \"app\"\n",
			"src/lib.rs", `pub const LIMIT: usize = 10;
pub struct Store;
pub fn normalize(s: &str) -> &str { s }

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn limit() { assert_eq!(LIMIT, 10); }

    #[test]
    fn passed() { let v: Vec<&str> = vec!["a"].into_iter().map(normalize).collect(); assert_eq!(v.len(), 1); }

    #[test]
    fn type_only() { let s: Option<Store> = None; assert!(s.is_none()); }

    #[test]
    fn call() { let s = normalize("a"); assert_eq!(s, "a"); }
}
`)), `
tests::call reads=0 calls=1
tests::limit reads=1 calls=0
tests::passed reads=1 calls=0
tests::type_only reads=0 calls=0`)
	})
}

// viewpoint: failure
// target: lang.Analyze
// Adapter invariant 2: a file that does not fully parse is not read silently in part. Every
// language marks it unread, and still reports what did parse (here its import).
func TestAFileThatDoesNotParseIsUnreadButKeepsWhatParsed(t *testing.T) {
	cases := []struct{ lang, path, src string }{
		{"typescript", "src/a.ts", "import { x } from \"./b\";\nexport function f( {\n"},
		{"python", "src/a.py", "import os\n\ndef f(:\n"},
		{"rust", "src/lib.rs", "use std::fs;\nfn f( {\n"},
		{"java", "src/main/java/a/A.java", "package a;\nimport java.io.File;\nclass A { void f( }\n"},
		{"kotlin", "src/main/kotlin/a/A.kt", "package a\nimport java.io.File\nfun f( {\n"},
	}
	for _, c := range cases {
		t.Run(c.lang, func(t *testing.T) {
			a := analyze(t, c.lang, tree(c.path, c.src))
			if len(a.Files) != 1 || !a.Files[0].Unread {
				t.Fatalf("the file is not unread: %+v", a.Files)
			}
			if len(a.Imports) == 0 {
				t.Errorf("the import that parsed was dropped")
			}
			ok := analyze(t, c.lang, tree(c.path, strings.SplitN(c.src, "\n", 2)[0]+"\n"))
			if ok.Files[0].Unread {
				t.Errorf("a file that parses is unread")
			}
		})
	}
}
