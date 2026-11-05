//go:build cgo

package treesitter

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// alwaysTrue renders which tests hold an always-true hazard.
func alwaysTrue(t *testing.T, name string, files ...string) string {
	t.Helper()
	var out []string
	for _, ts := range analyze(t, name, tree(files...)).Tests {
		hit := false
		for _, h := range ts.Hazards {
			hit = hit || h.Category == "always-true"
		}
		out = append(out, fmt.Sprintf("%s always-true=%v", ts.Name, hit))
	}
	sort.Strings(out)
	return strings.Join(out, "\n")
}

// viewpoint: behaviour
// target: the readers of every tree-sitter language
// A value compared with itself cannot fail, but the results of two calls spelled the same can
// differ: comparing them checks that the same input gives the same result.
func TestComparingTwoCallsIsNotAlwaysTrue(t *testing.T) {
	t.Run("typescript", func(t *testing.T) {
		expect(t, alwaysTrue(t, "typescript",
			"src/key.ts", "export function key(s: string): string { return s }\n",
			"src/key.test.ts", `import { key } from './key'

it('self', () => { const k = key('a'); expect(k).toBe(k) })
it('twice', () => { expect(key('a')).toBe(key('a')) })
`), "self always-true=true\ntwice always-true=false")
	})
	t.Run("python", func(t *testing.T) {
		expect(t, alwaysTrue(t, "python",
			"src/app/__init__.py", "",
			"src/app/key.py", "def key(s):\n    return s\n",
			"tests/test_key.py", `from app.key import key


def test_self():
    k = key("a")
    assert k == k


def test_twice():
    assert key("a") == key("a")
`), "test_self always-true=true\ntest_twice always-true=false")
	})
	t.Run("kotlin", func(t *testing.T) {
		expect(t, alwaysTrue(t, "kotlin",
			"src/main/kotlin/acme/Key.kt", "package acme\n\nfun key(s: String): String = s\n",
			"src/test/kotlin/acme/KeyTest.kt", `package acme

import kotlin.test.Test
import kotlin.test.assertEquals

class KeyTest {
    @Test
    fun self() { val k = key("a"); assertEquals(k, k) }

    @Test
    fun twice() { assertEquals(key("a"), key("a")) }
}
`), "KeyTest.self always-true=true\nKeyTest.twice always-true=false")
	})
	t.Run("rust", func(t *testing.T) {
		expect(t, alwaysTrue(t, "rust",
			"Cargo.toml", "[package]\nname = \"acme\"\n",
			"src/lib.rs", `pub fn key(s: &str) -> String { s.to_string() }

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn same() { let k = key("a"); assert_eq!(k, k); }

    #[test]
    fn twice() { assert_eq!(key("a"), key("a")); }

    #[test]
    fn differs() { let k = key("a"); assert_ne!(k, k); }

    #[test]
    fn differs_twice() { assert_ne!(key("a"), key("b")); }
}
`), "tests::differs always-true=true\ntests::differs_twice always-true=false\ntests::same always-true=true\ntests::twice always-true=false")
	})
}
