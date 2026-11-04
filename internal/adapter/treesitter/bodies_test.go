//go:build cgo

package treesitter

import (
	"sort"
	"strings"
	"testing"
	"testing/fstest"
)

// bodies renders the tests grouped by Body, one sorted line per group.
func bodies(t *testing.T, name string, tr fstest.MapFS) string {
	t.Helper()
	groups := map[string][]string{}
	for _, ts := range analyze(t, name, tr).Tests {
		if ts.Body == "" {
			t.Errorf("%s has no body fingerprint", ts.Name)
		}
		groups[ts.Body] = append(groups[ts.Body], ts.Name)
	}
	var lines []string
	for _, names := range groups {
		sort.Strings(names)
		lines = append(lines, strings.Join(names, " "))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// viewpoint: behaviour
// target: the readers of every tree-sitter language
// Tests that differ only in names, comments and whitespace share a fingerprint. A different
// literal, in the body or in the inputs of a parameterised test, makes a different one.
func TestBodies(t *testing.T) {
	t.Run("python", func(t *testing.T) {
		expect(t, bodies(t, "python", tree(
			"src/app/__init__.py", "",
			"src/app/calc.py", "def add(a, b):\n    return a + b\n",
			"tests/test_calc.py", `import pytest
from app.calc import add


def test_add():
    assert add(1, 2) == 3


def test_add_again():
    # the same check
    assert add(1,2)  ==  3


def test_add_other():
    assert add(1, 3) == 4


@pytest.mark.parametrize("a", [1])
def test_one(a):
    assert add(a, 0) == a


@pytest.mark.parametrize("a", [2])
def test_two(a):
    assert add(a, 0) == a
`)), `
test_add test_add_again
test_add_other
test_one
test_two`)
	})
	t.Run("typescript", func(t *testing.T) {
		expect(t, bodies(t, "typescript", tree(
			"src/calc.ts", "export function add(a: number, b: number) { return a + b; }\n",
			"src/calc.test.ts", `import { add } from "./calc";

it("adds", () => {
  expect(add(1, 2)).toBe(3);
});

// a copy
it("adds again", () => { expect(add(1,2)).toBe(3); });

it("adds others", () => {
  expect(add(1, 3)).toBe(4);
});
`)), `
adds adds again
adds others`)
	})
	t.Run("java", func(t *testing.T) {
		expect(t, bodies(t, "java", tree(
			"src/main/java/app/Calc.java", "package app;\n\npublic class Calc {\n  public static int add(int a, int b) { return a + b; }\n}\n",
			"src/test/java/app/CalcTest.java", `package app;

import org.junit.jupiter.api.Test;
import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.ValueSource;
import static org.junit.jupiter.api.Assertions.assertEquals;

class CalcTest {
  @Test
  void adds() {
    assertEquals(3, Calc.add(1, 2));
  }

  /** A copy. */
  @Test
  void addsAgain() { assertEquals(3, Calc.add(1,2)); }

  @Test
  void addsOther() {
    assertEquals(4, Calc.add(1, 3));
  }

  @ParameterizedTest
  @ValueSource(ints = {1})
  void one(int a) { assertEquals(a, Calc.add(a, 0)); }

  @ParameterizedTest
  @ValueSource(ints = {2})
  void two(int a) { assertEquals(a, Calc.add(a, 0)); }
}
`)), `
CalcTest.adds CalcTest.addsAgain
CalcTest.addsOther
CalcTest.one
CalcTest.two`)
	})
	t.Run("kotlin", func(t *testing.T) {
		expect(t, bodies(t, "kotlin", tree(
			"src/main/kotlin/app/Calc.kt", "package app\n\nfun add(a: Int, b: Int) = a + b\n",
			"src/test/kotlin/app/CalcTest.kt", `package app

import org.junit.jupiter.api.Test
import org.junit.jupiter.params.ParameterizedTest
import org.junit.jupiter.params.provider.ValueSource
import kotlin.test.assertEquals

class CalcTest {
    @Test
    fun adds() {
        assertEquals(3, add(1, 2))
    }

    // a copy
    @Test
    fun `+"`adds again`"+`() { assertEquals(3, add(1,2)) }

    @Test
    fun addsOther() {
        assertEquals(4, add(1, 3))
    }

    @ParameterizedTest
    @ValueSource(ints = [1])
    fun one(a: Int) { assertEquals(a, add(a, 0)) }

    @ParameterizedTest
    @ValueSource(ints = [2])
    fun two(a: Int) { assertEquals(a, add(a, 0)) }
}
`)), `
CalcTest.`+"`adds again`"+` CalcTest.adds
CalcTest.addsOther
CalcTest.one
CalcTest.two`)
	})
	t.Run("rust", func(t *testing.T) {
		expect(t, bodies(t, "rust", tree(
			"Cargo.toml", "[package]\nname = \"calc\"\n",
			"src/lib.rs", `pub fn add(a: i32, b: i32) -> i32 { a + b }

#[cfg(test)]
mod tests {
    use super::*;
    use rstest::rstest;

    #[test]
    fn adds() {
        assert_eq!(add(1, 2), 3);
    }

    /// A copy.
    #[test]
    fn adds_again() { assert_eq!(add(1,2), 3); }

    #[test]
    fn adds_other() {
        assert_eq!(add(1, 3), 4);
    }

    #[rstest]
    #[case(1)]
    fn one(#[case] a: i32) { assert_eq!(add(a, 0), a); }

    #[rstest]
    #[case(2)]
    fn two(#[case] a: i32) { assert_eq!(add(a, 0), a); }
}
`)), `
tests::adds tests::adds_again
tests::adds_other
tests::one
tests::two`)
	})
}
