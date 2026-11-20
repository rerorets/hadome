// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

//go:build cgo

package treesitter

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/rerorets/hadome/internal/facts"
)

// kinds lists each test of a tree as its targets, its unresolved calls and its reads.
func kinds(t *testing.T, name string, files ...string) string {
	t.Helper()
	var out []string
	for _, ts := range analyze(t, name, tree(files...)).Tests {
		var targets []string
		for _, c := range ts.Calls {
			targets = append(targets, c.Target)
		}
		out = append(out, fmt.Sprintf("%s calls=%v unresolved=%d reads=%d", ts.Name, targets, ts.Unresolved, ts.Reads))
	}
	sort.Strings(out)
	return strings.Join(out, "\n")
}

// viewpoint: behaviour
// target: the TypeScript reader
// What a value of a test is decides how a method called on it counts: a method of a value whose
// written type, value, element or imported declaration is outside the repository counts as
// nothing, and the value it is taken from is read; one of the repository's type is that type's
// method; anything else is unresolved. A written type that says nothing, such as any, gives way to
// the value.
func TestTypeScriptKindsOfValuesDecideHowTheirMethodsCount(t *testing.T) {
	expect(t, kinds(t, "typescript",
		"src/store.ts", "export class Store { add(n: number) { return n } }\nexport function list(): Store[] { return [] }\n",
		"src/data.ts", `import { Store } from './store'
export type Grid = string[][]
export const FRAMES: string[][] = [['a.']]
export const GRID: Grid = [['a.']]
export const NAMES = ['a', 'b']
export const STORES: Store[] = []
export const MIXED = [new Store()]
export const shared: Store = new Store()
export const made = new Store()
export const config = load()
function load() { return { read() {} } }
`,
		"src/data.test.ts", `import { FRAMES, GRID, NAMES, STORES, MIXED, shared, made, config } from './data'
import { list } from './store'
const joined = (g: string[]) => g.join('')
const first = (s) => s.add(1)

test('index', () => { const g = FRAMES[0]; g.map((r) => r) })
test('written', () => { const g: string[] = load(); g.map((r) => r) })
test('any', () => { const g: any = [1]; g.map((r) => r) })
test('loop', () => { for (const g of FRAMES) g.map((r) => r) })
test('keys', () => { for (const k in shared) k.trim() })
test('destructured', () => { const [g, ...rest] = FRAMES; g.map((r) => r); rest.pop() })
test('helper param', () => { joined(load()) })
test('alias', () => { for (const g of GRID) g.map((r) => r) })
test('literal', () => { NAMES[0].trim(); const n = NAMES[1]; n.trim() })
test('element of the repository', () => { for (const s of STORES) s.add(1) })
test('holding', () => { const s = MIXED[0]; s.add(1) })
test('imported typed', () => { shared.add(1) })
test('imported made', () => { made.add(1) })
test('imported unknown', () => { config.read() })
test('imported outside', () => { FRAMES.map((g) => g) })
test('unknown loop', () => { for (const s of list()) s.add(1) })
test('passed element', () => { first(STORES[0]) })
function load(): any { return [] }
`), `alias calls=[] unresolved=0 reads=1
any calls=[] unresolved=0 reads=0
destructured calls=[] unresolved=0 reads=1
element of the repository calls=[src.Store.add] unresolved=0 reads=1
helper param calls=[] unresolved=0 reads=0
holding calls=[] unresolved=1 reads=1
imported made calls=[src.Store.add] unresolved=0 reads=0
imported outside calls=[] unresolved=0 reads=1
imported typed calls=[src.Store.add] unresolved=0 reads=0
imported unknown calls=[src.config.read] unresolved=0 reads=0
index calls=[] unresolved=0 reads=1
keys calls=[] unresolved=0 reads=1
literal calls=[] unresolved=0 reads=2
loop calls=[] unresolved=0 reads=1
passed element calls=[src.Store.add] unresolved=0 reads=1
unknown loop calls=[src.list] unresolved=1 reads=0
written calls=[] unresolved=0 reads=0`)
}

