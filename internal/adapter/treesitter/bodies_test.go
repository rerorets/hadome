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

// viewpoint: behaviour
// target: the Python and TypeScript readers
// The setup that runs for a test is part of its fingerprint: tests with the same body under a
// different setUp or autouse fixture of their class, a different beforeEach of their describe, or
// a different declaration of an outer variable they use, check different things. Under the same
// setup they share a fingerprint.
func TestBodiesIncludeTheSetup(t *testing.T) {
	t.Run("python", func(t *testing.T) {
		expect(t, bodies(t, "python", tree(
			"src/app/__init__.py", "",
			"src/app/store.py", "class Store:\n    def add(self, row):\n        return 1\n\n\nclass Ledger:\n    def add(self, row):\n        return 1\n",
			"tests/test_store.py", `import unittest

import pytest

from app.store import Ledger, Store


class TestA(unittest.TestCase):
    def setUp(self):
        self.store = Store()

    def test_add(self):
        self.assertEqual(self.store.add("x"), 1)


class TestB(unittest.TestCase):
    def setUp(self):
        self.store = Ledger()

    def test_add(self):
        self.assertEqual(self.store.add("x"), 1)


class TestC(unittest.TestCase):
    def setUp(self):
        self.store = Store()

    def test_add(self):
        self.assertEqual(self.store.add("x"), 1)


class TestD:
    @pytest.fixture(autouse=True)
    def _store(self):
        self.store = Ledger()
        self.store.add("seed")

    def test_add(self):
        self.assertEqual(self.store.add("x"), 1)
`)), `TestA.test_add TestC.test_add
TestB.test_add
TestD.test_add`)
	})
	t.Run("typescript", func(t *testing.T) {
		expect(t, bodies(t, "typescript", tree(
			"src/words.ts", "export const en = { hi: 'hi' };\nexport const fr = { hi: 'salut' };\nexport function say(k: string) { return k; }\n",
			"src/words.test.ts", `import { en, fr, say } from "./words";

describe("en", () => {
  const KEYS = Object.keys(en);
  it("says every key", () => {
    for (const k of KEYS) expect(say(k)).toBe(k);
  });
});

describe("fr", () => {
  const KEYS = Object.keys(fr);
  it("says every key", () => {
    for (const k of KEYS) expect(say(k)).toBe(k);
  });
});

describe("en again", () => {
  const KEYS = Object.keys(en);
  it("says every key", () => {
    for (const k of KEYS) expect(say(k)).toBe(k);
  });
});

describe("hooked", () => {
  beforeEach(() => say("start"));
  it("says hi", () => {
    expect(say("hi")).toBe("hi");
  });
});

describe("plain", () => {
  it("says hi", () => {
    expect(say("hi")).toBe("hi");
  });
  it("says hi twice", () => {
    expect(say("hi")).toBe("hi");
  });
});
`)), `en again says every key en says every key
fr says every key
hooked says hi
plain says hi plain says hi twice`)
	})
}

