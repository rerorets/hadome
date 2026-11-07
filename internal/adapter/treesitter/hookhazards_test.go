//go:build cgo

package treesitter

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/rerorets/hadome/internal/facts"
)

// timeHazards renders each test's clock and sleep hazards as "name: category@path:line", sorted.
func timeHazards(a *facts.Analysis) string {
	var out []string
	for _, x := range a.Tests {
		var hs []string
		for _, h := range x.Hazards {
			if h.Category == facts.HazardClock || h.Category == facts.HazardSleep {
				hs = append(hs, fmt.Sprintf("%s@%s:%d", h.Category, h.Path, h.Line))
			}
		}
		slices.Sort(hs)
		out = append(out, x.Name+": "+strings.Join(hs, " "))
	}
	slices.Sort(out)
	return strings.Join(out, "\n")
}

// viewpoint: behaviour
// target: the TypeScript reader
// A beforeAll or beforeEach runs before each test of its file or describe block, so reading the
// clock or sleeping there is a hazard of each of those tests, at the hook's line. A test outside the
// block does not get the block's hooks, and a hook that turns on fake timers first reads the fake
// clock.
func TestTypeScriptHooksGiveTheirClockAndSleepToEachTest(t *testing.T) {
	a := analyze(t, "typescript", tree(
		"src/calc.ts", "export const add = (a: number, b: number) => a + b;\n",
		"src/calc.test.ts", `import { add } from './calc';
beforeAll(() => { Date.now(); });
describe('slow', () => {
  beforeEach(async () => { await new Promise((r) => setTimeout(r, 5)); });
  it('adds', () => { expect(add(1, 2)).toBe(3); });
  it('adds again', () => { expect(add(2, 2)).toBe(4); });
});
describe('faked', () => {
  beforeEach(() => { vi.useFakeTimers(); setTimeout(() => {}, 5); });
  it('adds', () => { expect(add(1, 2)).toBe(3); });
});
it('adds alone', () => { expect(add(1, 1)).toBe(2); });
`,
	))
	expect(t, timeHazards(a), `
adds alone: clock@src/calc.test.ts:2
faked adds: clock@src/calc.test.ts:2
slow adds again: clock@src/calc.test.ts:2 sleep@src/calc.test.ts:4
slow adds: clock@src/calc.test.ts:2 sleep@src/calc.test.ts:4`)
}

// viewpoint: behaviour
// target: the Python reader
// What pytest and unittest run before a test without the test asking for it gives the test its
// clock and sleep hazards, at the setup's own line: the autouse fixtures of the file, of a
// conftest.py above it and of the test's class, setUp and setUpClass of a unittest class,
// setup_method of a pytest class, and setup_module and setup_function. A fixture that is not
// autouse gives nothing to a test that does not ask for it, and a class's setup gives nothing to the
// tests outside the class.
func TestPythonSetupGivesItsClockAndSleepToEachTest(t *testing.T) {
	a := analyze(t, "python", tree(
		"app/__init__.py", "",
		"app/calc.py", "def add(a, b):\n    return a + b\n",
		"tests/conftest.py", `import time

import pytest


@pytest.fixture(autouse=True)
def pause():
    time.sleep(1)
`,
		"tests/test_calc.py", `import time
import unittest
from datetime import datetime

import pytest

from app.calc import add


@pytest.fixture(autouse = True)
def stamp():
    datetime.now()


@pytest.fixture
def slow():
    time.sleep(1)
    return 1


def setup_function():
    time.time()


def test_add():
    assert add(1, 2) == 3


class TestCalc(unittest.TestCase):
    def setUp(self):
        time.sleep(1)

    @classmethod
    def setUpClass(cls):
        datetime.now()

    def test_add(self):
        self.assertEqual(add(1, 2), 3)


class TestMore:
    def setup_method(self):
        time.sleep(2)

    @pytest.fixture(autouse=True)
    def ready(self):
        time.time()

    def test_add(self):
        assert add(2, 2) == 4
`,
		"tests/other/test_other.py", `from app.calc import add


def setup_module():
    import time
    time.sleep(3)


def test_other():
    assert add(1, 1) == 2
`,
	))
	expect(t, timeHazards(a), `
TestCalc.test_add: clock@tests/test_calc.py:12 clock@tests/test_calc.py:35 sleep@tests/conftest.py:8 sleep@tests/test_calc.py:31
TestMore.test_add: clock@tests/test_calc.py:12 clock@tests/test_calc.py:47 sleep@tests/conftest.py:8 sleep@tests/test_calc.py:43
test_add: clock@tests/test_calc.py:12 clock@tests/test_calc.py:22 sleep@tests/conftest.py:8
test_other: sleep@tests/conftest.py:8 sleep@tests/other/test_other.py:6`)
}

