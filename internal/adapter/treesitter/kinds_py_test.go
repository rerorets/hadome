// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

//go:build cgo

package treesitter

import (
	"fmt"
	"strings"
	"testing"
)

// viewpoint: behaviour
// target: the Python reader
// A local's annotation decides what it is, as in TypeScript: a built-in type or one imported from
// outside is outside, as is a container of them and its elements; one of the repository's classes
// or generic classes, by name, through a module or as a forward reference, is that class; an alias
// of the repository is what it names; Optional, Union and | read as unions; a container of the
// repository's class is outside, its elements of that class; Any, object, a name nothing declares
// and an alias that names itself say nothing, so the value decides, here unknown.
func TestPythonAnnotationsDecideWhatALocalIs(t *testing.T) {
	types := [][2]string{{"str", "str"}, {"any", "Any"}, {"object", "object"}, {"store", "Store"},
		{"optional", "Optional[Store]"}, {"or_none", "Store | None"}, {"union_none", "Union[Store, None]"},
		{"forward", `"Store"`}, {"str_or_int", "str | int"}, {"str_or_store", "str | Store"}, {"union", "Union[str, int]"},
		{"list", "list[Store]"}, {"typing_list", "List[Store]"}, {"dotted_list", "typing.List[Store]"},
		{"sequence", "Sequence[str]"}, {"tuple_of", "tuple[Store, ...]"}, {"pair", "tuple[str, int]"},
		{"mixed_pair", "tuple[str, Store]"}, {"dict_store", "dict[str, Store]"}, {"dict_int", "dict[str, int]"},
		{"box", "Box[Store]"}, {"module", "st.Store"}, {"ext_module", "ext_lib.Thing"}, {"thing", "Thing"},
		{"grid", "Grid"}, {"port", "Port"}, {"missing", "Missing"}, {"local", "Local"}, {"cycle", "Cycle"},
		{"nope", "nope.Store"}}
	var b strings.Builder
	for _, nt := range types {
		fmt.Fprintf(&b, "\ndef test_%s():\n    x: %s = load()\n    x.add(1)\n", nt[0], nt[1])
		fmt.Fprintf(&b, "\ndef test_%s_element():\n    x: %s = load()\n    for e in x:\n        e.add(1)\n", nt[0], nt[1])
	}
	got := kinds(t, "python",
		"app/__init__.py", "",
		"app/store.py", `from typing import Protocol

class Store:
    def add(self, n):
        return n

class Box:
    def add(self, n):
        return n

class Port(Protocol):
    def add(self, n): ...

Grid = list[list[str]]
`,
		"tests/test_types.py", `import typing
import ext_lib
import app.store as st
from typing import Any, Optional, Union, List, Sequence
from ext_lib import Thing
from app.store import Store, Box, Port, Grid

Local = list[Store]
Cycle = Again
Again = Cycle

def load():
    return None
`+b.String())
	expectEach(t, got, map[string]string{
		"test_str": "calls=[] unresolved=0", "test_str_element": "calls=[] unresolved=0",
		"test_any": "calls=[] unresolved=1", "test_object": "calls=[] unresolved=1",
		"test_store": "calls=[app/store.Store.add] unresolved=0", "test_store_element": "calls=[] unresolved=1",
		"test_optional": "calls=[app/store.Store.add] unresolved=0", "test_or_none": "calls=[app/store.Store.add] unresolved=0",
		"test_union_none": "calls=[app/store.Store.add] unresolved=0", "test_forward": "calls=[app/store.Store.add] unresolved=0",
		"test_str_or_int": "calls=[] unresolved=0", "test_str_or_int_element": "calls=[] unresolved=0",
		"test_str_or_store": "calls=[] unresolved=1", "test_union_element": "calls=[] unresolved=0",
		"test_list": "calls=[] unresolved=0", "test_list_element": "calls=[app/store.Store.add] unresolved=0",
		"test_typing_list_element": "calls=[app/store.Store.add] unresolved=0",
		"test_dotted_list_element": "calls=[app/store.Store.add] unresolved=0",
		"test_sequence_element":    "calls=[] unresolved=0", "test_tuple_of_element": "calls=[app/store.Store.add] unresolved=0",
		"test_pair_element": "calls=[] unresolved=0", "test_mixed_pair_element": "calls=[] unresolved=1",
		"test_dict_store": "calls=[] unresolved=0", "test_dict_store_element": "calls=[] unresolved=1",
		"test_dict_int_element": "calls=[] unresolved=0", "test_box": "calls=[app/store.Box.add] unresolved=0",
		"test_module": "calls=[app/store.Store.add] unresolved=0", "test_ext_module": "calls=[] unresolved=0",
		"test_thing": "calls=[] unresolved=0", "test_grid": "calls=[] unresolved=0", "test_grid_element": "calls=[] unresolved=0",
		"test_port": "calls=[app/store.Port.add] unresolved=0", "test_missing": "calls=[] unresolved=1",
		"test_local_element": "calls=[app/store.Store.add] unresolved=0", "test_cycle": "calls=[] unresolved=1",
		"test_nope": "calls=[] unresolved=1",
	})
}

