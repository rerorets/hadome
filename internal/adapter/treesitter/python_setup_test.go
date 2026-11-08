//go:build cgo

package treesitter

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// viewpoint: behaviour
// target: the Python reader
// A test reaches the code through what its file sets up: the object a pytest fixture returns or
// yields, what setUp or setup_method stores on self, and a helper held by a name as a lambda. A
// parameter named like a plain or otherwise decorated function is not a fixture, and reassigning a fixture's name inside
// the test drops the type the fixture gave, though the fixture still runs and its calls count.
func TestPythonTestsReachTheCodeThroughTheirSetup(t *testing.T) {
	expect(t, calls(t, "python",
		"src/app/__init__.py", "",
		"src/app/store.py", "class Store:\n    def add(self, row):\n        return len(row)\n\n\ndef double(n):\n    return n * 2\n",
		"tests/test_store.py", `import functools
import unittest

import pytest

from app.store import Store, double

twice = lambda n: double(n)


@pytest.fixture
def store():
    return Store()


@pytest.fixture
def yielded():
    s = Store()
    yield Store()


def plain():
    return Store()


@functools.cache
def cached():
    return Store()


def test_fixture(store):
    assert store.add("x") == 1


def test_yield(yielded):
    assert yielded.add("x") == 1


def test_plain(plain):
    assert plain.add("x") == 1


def test_cached(cached):
    assert cached.add("x") == 1


def test_reassigned(store):
    store = object()
    assert store.add("x") == 1


def test_lambda():
    assert twice(2) == 4


class TestStore(unittest.TestCase):
    def setUp(self):
        self.store = Store()

    def test_setup(self):
        self.assertEqual(self.store.add("x"), 1)


class TestPlain:
    def setup_method(self):
        self.store = Store()

    def test_setup_method(self):
        assert self.store.add("x") == 1
`), `TestPlain.test_setup_method calls=[src/app/store.Store.add] unresolved=0
TestStore.test_setup calls=[src/app/store.Store.add] unresolved=0
test_cached calls=[] unresolved=0
test_fixture calls=[src/app/store.Store src/app/store.Store.add] unresolved=0
test_lambda calls=[src/app/store.double] unresolved=0
test_plain calls=[] unresolved=0
test_reassigned calls=[src/app/store.Store] unresolved=1
test_yield calls=[src/app/store.Store src/app/store.Store.add] unresolved=0`)
}

// viewpoint: behaviour
// target: the Python reader
// What a base class's setUp or setup_method stores on self types what the test of a class that
// inherits it calls, across files. A setUp the class writes itself replaces the base's, and when it
// calls the base's through super() first, what it stores itself wins.
func TestPythonTestsReachTheCodeThroughInheritedSetup(t *testing.T) {
	expect(t, calls(t, "python",
		"src/app/__init__.py", "",
		"src/app/store.py", "class Store:\n    def add(self, row):\n        return len(row)\n\n\nclass Ledger:\n    def add(self, row):\n        return 1\n",
		"tests/base.py", `import unittest

from app.store import Store


class StoreCase(unittest.TestCase):
    def setUp(self):
        self.store = Store()


class StorePlain:
    def setup_method(self):
        self.store = Store()
`,
		"tests/test_store.py", `from app.store import Ledger
from tests.base import StoreCase, StorePlain


class TestInherited(StoreCase):
    def test_add(self):
        self.assertEqual(self.store.add("x"), 1)


class TestPlain(StorePlain):
    def test_add(self):
        assert self.store.add("x") == 1


class TestOwn(StoreCase):
    def setUp(self):
        self.store = object()

    def test_add(self):
        self.assertEqual(self.store.add("x"), 1)


class TestSuper(StoreCase):
    def setUp(self):
        super().setUp()
        self.store = Ledger()

    def test_add(self):
        self.assertEqual(self.store.add("x"), 1)
`), `TestInherited.test_add calls=[src/app/store.Store.add] unresolved=0
TestOwn.test_add calls=[] unresolved=0
TestPlain.test_add calls=[src/app/store.Store.add] unresolved=0
TestSuper.test_add calls=[src/app/store.Ledger.add] unresolved=0`)
}

// viewpoint: behaviour
// target: the Python reader
// unittest collects every subclass of TestCase whatever its name: extended directly, through
// TestCase imported by name, or through a base class of the file or of an imported module. Their
// tests are read, and an inherited setUp types what they call. A class whose name does not start
// with Test and that extends no TestCase holds no test.
func TestPythonSubclassesOfTestCaseAreTestClasses(t *testing.T) {
	expect(t, calls(t, "python",
		"src/app/__init__.py", "",
		"src/app/store.py", "class Store:\n    def add(self, row):\n        return len(row)\n",
		"tests/base.py", `import unittest

from app.store import Store


class StoreCase(unittest.TestCase):
    def setUp(self):
        self.store = Store()
`,
		"tests/test_store.py", `import unittest
from unittest import TestCase

from app.store import Store
from tests.base import StoreCase


class StoreTests(unittest.TestCase):
    def test_add(self):
        s = Store()
        self.assertEqual(s.add("x"), 1)


class ImportedTests(TestCase):
    def test_add(self):
        s = Store()
        self.assertEqual(s.add("x"), 1)


class LocalCase(unittest.IsolatedAsyncioTestCase):
    pass


class ThroughLocal(LocalCase):
    def test_add(self):
        s = Store()
        self.assertEqual(s.add("x"), 1)


class InheritedTests(StoreCase):
    def test_add(self):
        self.assertEqual(self.store.add("x"), 1)


class Helpers:
    def test_not_collected(self):
        s = Store()
        assert s.add("x") == 1
`), `ImportedTests.test_add calls=[src/app/store.Store src/app/store.Store.add] unresolved=0
InheritedTests.test_add calls=[src/app/store.Store.add] unresolved=0
StoreTests.test_add calls=[src/app/store.Store src/app/store.Store.add] unresolved=0
ThroughLocal.test_add calls=[src/app/store.Store src/app/store.Store.add] unresolved=0`)
}

