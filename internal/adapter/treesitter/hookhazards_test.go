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
