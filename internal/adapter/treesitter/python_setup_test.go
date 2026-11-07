//go:build cgo

package treesitter

import "testing"

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
