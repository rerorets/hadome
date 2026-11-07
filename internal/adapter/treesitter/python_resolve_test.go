//go:build cgo

package treesitter

import "testing"

// viewpoint: behaviour
// target: the Python reader
// A project in a subdirectory, marked by its pyproject.toml or setup.py, keeps its packages in its
// src/ or beside the marker; a test of that project imports them by their package name.
func TestPythonResolvesPackagesOfNestedProjects(t *testing.T) {
	expect(t, calls(t, "python",
		"services/billing/pyproject.toml", "[project]\nname = \"billing\"\n",
		"services/billing/src/billing/__init__.py", "",
		"services/billing/src/billing/invoice.py", "def total(rows):\n    return sum(rows)\n",
		"services/billing/tests/test_invoice.py", "from billing.invoice import total\n\n\ndef test_total():\n    assert total([1]) == 1\n",
		"services/notify/setup.py", "from setuptools import setup\n\nsetup(name=\"notify\")\n",
		"services/notify/notify/__init__.py", "",
		"services/notify/notify/send.py", "def send(to):\n    return to\n",
		"services/notify/tests/test_send.py", "import notify.send\n\n\ndef test_send():\n    assert notify.send.send(\"a\") == \"a\"\n",
	), `
test_send calls=[services/notify/notify/send.send] unresolved=0
test_total calls=[services/billing/src/billing/invoice.total] unresolved=0`)
}

// viewpoint: behaviour
// target: the Python reader
// An import that looks like the repository's own (relative, or under a package the repository
// has) but resolves to no module is a call hadome cannot identify, not a call of an outside
// package.
func TestPythonMissingLocalImportsAreUnresolved(t *testing.T) {
	expect(t, calls(t, "python",
		"app/__init__.py", "",
		"app/store.py", "def add(row):\n    return row\n",
		"app/test_store.py", `import requests

from app.gone import vanish
from .missing import lost
from app.store import add


def test_gone():
    assert vanish() == 1


def test_relative():
    assert lost() == 1


def test_outside():
    assert requests.get("x")


def test_found():
    assert add(1) == 1
`,
	), `
test_found calls=[app/store.add] unresolved=0
test_gone calls=[] unresolved=1
test_outside calls=[] unresolved=0
test_relative calls=[] unresolved=1`)
}

// viewpoint: behaviour
// target: the Python reader
// conftest.py and the modules under tests/ are test support, not product: they hold no targets
// and no tests of their own. A test is followed into a helper it imports from them and into the
// fixtures it receives from conftest.py, so it reaches the product code they call.
func TestPythonTestSupportFilesAreFollowed(t *testing.T) {
	a := analyze(t, "python", tree(
		"app/__init__.py", "",
		"app/store.py", "class Store:\n    def add(self, row):\n        return row\n\n\ndef seed():\n    return 1\n",
		"tests/__init__.py", "",
		"tests/conftest.py", `import pytest

from app.store import Store, seed


@pytest.fixture
def store():
    seed()
    return Store()


def test_data():
    return [1]
`,
		"tests/helpers.py", "from app.store import seed\n\n\ndef prepared():\n    return seed()\n\n\nLIMIT = 3\n",
		"tests/api/test_store.py", `from tests import helpers
from tests.helpers import prepared, LIMIT


def test_fixture(store):
    assert store.add(1) == 1


def test_helper():
    assert prepared() == 1


def test_module():
    assert helpers.prepared() == 1


def test_value():
    assert LIMIT() == 3
`,
	))
	expect(t, lines(summary(a), "file ", "symbol ", "test "), `
file app/__init__.py source unit=app at=app
file app/store.py source unit=app at=app
file tests/__init__.py test unit=tests at=tests
file tests/api/test_store.py test unit=tests/api at=tests/api
file tests/conftest.py test unit=tests at=tests
file tests/helpers.py test unit=tests at=tests
symbol function app/store.seed exported=true doc=false params=0 cx=1 methods=0 scoped=false
symbol method app/store.Store.add exported=true doc=false params=1 cx=1 methods=0 scoped=false
symbol type app/store.Store exported=true doc=false params=0 cx=0 methods=0 scoped=false
test "test_fixture" calls=[app/store.Store×1/0 app/store.Store.add×1/1 app/store.seed×1/0] hazards=[] skip=false:"" unresolved=0
test "test_helper" calls=[app/store.seed×1/0] hazards=[] skip=false:"" unresolved=0
test "test_module" calls=[app/store.seed×1/0] hazards=[] skip=false:"" unresolved=0
test "test_value" calls=[] hazards=[] skip=false:"" unresolved=1
`)
}
