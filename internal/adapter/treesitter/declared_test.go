//go:build cgo

package treesitter

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"testing/fstest"
)

// guards renders the guards: values each test declares, one test per line.
func guards(t *testing.T, name string, tr fstest.MapFS) string {
	t.Helper()
	var out []string
	for _, ts := range analyze(t, name, tr).Tests {
		out = append(out, fmt.Sprintf("%s %v", ts.Name, ts.Declared["guards"]))
	}
	sort.Strings(out)
	return strings.Join(out, "\n")
}

// viewpoint: behaviour
// target: the readers of every tree-sitter language
// The "key: value" lines of the comment right before a test are its declarations, in every
// language: past attributes and decorators, in block comments and, in Python, in the docstring.
// A comment separated from the test by a blank line declares nothing.
func TestDeclared(t *testing.T) {
	t.Run("python", func(t *testing.T) {
		expect(t, guards(t, "python", tree(
			"src/app/__init__.py", "",
			"src/app/store.py", "def save(row):\n    return row\n",
			"tests/test_store.py", `import pytest
from app.store import save


# guards: durable app.store.save
def test_comment():
    save(1)


# guards: durable app.store.save
@pytest.mark.slow
def test_decorated():
    save(1)


def test_docstring():
    """Saved rows survive.

    guards: rows-kept
    """
    save(1)


# guards: durable app.store.save

def test_apart():
    save(1)
`)), `test_apart []
test_comment [durable app.store.save]
test_decorated [durable app.store.save]
test_docstring [rows-kept]`)
	})
	t.Run("typescript", func(t *testing.T) {
		expect(t, guards(t, "typescript", tree(
			"src/store.ts", "export function save(row: number) { return row; }\n",
			"src/store.test.ts", `import { save } from "./store";

// guards: durable store.save
it("comment", () => {
  save(1);
});

describe("suite", () => {
  /**
   * Saved rows survive.
   * guards: rows-kept
   */
  test("block", () => {
    save(1);
  });
});

// guards: durable store.save

it("apart", () => {
  save(1);
});
`)), `apart []
comment [durable store.save]
suite block [rows-kept]`)
	})
	t.Run("java", func(t *testing.T) {
		expect(t, guards(t, "java", tree(
			"src/main/java/app/Store.java", "package app;\n\npublic class Store {\n  public static int save(int r) { return r; }\n}\n",
			"src/test/java/app/StoreTest.java", `package app;

import org.junit.jupiter.api.Test;

class StoreTest {
  /**
   * Saved rows survive.
   * guards: durable app.Store.save
   */
  @Test
  void block() { Store.save(1); }

  // guards: rows-kept
  @Test
  void line() { Store.save(1); }

  @Test
  void none() { Store.save(1); }
}
`)), `StoreTest.block [durable app.Store.save]
StoreTest.line [rows-kept]
StoreTest.none []`)
	})
	t.Run("kotlin", func(t *testing.T) {
		expect(t, guards(t, "kotlin", tree(
			"src/main/kotlin/app/Store.kt", "package app\n\nfun save(r: Int) = r\n",
			"src/test/kotlin/app/StoreTest.kt", `package app

import org.junit.jupiter.api.Test

class StoreTest {
    /** guards: durable app.save */
    @Test
    fun block() { save(1) }

    // guards: rows-kept
    @Test
    fun line() { save(1) }

    @Test
    fun none() { save(1) }
}
`)), `StoreTest.block [durable app.save]
StoreTest.line [rows-kept]
StoreTest.none []`)
	})
	t.Run("rust", func(t *testing.T) {
		expect(t, guards(t, "rust", tree(
			"Cargo.toml", "[package]\nname = \"store\"\n",
			"src/lib.rs", `pub fn save(r: i32) -> i32 { r }

#[cfg(test)]
mod tests {
    use super::*;

    /// Saved rows survive.
    /// guards: durable store::save
    #[test]
    fn doc() { save(1); }

    // guards: rows-kept
    #[test]
    #[ignore = "slow"]
    fn plain() { save(1); }

    #[test]
    fn none() { save(1); }
}
`)), `tests::doc [durable store::save]
tests::none []
tests::plain [rows-kept]`)
	})
}