// viewpoint: behaviour
// target: the Python reader
// As in TypeScript, a method of a local whose written type, value, element or imported declaration
// is outside the repository counts as nothing; one of the repository's class is that class's
// method; anything else is unresolved. An operator on values of unknown kind may be a method of
// their class, so its result is unknown.
func TestPythonKindsOfValuesDecideHowTheirMethodsCount(t *testing.T) {
	expect(t, kinds(t, "python",
		"app/__init__.py", "",
		"app/store.py", "class Store:\n    def add(self, n):\n        return n\n\ndef listing() -> list[Store]:\n    return []\n",
		"app/data.py", `from typing import Optional
from app.store import Store

Grid = list[list[str]]
FRAMES: list[list[str]] = [["a."]]
GRID: Grid = [["a."]]
NAMES = ["a", "b"]
STORES: list[Store] = []
MIXED = [Store()]
SHARED: Optional[Store] = None
`,
		"tests/test_kinds.py", `import json
from app.data import FRAMES, GRID, NAMES, STORES, MIXED, SHARED
from app.store import listing

def load():
    return []

def test_index():
    g = FRAMES[0]
    g.count("a")

def test_written():
    g: list[str] = load()
    g.count("a")

def test_loop():
    for g in FRAMES:
        g.count("a")

def test_unpacked():
    g, *rest = FRAMES
    g.count("a")
    rest.pop()

def test_alias():
    for g in GRID:
        g.count("a")

def test_literal():
    n = NAMES[0]
    n.strip()

def test_copied():
    xs = list(FRAMES)
    xs.append(["b"])

def test_external():
    out = json.loads("[]")
    out.append(1)

def test_element_of_the_repository():
    for s in STORES:
        s.add(1)

def test_holding():
    s = MIXED[0]
    s.add(1)

def test_imported_typed():
    SHARED.add(1)

def test_imported_outside():
    FRAMES.count(["a."])

def test_unknown_loop():
    for s in listing():
        s.add(1)

def test_operator():
    n = load() + load()
    n.add(1)
`), `test_alias calls=[] unresolved=0 reads=1
test_copied calls=[] unresolved=0 reads=1
test_element_of_the_repository calls=[app/store.Store.add] unresolved=0 reads=1
test_external calls=[] unresolved=0 reads=0
test_holding calls=[] unresolved=1 reads=1
test_imported_outside calls=[] unresolved=0 reads=1
test_imported_typed calls=[app/store.Store.add] unresolved=0 reads=0
test_index calls=[] unresolved=0 reads=1
test_literal calls=[] unresolved=0 reads=1
test_loop calls=[] unresolved=0 reads=1
test_operator calls=[] unresolved=1 reads=0
test_unknown_loop calls=[app/store.listing] unresolved=1 reads=0
test_unpacked calls=[] unresolved=0 reads=1
test_written calls=[] unresolved=0 reads=0`)
}

// viewpoint: behaviour
// target: the Rust reader
// As in TypeScript, a method of a local whose written type, value, element or constant is outside
// the crate counts as nothing, and a constant whose method counts as nothing is read; one of the
// crate's type is that type's method; anything else is unresolved. An iterator over a container
// takes out its elements as they are.
func TestRustKindsOfValuesDecideHowTheirMethodsCount(t *testing.T) {
	expect(t, kinds(t, "rust",
		"Cargo.toml", "[package]\nname = \"acme\"\n",
		"src/lib.rs", `pub struct Store;
impl Store { pub fn add(&self, n: i32) -> i32 { n } }
pub type Grid = Vec<Vec<String>>;
pub const FRAMES: [&str; 2] = ["a.", ".a"];
pub static STORES: [Store; 1] = [Store];
pub fn load() -> Vec<String> { Vec::new() }
pub fn grid() -> Grid { Vec::new() }
`,
		"tests/kinds.rs", `use acme::{grid, load, Grid, Store, FRAMES, STORES};

#[test]
fn index() { let g = FRAMES[0]; g.len(); }

#[test]
fn written() { let mut v: Vec<String> = load(); v.push(String::new()); }

#[test]
fn alias() { let g: Grid = grid(); g.len(); }

#[test]
fn looped() { for g in FRAMES.iter() { g.len(); } }

#[test]
fn constant() { FRAMES.len(); }

#[test]
fn tuple() { let (a, b) = (1, "x"); b.len(); a.pow(2); }

#[test]
fn element_of_the_crate() { for s in STORES.iter() { s.add(1); } }

#[test]
fn built() { let s = Store; let t: &Store = &s; t.add(1); }

#[test]
fn unknown() { let v = load(); v.len(); }

#[test]
fn operator() { let n = load() + load(); n.len(); }
`), `alias calls=[src.grid] unresolved=0 reads=0
built calls=[src.Store.add] unresolved=0 reads=1
constant calls=[] unresolved=0 reads=1
element_of_the_crate calls=[src.Store.add] unresolved=0 reads=1
index calls=[] unresolved=0 reads=1
looped calls=[] unresolved=0 reads=1
operator calls=[src.load] unresolved=1 reads=0
tuple calls=[] unresolved=0 reads=0
unknown calls=[src.load] unresolved=1 reads=0
written calls=[src.load] unresolved=0 reads=0`)
}