// viewpoint: behaviour
// target: the Python reader
// What a class's autouse fixture stores on self types what its tests call, the class's own fixture
// or one it inherits. A fixture of the same name nearer in the MRO replaces the base's, and what
// setup_method stores wins over what an autouse fixture stores.
func TestPythonTestsReachTheCodeThroughClassAutouseFixtures(t *testing.T) {
	expect(t, calls(t, "python",
		"src/app/__init__.py", "",
		"src/app/store.py", "class Store:\n    def add(self, row):\n        return len(row)\n\n\nclass Ledger:\n    def add(self, row):\n        return 1\n",
		"tests/test_store.py", `import pytest

from app.store import Ledger, Store


class StoreFixture:
    @pytest.fixture(autouse=True)
    def _store(self):
        self.store = Store()


class TestOwn:
    @pytest.fixture(autouse=True)
    def _store(self):
        self.store = Store()
        yield

    def test_add(self):
        assert self.store.add("x") == 1


class TestInherited(StoreFixture):
    def test_add(self):
        assert self.store.add("x") == 1


class TestOverridden(StoreFixture):
    @pytest.fixture(autouse=True)
    def _store(self):
        self.store = Ledger()

    def test_add(self):
        assert self.store.add("x") == 1


class TestSetupWins(StoreFixture):
    def setup_method(self):
        self.store = Ledger()

    def test_add(self):
        assert self.store.add("x") == 1
`), `TestInherited.test_add calls=[src/app/store.Store.add] unresolved=0
TestOverridden.test_add calls=[src/app/store.Ledger.add] unresolved=0
TestOwn.test_add calls=[src/app/store.Store.add] unresolved=0
TestSetupWins.test_add calls=[src/app/store.Ledger.add] unresolved=0`)
}

// viewpoint: behaviour
// target: the Python and TypeScript readers
// A test that calls a function of a test support file is marked as calling support code, whether
// or not that function reaches the product; a test that calls none is not.
func TestTestsThatCallSupportCodeAreMarked(t *testing.T) {
	marked := func(a []string) string {
		var out []string
		for _, ts := range analyze(t, a[0], tree(a[1:]...)).Tests {
			out = append(out, fmt.Sprintf("%s support=%v", ts.Name, ts.CallsSupport))
		}
		sort.Strings(out)
		return strings.Join(out, "\n")
	}
	expect(t, marked([]string{"python",
		"src/app/__init__.py", "",
		"src/app/store.py", "def rows():\n    return [1]\n",
		"tests/helpers.py", "import os\n\n\ndef force_zone(zone):\n    os.environ[\"TZ\"] = zone\n",
		"tests/test_helpers_itself.py", `import os

from tests.helpers import force_zone
from app.store import rows


def test_zone():
    force_zone("UTC")
    assert os.environ["TZ"] == "UTC"


def test_rows():
    assert rows() == [1]
`}), "test_rows support=false\ntest_zone support=true")
	expect(t, marked([]string{"typescript",
		"src/rows.ts", "export function rows() { return [1]; }\n",
		"test/clock.ts", "export function forceZone(zone: string) { process.env.TZ = zone; }\n",
		"test/clock.test.ts", `import { forceZone } from "./clock";
import { rows } from "../src/rows";

it("sets the zone", () => {
  forceZone("UTC");
  expect(process.env.TZ).toBe("UTC");
});

it("lists rows", () => {
  expect(rows()).toEqual([1]);
});
`}), "lists rows support=false\nsets the zone support=true")
}

// viewpoint: behaviour
// target: the Python reader
// A method called on an object built in the same expression is a call of the class's method, as
// it is when a local holds the object.
func TestPythonMethodOnABuiltObjectIsTheClassMethod(t *testing.T) {
	expect(t, calls(t, "python",
		"src/app/__init__.py", "",
		"src/app/store.py", "class Store:\n    def add(self, row):\n        return len(row)\n",
		"tests/test_store.py", `from app.store import Store


def test_inline():
    assert Store().add("x") == 1


def test_local():
    s = Store()
    assert s.add("x") == 1
`), `test_inline calls=[src/app/store.Store src/app/store.Store.add] unresolved=0
test_local calls=[src/app/store.Store src/app/store.Store.add] unresolved=0`)
}
