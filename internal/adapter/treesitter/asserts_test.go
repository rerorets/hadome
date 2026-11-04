//go:build cgo

package treesitter

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/config"
)

// asserts renders each test's count of checks, and whether it handed its tool to unread code.
func asserts(t *testing.T, name string, tr fstest.MapFS) string {
	t.Helper()
	c := config.Config{Assertions: []string{"must_equal", "mustEqual", "MustEqual.that", "must_equal!"}}
	var out []string
	for _, ts := range analyzeWith(t, name, tr, c).Tests {
		out = append(out, fmt.Sprintf("%s asserts=%d unknown=%v", ts.Name, ts.Asserts, ts.AssertsUnknown))
	}
	sort.Strings(out)
	return strings.Join(out, "\n")
}

// viewpoint: behaviour
// target: the readers of every tree-sitter language
// The checks of each language's test libraries count as asserts, in the test and in a helper of
// the file, and so do configured functions. A test without one counts none. In Python, handing
// self to unread code makes the count unknown.
func TestAsserts(t *testing.T) {
	t.Run("python", func(t *testing.T) {
		expect(t, asserts(t, "python", tree(
			"src/app/__init__.py", "",
			"src/app/calc.py", "def add(a, b):\n    return a + b\n",
			"tests/test_calc.py", `import unittest
import pytest
from app.calc import add
from tests.checks import check_sum


def same(a, b):
    assert a == b


def test_none():
    add(1, 2)


def test_assert():
    assert add(1, 2) == 3


def test_helper():
    same(add(1, 2), 3)


def test_raises():
    with pytest.raises(TypeError):
        add(1)


def test_configured():
    must_equal(add(1, 2), 3)


class TestCalc(unittest.TestCase):
    def test_method(self):
        self.assertEqual(add(1, 2), 3)

    def test_own_method(self):
        self.check(add(1, 2))

    def test_self_passed(self):
        check_sum(self, add(1, 2))

    def check(self, n):
        self.assertEqual(n, 3)
`)), `
TestCalc.test_method asserts=1 unknown=false
TestCalc.test_own_method asserts=0 unknown=true
TestCalc.test_self_passed asserts=0 unknown=true
test_assert asserts=1 unknown=false
test_configured asserts=1 unknown=false
test_helper asserts=1 unknown=false
test_none asserts=0 unknown=false
test_raises asserts=1 unknown=false`)
	})
	t.Run("typescript", func(t *testing.T) {
		expect(t, asserts(t, "typescript", tree(
			"src/calc.ts", "export function add(a: number, b: number) { return a + b; }\n",
			"src/calc.test.ts", `import { add } from "./calc";
import assert from "node:assert";

function same(a: number, b: number) {
  assert.equal(a, b);
}

it("none", () => {
  add(1, 2);
});

it("expect", () => {
  expect(add(1, 2)).toBe(3);
});

it("helper", () => {
  same(add(1, 2), 3);
});

it("configured", () => {
  mustEqual(add(1, 2), 3);
});
`)), `
configured asserts=1 unknown=false
expect asserts=1 unknown=false
helper asserts=1 unknown=false
none asserts=0 unknown=false`)
	})
	t.Run("java", func(t *testing.T) {
		expect(t, asserts(t, "java", tree(
			"src/main/java/app/Calc.java", "package app;\n\npublic class Calc {\n  public static int add(int a, int b) { return a + b; }\n}\n",
			"src/test/java/app/CalcTest.java", `package app;

import org.junit.jupiter.api.Test;
import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.assertj.core.api.Assertions.assertThat;
import static org.junit.jupiter.api.Assertions.fail;

class CalcTest {
  @Test
  void none() { Calc.add(1, 2); }

  @Test
  void junit() { assertEquals(3, Calc.add(1, 2)); }

  @Test
  void assertj() { assertThat(Calc.add(1, 2)).isEqualTo(3); }

  @Test
  void failing() {
    if (Calc.add(1, 2) != 3) {
      fail("want 3");
    }
  }

  @Test
  void helper() { same(Calc.add(1, 2), 3); }

  @Test
  void configured() { MustEqual.that(Calc.add(1, 2), 3); }

  private void same(int a, int b) { assertEquals(a, b); }
}
`)), `
CalcTest.assertj asserts=1 unknown=false
CalcTest.configured asserts=1 unknown=false
CalcTest.failing asserts=1 unknown=false
CalcTest.helper asserts=1 unknown=false
CalcTest.junit asserts=1 unknown=false
CalcTest.none asserts=0 unknown=false`)
	})
	t.Run("kotlin", func(t *testing.T) {
		expect(t, asserts(t, "kotlin", tree(
			"src/main/kotlin/app/Calc.kt", "package app\n\nfun add(a: Int, b: Int) = a + b\n",
			"src/test/kotlin/app/CalcTest.kt", `package app

import org.junit.jupiter.api.Test
import io.kotest.matchers.shouldBe
import kotlin.test.assertEquals

class CalcTest {
    @Test
    fun none() { add(1, 2) }

    @Test
    fun junit() { assertEquals(3, add(1, 2)) }

    @Test
    fun infix() { add(1, 2) shouldBe 3 }

    @Test
    fun dotted() { add(1, 2).shouldBe(3) }

    @Test
    fun helper() { same(add(1, 2), 3) }

    private fun same(a: Int, b: Int) { assertEquals(a, b) }
}
`)), `
CalcTest.dotted asserts=1 unknown=false
CalcTest.helper asserts=1 unknown=false
CalcTest.infix asserts=1 unknown=false
CalcTest.junit asserts=1 unknown=false
CalcTest.none asserts=0 unknown=false`)
	})
	t.Run("rust", func(t *testing.T) {
		expect(t, asserts(t, "rust", tree(
			"Cargo.toml", "[package]\nname = \"calc\"\n",
			"src/lib.rs", `pub fn add(a: i32, b: i32) -> i32 { a + b }

#[cfg(test)]
mod tests {
    use super::*;

    fn same(a: i32, b: i32) {
        assert_eq!(a, b);
    }

    #[test]
    fn none() { add(1, 2); }

    #[test]
    fn macro_eq() { assert_eq!(add(1, 2), 3); }

    #[test]
    fn helper() { same(add(1, 2), 3); }

    #[test]
    #[should_panic]
    fn panics() { add(i32::MAX, 1); }

    #[test]
    fn configured() { must_equal!(add(1, 2), 3); }

    #[test]
    fn explicit_panic() {
        if add(1, 2) != 3 {
            panic!("want 3");
        }
    }
}
`)), `
tests::configured asserts=1 unknown=false
tests::explicit_panic asserts=1 unknown=false
tests::helper asserts=1 unknown=false
tests::macro_eq asserts=1 unknown=false
tests::none asserts=0 unknown=false
tests::panics asserts=1 unknown=false`)
	})
}