// viewpoint: behaviour
// target: the Python reader
// A test class runs the setup it inherits, found the way Python finds a method: from the class
// itself up through its base classes, in the same file or in a module the test file imports, over
// several levels. A setup the class defines itself replaces the base's, unless it calls the base's
// through super(). The autouse fixtures of a base class run too, unless the class defines a fixture
// of the same name. A base class hadome cannot find gives nothing, and the test is still read.
func TestPythonTestClassesRunTheSetupTheyInherit(t *testing.T) {
	a := analyze(t, "python", tree(
		"app/__init__.py", "",
		"app/calc.py", "def add(a, b):\n    return a + b\n",
		"tests/base.py", `import time
import unittest
from datetime import datetime

import pytest


class BaseCase(unittest.TestCase):
    def setUp(self):
        time.sleep(1)


class TimedCase(BaseCase):
    @classmethod
    def setUpClass(cls):
        datetime.now()


class BasePlain:
    def setup_method(self):
        time.time()

    @pytest.fixture(autouse=True)
    def ready(self):
        time.sleep(2)
`,
		"tests/test_orders.py", `import time
import unittest

import pytest

import tests.base as base
from app.calc import add
from tests.base import BaseCase, BasePlain, TimedCase
from vendorlib import ExternalCase


class LocalBase(unittest.TestCase):
    def setUp(self):
        time.monotonic()


class TestLocal(LocalBase):
    def test_add(self):
        self.assertEqual(add(1, 2), 3)


class TestImported(BaseCase):
    def test_add(self):
        self.assertEqual(add(1, 2), 3)


class TestModule(base.BaseCase):
    def test_add(self):
        self.assertEqual(add(1, 2), 3)


class TestDeep(TimedCase):
    def test_add(self):
        self.assertEqual(add(1, 2), 3)


class TestOverride(BaseCase):
    def setUp(self):
        self.ready = True

    def test_add(self):
        self.assertEqual(add(1, 2), 3)


class TestSuper(BaseCase):
    def setUp(self):
        super().setUp()

    def test_add(self):
        self.assertEqual(add(1, 2), 3)


class TestExternal(ExternalCase):
    def test_add(self):
        self.assertEqual(add(1, 2), 3)


class TestMissingBase(Missing):
    def test_add(self):
        self.assertEqual(add(1, 2), 3)


class TestLoop(TestLoop):
    def test_add(self):
        self.assertEqual(add(1, 2), 3)


class TestPlain(BasePlain):
    def test_add(self):
        assert add(1, 2) == 3


class TestPlainOverride(BasePlain):
    @pytest.fixture(autouse=True)
    def ready(self):
        yield

    def test_add(self):
        assert add(1, 2) == 3
`,
	))
	// A test with no such hazard renders as "name: "; the space is dropped so the want reads plainly.
	expect(t, strings.TrimSpace(strings.ReplaceAll(timeHazards(a)+"\n", ": \n", ":\n")), `
TestDeep.test_add: clock@tests/base.py:16 sleep@tests/base.py:10
TestExternal.test_add:
TestImported.test_add: sleep@tests/base.py:10
TestLocal.test_add: clock@tests/test_orders.py:14
TestLoop.test_add:
TestMissingBase.test_add:
TestModule.test_add: sleep@tests/base.py:10
TestOverride.test_add:
TestPlain.test_add: clock@tests/base.py:21 sleep@tests/base.py:25
TestPlainOverride.test_add: clock@tests/base.py:21
TestSuper.test_add: sleep@tests/base.py:10
`)
}

// viewpoint: behaviour
// target: the Python reader
// With several base classes, setup is looked up in Python's method resolution order: the first
// base listed wins over the second, and in a diamond super() in the left branch reaches the right
// branch before the shared root.
func TestPythonInheritedSetupFollowsTheMethodResolutionOrder(t *testing.T) {
	a := analyze(t, "python", tree(
		"app/__init__.py", "",
		"app/calc.py", "def add(a, b):\n    return a + b\n",
		"tests/test_calc.py", `import time
import unittest
from datetime import datetime

from app.calc import add


class Root(unittest.TestCase):
    def setUp(self):
        time.sleep(1)


class Left(Root):
    def setUp(self):
        super().setUp()


class Right(Root):
    def setUp(self):
        datetime.now()


class TestFirst(Right, Left):
    def test_add(self):
        self.assertEqual(add(1, 2), 3)


class TestDiamond(Left, Right):
    def test_add(self):
        self.assertEqual(add(1, 2), 3)
`,
	))
	expect(t, timeHazards(a), `
TestDiamond.test_add: clock@tests/test_calc.py:20
TestFirst.test_add: clock@tests/test_calc.py:20`)
}