// viewpoint: behaviour
// target: the Java and Kotlin readers
// As in TypeScript, a method of a local whose declared type, value, element or field is outside
// the product counts as nothing, and a property whose method counts as nothing is read; one of the
// product's class is that class's method; anything else is unresolved.
func TestJvmKindsOfValuesDecideHowTheirMethodsCount(t *testing.T) {
	t.Run("java", func(t *testing.T) {
		expect(t, kinds(t, "java",
			"src/main/java/acme/Store.java", "package acme;\n\npublic class Store {\n    public int add(int n) { return n; }\n}\n",
			"src/main/java/acme/Data.java", `package acme;

import java.util.List;

public final class Data {
    public static final List<String> FRAMES = List.of("a.", ".a");
    public static final List<Store> STORES = List.of(new Store());
    public static Object load() { return null; }
}
`,
			"src/test/java/acme/KindsTest.java", `package acme;

import java.util.List;
import org.junit.jupiter.api.Test;

class KindsTest {
    @Test
    void element() { var g = Data.FRAMES.get(0); g.length(); }

    @Test
    void looped() { for (var g : Data.FRAMES) { g.length(); } }

    @Test
    void ofTheProduct() { for (var s : Data.STORES) { s.add(1); } }

    @Test
    void cast() { var s = (Store) Data.load(); s.add(1); }

    @Test
    void unknown() { var o = Data.load(); o.hashCode(); }

    @Test
    void statics() { var words = List.of("a"); words.contains("a"); }
}
`), `KindsTest.cast calls=[acme.Data.load acme.Store.add] unresolved=0 reads=0
KindsTest.element calls=[] unresolved=0 reads=1
KindsTest.looped calls=[] unresolved=0 reads=1
KindsTest.ofTheProduct calls=[acme.Store.add] unresolved=0 reads=1
KindsTest.statics calls=[] unresolved=0 reads=0
KindsTest.unknown calls=[acme.Data.load] unresolved=1 reads=0`)
	})
	t.Run("kotlin", func(t *testing.T) {
		expect(t, kinds(t, "kotlin",
			"src/main/kotlin/acme/Store.kt", "package acme\n\nclass Store {\n    fun add(n: Int): Int = n\n}\n",
			"src/main/kotlin/acme/Data.kt", `package acme

val FRAMES: List<String> = listOf("a.", ".a")
val NAMES = listOf("a", "b")
val STORES: List<Store> = listOf(Store())
val SHARED: Store? = null

object Names {
    val ALL = listOf("x")
}

fun load(): Any = 1
`,
			"src/test/kotlin/acme/KindsTest.kt", `package acme

import org.junit.jupiter.api.Test

class KindsTest {
    @Test
    fun index() { val g = FRAMES[0]; g.count { it == 'a' } }

    @Test
    fun looped() { for (g in FRAMES) { g.count { it == 'a' } } }

    @Test
    fun inferred() { for (n in NAMES) { n.trim() } }

    @Test
    fun objectProperty() { val n = Names.ALL.first(); n.trim() }

    @Test
    fun ofTheProduct() { for (s in STORES) { s.add(1) } }

    @Test
    fun typed() { SHARED.add(1) }

    @Test
    fun outside() { FRAMES.first() }

    @Test
    fun cast() { val s = load() as Store; s.add(1) }

    @Test
    fun unknown() { val o = load(); o.hashCode() }
}
`), `KindsTest.cast calls=[acme.Store.add acme.load] unresolved=0 reads=0
KindsTest.index calls=[] unresolved=0 reads=1
KindsTest.inferred calls=[] unresolved=0 reads=1
KindsTest.looped calls=[] unresolved=0 reads=1
KindsTest.objectProperty calls=[] unresolved=0 reads=1
KindsTest.ofTheProduct calls=[acme.Store.add] unresolved=0 reads=1
KindsTest.outside calls=[] unresolved=0 reads=1
KindsTest.typed calls=[acme.Store.add] unresolved=0 reads=0
KindsTest.unknown calls=[acme.load] unresolved=1 reads=0`)
	})
}