// viewpoint: behaviour
// target: the Python, TypeScript, Java and Kotlin readers
// A parameterised suite (describe.each, a class under @pytest.mark.parametrize or
// @ParameterizedClass) writes the inputs of every test in it outside the test. Two such suites with
// the same test and other inputs check other values, so their tests have different fingerprints.
// With the same inputs they are copies and share one.
func TestParameterisedSuitesGiveTheirInputs(t *testing.T) {
	t.Run("typescript", func(t *testing.T) {
		expect(t, bodies(t, "typescript", tree(
			"src/calc.ts", "export function add(a: number, b: number) { return a + b; }\n",
			"src/calc.test.ts", `import { add } from "./calc";

describe.each([[1, 2]])("small %i", (a, b) => {
  it("adds", () => { expect(add(a, b)).toBe(a + b); });
});

describe.each([[5, 6]])("large %i", (a, b) => {
  it("adds", () => { expect(add(a, b)).toBe(a + b); });
});

describe.each([[5, 6]])("large again %i", (a, b) => {
  it("adds", () => { expect(add(a, b)).toBe(a + b); });
});
`)), `
large %i adds large again %i adds
small %i adds`)
	})
	t.Run("python", func(t *testing.T) {
		expect(t, bodies(t, "python", tree(
			"src/app/__init__.py", "",
			"src/app/calc.py", "def add(a, b):\n    return a + b\n",
			"tests/test_calc.py", `import pytest
from app.calc import add


@pytest.mark.parametrize("a", [1])
class TestSmall:
    def test_add(self, a):
        assert add(a, 0) == a


@pytest.mark.parametrize("a", [9])
class TestLarge:
    def test_add(self, a):
        assert add(a, 0) == a


@pytest.mark.parametrize("a", [9])
class TestLargeAgain:
    def test_add(self, a):
        assert add(a, 0) == a
`)), `
TestLarge.test_add TestLargeAgain.test_add
TestSmall.test_add`)
	})
	t.Run("java", func(t *testing.T) {
		expect(t, bodies(t, "java", tree(
			"src/main/java/app/Calc.java", "package app;\n\npublic class Calc {\n  public static int add(int a, int b) { return a + b; }\n}\n",
			"src/test/java/app/CalcTest.java", `package app;

import org.junit.jupiter.api.Nested;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.params.Parameter;
import org.junit.jupiter.params.ParameterizedClass;
import org.junit.jupiter.params.provider.ValueSource;
import static org.junit.jupiter.api.Assertions.assertEquals;

class CalcTest {
  @Nested
  @ParameterizedClass
  @ValueSource(ints = {1})
  class Small {
    @Parameter int a;

    @Test
    void adds() { assertEquals(a, Calc.add(a, 0)); }
  }

  @Nested
  @ParameterizedClass
  @ValueSource(ints = {9})
  class Large {
    @Parameter int a;

    @Test
    void adds() { assertEquals(a, Calc.add(a, 0)); }
  }

  @Nested
  @ParameterizedClass
  @ValueSource(ints = {9})
  class LargeAgain {
    @Parameter int a;

    @Test
    void adds() { assertEquals(a, Calc.add(a, 0)); }
  }
}
`)), `
CalcTest.Large.adds CalcTest.LargeAgain.adds
CalcTest.Small.adds`)
	})
	t.Run("kotlin", func(t *testing.T) {
		expect(t, bodies(t, "kotlin", tree(
			"src/main/kotlin/app/Calc.kt", "package app\n\nfun add(a: Int, b: Int) = a + b\n",
			"src/test/kotlin/app/CalcTest.kt", `package app

import org.junit.jupiter.api.Nested
import org.junit.jupiter.api.Test
import org.junit.jupiter.params.ParameterizedClass
import org.junit.jupiter.params.provider.ValueSource
import kotlin.test.assertEquals

class CalcTest {
    @Nested
    @ParameterizedClass
    @ValueSource(ints = [1])
    inner class Small(val a: Int) {
        @Test
        fun adds() { assertEquals(a, add(a, 0)) }
    }

    @Nested
    @ParameterizedClass
    @ValueSource(ints = [9])
    inner class Large(val a: Int) {
        @Test
        fun adds() { assertEquals(a, add(a, 0)) }
    }

    @Nested
    @ParameterizedClass
    @ValueSource(ints = [9])
    inner class LargeAgain(val a: Int) {
        @Test
        fun adds() { assertEquals(a, add(a, 0)) }
    }
}
`)), `
CalcTest.Large.adds CalcTest.LargeAgain.adds
CalcTest.Small.adds`)
	})
}