// viewpoint: behaviour
// target: the Python reader
// A local without an annotation is what its value makes it: arithmetic on outside values, a literal,
// a member of an outside module and a built-in function's result are outside; arithmetic on values
// of unknown kind is unknown, as a class may define the operator; a copy by list() keeps its
// argument's elements; and unpacking gives each name an element, or the rest for a starred name.
func TestPythonValuesDecideWhatALocalIs(t *testing.T) {
	got := kinds(t, "python",
		"app/__init__.py", "",
		"app/store.py", "class Store:\n    def add(self, n):\n        return n\n",
		"tests/test_values.py", `import os
from app.store import Store

def load():
    return None

def test_sum():
    n = 1 + 2
    n.bit_length()

def test_added():
    n = load() + load()
    n.bit_length()

def test_literal():
    xs = [1, 2]
    xs.append(3)

def test_member():
    sep = os.sep
    sep.join([])

def test_builtin():
    n = len([1])
    n.bit_length()

def test_copied():
    stores: list[Store] = load()
    for s in list(stores):
        s.add(1)

def test_unpacked():
    stores: list[Store] = load()
    first, second = stores
    first.add(1)

def test_starred():
    stores: list[Store] = load()
    first, *rest = stores
    for s in rest:
        s.add(1)
`)
	expectEach(t, got, map[string]string{
		"test_sum": "calls=[] unresolved=0", "test_added": "calls=[] unresolved=1",
		"test_literal": "calls=[] unresolved=0", "test_member": "calls=[] unresolved=0",
		"test_builtin": "calls=[] unresolved=0", "test_copied": "calls=[app/store.Store.add] unresolved=0",
		"test_unpacked": "calls=[app/store.Store.add] unresolved=0",
		"test_starred":  "calls=[app/store.Store.add] unresolved=0",
	})
}

// viewpoint: behaviour
// target: the Python reader
// A variable imported from the repository is what its declaration makes it: its annotation, else
// its value read without running anything, where a literal is outside and holds what it names, an
// operator on outside values is outside, another variable or a name from outside gives its kind,
// and a call makes a built-in value or an object of the repository's class. Variables that name
// each other stay unknown: their methods are named after them, as before.
func TestPythonImportedVariablesAreWhatTheirDeclarationsMake(t *testing.T) {
	names := []string{"NAMES", "ALIAS", "EXT", "SUM", "MONEY", "MADE", "LOOP_A", "HOLDS", "COPIED", "TYPED"}
	var b strings.Builder
	for _, n := range names {
		fmt.Fprintf(&b, "\ndef test_%s():\n    %s.add(1)\n", strings.ToLower(n), n)
		fmt.Fprintf(&b, "\ndef test_%s_element():\n    for e in %s:\n        e.add(1)\n", strings.ToLower(n), n)
	}
	got := kinds(t, "python",
		"app/__init__.py", "",
		"app/store.py", "class Store:\n    def add(self, n):\n        return n\n",
		"app/data.py", `import ext_lib
from app.store import Store

def load():
    return None

NAMES = ["a"]
ALIAS = NAMES
EXT = ext_lib
SUM = 1 + 2
MONEY = load() + load()
MADE = Store()
LOOP_A = LOOP_B
LOOP_B = LOOP_A
HOLDS = [1, MADE]
COPIED = len(NAMES)
TYPED: list[Store] = []
`,
		"tests/test_data.py", "from app.data import "+strings.Join(names, ", ")+"\n"+b.String())
	expectEach(t, got, map[string]string{
		"test_names": "calls=[] unresolved=0 reads=1", "test_names_element": "calls=[] unresolved=0",
		"test_alias": "calls=[] unresolved=0 reads=1", "test_ext": "calls=[] unresolved=0 reads=1",
		"test_sum": "calls=[] unresolved=0 reads=1", "test_money": "calls=[app/data.MONEY.add] unresolved=0",
		"test_made": "calls=[app/store.Store.add] unresolved=0", "test_loop_a": "calls=[app/data.LOOP_A.add] unresolved=0",
		"test_holds": "calls=[] unresolved=0 reads=1", "test_holds_element": "calls=[] unresolved=1",
		"test_copied": "calls=[] unresolved=0 reads=1",
		"test_typed":  "calls=[] unresolved=0 reads=1", "test_typed_element": "calls=[app/store.Store.add] unresolved=0",
	})
}