// throughOf returns the calls a symbol makes through a value's type, or "no symbol".
func throughOf(a *facts.Analysis, id string) string {
	for _, s := range a.Symbols {
		if s.ID == id {
			return fmt.Sprintf("%v", s.Through)
		}
	}
	return "no symbol"
}

// viewpoint: behaviour
// target: typescript.trace, python.trace
// An element taken out of a container whose written type names one of the repository's types as
// its elements is a value of that type for the call diagram, as it is for tests: a method called on
// it is a call through that type. The container may be a parameter, a local, a field, or a value at
// the top of the file or imported into it. Keys taken out by for … in, and the elements of a
// container of outside values, are not.
func TestElementsOfContainersOfTheRepositorysTypesAreCallsThroughThem(t *testing.T) {
	ts := analyze(t, "typescript", tree(
		"package.json", `{"name":"app"}`,
		"src/repo/r.ts", "export interface Port { find(id: string): string }\nexport const PORTS: Port[] = []\n",
		"src/api/a.ts", `import { Port, PORTS } from '../repo/r'

const LOCAL: Port[] = []
export function param(ports: Port[]) { for (const p of ports) p.find('x') }
export function local() { const ports: Array<Port> = []; const p = ports[0]; p.find('x') }
export function top() { for (const p of LOCAL) p.find('x') }
export function imported() { for (const p of PORTS) p.find('x') }
export function keys(ports: Port[]) { for (const k in ports) k.trim() }
export function names(xs: string[]) { for (const x of xs) x.trim() }
export class Api {
  constructor(private ports: readonly Port[]) {}
  field() { for (const p of this.ports) p.find('x') }
}
`))
	py := analyze(t, "python", tree(
		"app/__init__.py", "",
		"app/repo.py", "from typing import Protocol\n\nclass Port(Protocol):\n    def find(self, i: str) -> str: ...\n\nPORTS: list[Port] = []\n",
		"app/api.py", `from typing import Sequence
from app.repo import Port, PORTS

LOCAL: list[Port] = []

def param(ports: Sequence[Port]):
    for p in ports:
        p.find("x")

def index(ports: list[Port]):
    p = ports[0]
    p.find("x")

def top():
    for p in LOCAL:
        p.find("x")

def imported():
    for p in PORTS:
        p.find("x")

def names(xs: list[str]):
    for x in xs:
        x.strip()

class Api:
    def __init__(self, ports: list[Port]):
        self.ports = ports

    def attribute(self):
        for p in self.ports:
            p.find("x")
`))
	for _, c := range []struct {
		a    *facts.Analysis
		id   string
		want string
	}{
		{ts, "src/api.param", "[src/repo.Port.find]"},
		{ts, "src/api.local", "[src/repo.Port.find]"},
		{ts, "src/api.top", "[src/repo.Port.find]"},
		{ts, "src/api.imported", "[src/repo.Port.find]"},
		{ts, "src/api.keys", "[]"},
		{ts, "src/api.names", "[]"},
		{ts, "src/api.Api.field", "[src/repo.Port.find]"},
		{py, "app/api.param", "[app/repo.Port.find]"},
		{py, "app/api.index", "[app/repo.Port.find]"},
		{py, "app/api.top", "[app/repo.Port.find]"},
		{py, "app/api.imported", "[app/repo.Port.find]"},
		{py, "app/api.names", "[]"},
		{py, "app/api.Api.attribute", "[app/repo.Port.find]"},
	} {
		if got := throughOf(c.a, c.id); got != c.want {
			t.Errorf("%s: got %s, want %s", c.id, got, c.want)
		}
	}
}