// viewpoint: behaviour
// target: the Python, Java, Kotlin and Rust readers
// A test's name is not part of its fingerprint, wherever it is written: @DisplayName and the name
// element of @ParameterizedTest, @RepeatedTest and @ParameterizedClass, pytest's ids= and
// pytest.param's id= and rstest's case description. Two tests that differ only there share a
// fingerprint. An annotation that changes how the test runs (a timeout, a tag, another input) still
// makes a different one.
func TestBodiesLeaveOutNameOnlyAnnotations(t *testing.T) {
	t.Run("python", func(t *testing.T) {
		expect(t, bodies(t, "python", tree(
			"src/app/__init__.py", "",
			"src/app/calc.py", "def add(a, b):\n    return a + b\n",
			"tests/test_calc.py", `import pytest
from app.calc import add


@pytest.mark.parametrize("a", [1])
def test_plain(a):
    assert add(a, 0) == a


@pytest.mark.parametrize("a", [1], ids=["one"])
def test_ids(a):
    assert add(a, 0) == a


@pytest.mark.parametrize("a", [pytest.param(1)])
def test_param(a):
    assert add(a, 0) == a


@pytest.mark.parametrize("a", [pytest.param(1, id="first")])
def test_param_id(a):
    assert add(a, 0) == a


@pytest.mark.parametrize("a", [1], ids=["one"])
@pytest.mark.timeout(5)
def test_timeout(a):
    assert add(a, 0) == a


@pytest.mark.parametrize("a", [2], ids=["one"])
def test_other(a):
    assert add(a, 0) == a


def test_body_x():
    assert add(pytest.param(1, id="x").values[0], 0) == 1


def test_body_y():
    assert add(pytest.param(1, id="y").values[0], 0) == 1
`)), `
test_body_x
test_body_y
test_ids test_plain
test_other
test_param test_param_id
test_timeout`)
	})
	t.Run("java", func(t *testing.T) {
		expect(t, bodies(t, "java", tree(
			"src/main/java/app/Calc.java", "package app;\n\npublic class Calc {\n  public static int add(int a, int b) { return a + b; }\n}\n",
			"src/test/java/app/CalcTest.java", `package app;

import org.junit.jupiter.api.*;
import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.ValueSource;
import static org.junit.jupiter.api.Assertions.assertEquals;

class CalcTest {
  @Test
  void adds() { assertEquals(3, Calc.add(1, 2)); }

  @Test
  @DisplayName("adds two numbers")
  void addsNamed() { assertEquals(3, Calc.add(1, 2)); }

  @Test
  @org.junit.jupiter.api.DisplayName("adds both")
  void addsQualified() { assertEquals(3, Calc.add(1, 2)); }

  @Test
  @Timeout(5)
  void addsInTime() { assertEquals(3, Calc.add(1, 2)); }

  @Test
  @Tag("slow")
  void addsTagged() { assertEquals(3, Calc.add(1, 2)); }

  @ParameterizedTest
  @ValueSource(ints = {1})
  void one(int a) { assertEquals(a, Calc.add(a, 0)); }

  @ParameterizedTest(name = "with {0}")
  @ValueSource(ints = {1})
  void oneNamed(int a) { assertEquals(a, Calc.add(a, 0)); }

  @RepeatedTest(value = 3)
  void repeated() { assertEquals(3, Calc.add(1, 2)); }

  @RepeatedTest(name = "run {currentRepetition}", value = 3)
  void repeatedNamed() { assertEquals(3, Calc.add(1, 2)); }

  @RepeatedTest(value = 4, name = "run {currentRepetition}")
  void repeatedMore() { assertEquals(3, Calc.add(1, 2)); }
}
`)), `
CalcTest.adds CalcTest.addsNamed CalcTest.addsQualified
CalcTest.addsInTime
CalcTest.addsTagged
CalcTest.one CalcTest.oneNamed
CalcTest.repeated CalcTest.repeatedNamed
CalcTest.repeatedMore`)
	})
	t.Run("kotlin", func(t *testing.T) {
		expect(t, bodies(t, "kotlin", tree(
			"src/main/kotlin/app/Calc.kt", "package app\n\nfun add(a: Int, b: Int) = a + b\n",
			"src/test/kotlin/app/CalcTest.kt", `package app

import org.junit.jupiter.api.*
import org.junit.jupiter.params.ParameterizedTest
import org.junit.jupiter.params.ParameterizedClass
import org.junit.jupiter.params.provider.ValueSource
import kotlin.test.assertEquals

class CalcTest {
    @Test
    fun adds() { assertEquals(3, add(1, 2)) }

    @Test
    @DisplayName("adds two numbers")
    fun addsNamed() { assertEquals(3, add(1, 2)) }

    @Test
    @Timeout(5)
    fun addsInTime() { assertEquals(3, add(1, 2)) }

    @ParameterizedTest
    @ValueSource(ints = [1])
    fun one(a: Int) { assertEquals(a, add(a, 0)) }

    @ParameterizedTest(name = "with {0}")
    @ValueSource(ints = [1])
    fun oneNamed(a: Int) { assertEquals(a, add(a, 0)) }

    @RepeatedTest(name)
    fun repeatedByConstant() { assertEquals(3, add(1, 2)) }

    @RepeatedTest
    fun repeatedBare() { assertEquals(3, add(1, 2)) }

    @Nested
    @ParameterizedClass
    @ValueSource(ints = [5])
    inner class Plain(val a: Int) {
        @Test
        fun adds() { assertEquals(a, add(a, 0)) }
    }

    @Nested
    @ParameterizedClass(name = "with {0}")
    @ValueSource(ints = [5])
    inner class Named(val a: Int) {
        @Test
        fun adds() { assertEquals(a, add(a, 0)) }
    }
}
`)), `
CalcTest.Named.adds CalcTest.Plain.adds
CalcTest.adds CalcTest.addsNamed
CalcTest.addsInTime
CalcTest.one CalcTest.oneNamed
CalcTest.repeatedBare
CalcTest.repeatedByConstant`)
	})
	t.Run("rust", func(t *testing.T) {
		expect(t, bodies(t, "rust", tree(
			"Cargo.toml", "[package]\nname = \"calc\"\n",
			"src/lib.rs", `pub fn add(a: i32, b: i32) -> i32 { a + b }

#[cfg(test)]
mod tests {
    use super::*;
    use rstest::rstest;

    #[rstest]
    #[case(1)]
    fn one(#[case] a: i32) { assert_eq!(add(a, 0), a); }

    #[rstest]
    #[case::small(1)]
    fn one_named(#[case] a: i32) { assert_eq!(add(a, 0), a); }

    #[rstest]
    #[case::small(2)]
    fn two_named(#[case] a: i32) { assert_eq!(add(a, 0), a); }

    #[test_case(1)]
    fn case_one(a: i32) { assert_eq!(add(a, 0), a); }

    #[test_case(1 ; "small")]
    fn case_one_named(a: i32) { assert_eq!(add(a, 0), a); }

    #[test_case(1 => 1 ; "small")]
    fn case_one_result(a: i32) -> i32 { add(a, 0) }

    #[test_case(1 => 1)]
    fn case_one_result_bare(a: i32) -> i32 { add(a, 0) }

    #[test_case(2 ; "small")]
    fn case_two_named(a: i32) { assert_eq!(add(a, 0), a); }
}
`)), `
tests::case_one tests::case_one_named
tests::case_one_result tests::case_one_result_bare
tests::case_two_named
tests::one tests::one_named
tests::two_named`)
	})
}
