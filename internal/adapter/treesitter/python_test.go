//go:build cgo

package treesitter

import (
	"fmt"
	"testing"
)

// viewpoint: behaviour
// target: the Python reader
// The facts rules see from a small project with a src/ layout: packages as units, absolute and
// relative imports, public and documented symbols, and pytest tests with skips, mocks and calls.
func TestPythonFacts(t *testing.T) {
	a := analyze(t, "python", tree(
		"src/app/__init__.py", "",
		"src/app/store.py", `"""Rows."""
import sqlite3
from . import util


class Store:
    """Store keeps rows."""

    def add(self, row):
        if row and len(row) > 1:
            return row

    def _secret(self):
        pass


def create_post(user_id, body):
    """Create a post."""
    return body


def _helper():
    pass
`,
		"src/app/util.py", "import requests\nimport os.path\n",
		"tests/test_store.py", `import time
from unittest import mock
from app.store import create_post, Store
from app import store as st
import pytest


def make():
    return Store()


def test_create_twice():
    create_post("u1", "a")
    create_post("u2", "a")
    s = Store()
    s.add("x")
    time.sleep(1)
    assert True


@pytest.mark.skip(reason="needs a database")
def test_later():
    st.create_post("u1", "b")


class TestLoop:
    @pytest.mark.skip
    def test_loop(self, mocker):
        for x in [1, 2]:
            create_post(x, "b")
        thing = make()
        thing.run()
`))
	expect(t, summary(a), `
file src/app/__init__.py source unit=src/app at=src/app
file src/app/store.py source unit=src/app at=src/app
file src/app/util.py source unit=src/app at=src/app
file tests/test_store.py test unit=tests at=tests
import src/app -> os.path internal=false io=true
import src/app -> requests internal=false io=true
import src/app -> sqlite3 internal=false io=true
import src/app -> src/app internal=true io=false
symbol function src/app/store._helper exported=false doc=false params=0 cx=1 methods=0 scoped=false
symbol function src/app/store.create_post exported=true doc=true params=2 cx=1 methods=0 scoped=true
symbol method src/app/store.Store._secret exported=false doc=false params=0 cx=1 methods=0 scoped=false
symbol method src/app/store.Store.add exported=true doc=false params=1 cx=3 methods=0 scoped=false
symbol type src/app/store.Store exported=true doc=true params=0 cx=0 methods=0 scoped=false
test "TestLoop.test_loop" calls=[src/app/store.Store×1/0 src/app/store.create_post×2/1] hazards=[mock mock] skip=true:"" unresolved=1
test "test_create_twice" calls=[src/app/store.Store×1/0 src/app/store.Store.add×1/1 src/app/store.create_post×2/2] hazards=[mock sleep always-true] skip=false:"" unresolved=0
test "test_later" calls=[src/app/store.create_post×1/1] hazards=[mock] skip=true:"needs a database" unresolved=0
`)
}

// viewpoint: behaviour
// target: the Python reader
// pytest.skip inside a branch skips only sometimes: the test still guards, and still needs a reason.
func TestPythonConditionalSkip(t *testing.T) {
	a := analyze(t, "python", tree("app/store.py", "def append(row):\n    pass\n",
		"tests/test_store.py", "import sys\nimport pytest\nfrom app.store import append\n\n\n"+
			"def test_append():\n    if sys.platform == 'win32':\n        pytest.skip('no windows')\n    append('a')\n\n\n"+
			"def test_never():\n    pytest.skip('broken')\n    append('a')\n"))
	got := map[string]string{}
	for _, ts := range a.Tests {
		got[ts.Name] = fmt.Sprintf("skipped=%t conditional=%t reason=%q", ts.Skip.Skipped, ts.Skip.Conditional, ts.Skip.Reason)
	}
	if got["test_append"] != `skipped=false conditional=true reason="no windows"` || got["test_never"] != `skipped=true conditional=false reason="broken"` {
		t.Errorf("got %v", got)
	}
}
