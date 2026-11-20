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
// target: the Rust reader
// A local's written type decides what it is, as in TypeScript: a type of the language, of the
// standard library by its path, or brought in from another crate is outside, as is a container of
// them and its elements; a reference, a Box, an Rc or a dyn of a type is that type; one of the
// crate's structs, traits or generic structs is that type; an alias of the crate is what it names,
// whatever its module brings in; a container of the crate's type is outside, its elements of that
// type; a name nothing declares, a path into another module of the crate and an alias that names
// itself say nothing, so the value decides, here unknown.
func TestRustWrittenTypesDecideWhatALocalIs(t *testing.T) {
	types := [][2]string{{"string", "String"}, {"int", "i32"}, {"store", "Store"}, {"by_ref", "&Store"},
		{"dyn_port", "&dyn Port"}, {"boxed", "Box<Store>"}, {"rc_dyn", "Rc<dyn Port>"}, {"option", "Option<Store>"},
		{"vec", "Vec<Store>"}, {"array", "[Store; 2]"}, {"slice", "&[Store]"}, {"mixed_tuple", "(i32, Store)"},
		{"tuple", "(i32, String)"}, {"map_store", "HashMap<String, Store>"}, {"map_int", "HashMap<String, i32>"},
		{"cow", "Cow<'static, str>"}, {"value", "Value"}, {"path", "std::path::PathBuf"}, {"stores", "Stores"},
		{"grid", "Grid"}, {"looped", "Loop"}, {"wrap", "Wrap<Store>"}, {"missing", "Missing"},
		{"crate_path", "crate::store::Store"}}
	var b strings.Builder
	for _, nt := range types {
		fmt.Fprintf(&b, "\n#[test]\nfn %s() { let x: %s = load(); x.add(1); }\n", nt[0], nt[1])
		fmt.Fprintf(&b, "\n#[test]\nfn %s_element() { let x: %s = load(); for e in x { e.add(1); } }\n", nt[0], nt[1])
	}
	got := kinds(t, "rust",
		"Cargo.toml", "[package]\nname = \"acme\"\n",
		"src/store.rs", "pub struct Store;\nimpl Store { pub fn add(&self, n: i32) -> i32 { n } }\n",
		"src/lib.rs", `pub mod store;
use crate::store::Store;
pub type Stores = Vec<Store>;
pub type Grid = Vec<Vec<String>>;
pub type Loop = Again;
pub type Again = Loop;
pub trait Port { fn add(&self, n: i32) -> i32; }
pub struct Wrap<T>(T);
impl<T> Wrap<T> { pub fn add(&self, n: i32) -> i32 { n } }
`,
		"tests/types.rs", `use std::borrow::Cow;
use std::collections::HashMap;
use std::rc::Rc;
use serde_json::Value;
use acme::{Again, Grid, Loop, Port, Stores, Wrap};
use acme::store::Store;

fn load<T>() -> T { unimplemented!() }
`+b.String())
	expectEach(t, got, map[string]string{
		"string": "calls=[] unresolved=0", "string_element": "calls=[] unresolved=0", "int": "calls=[] unresolved=0",
		"store": "calls=[src/store.Store.add] unresolved=0", "store_element": "calls=[] unresolved=1",
		"by_ref": "calls=[src/store.Store.add] unresolved=0", "dyn_port": "calls=[src.Port.add] unresolved=0",
		"boxed": "calls=[src/store.Store.add] unresolved=0", "rc_dyn": "calls=[src.Port.add] unresolved=0",
		"option": "calls=[] unresolved=0", "option_element": "calls=[src/store.Store.add] unresolved=0",
		"vec": "calls=[] unresolved=0", "vec_element": "calls=[src/store.Store.add] unresolved=0",
		"array_element": "calls=[src/store.Store.add] unresolved=0", "slice_element": "calls=[src/store.Store.add] unresolved=0",
		"mixed_tuple": "calls=[] unresolved=0", "mixed_tuple_element": "calls=[] unresolved=1",
		"tuple_element": "calls=[] unresolved=0",
		"map_store":     "calls=[] unresolved=0", "map_store_element": "calls=[] unresolved=1",
		"map_int_element": "calls=[] unresolved=0", "cow": "calls=[] unresolved=0", "cow_element": "calls=[] unresolved=0",
		"value": "calls=[] unresolved=0", "path": "calls=[] unresolved=0",
		"stores": "calls=[] unresolved=0", "stores_element": "calls=[src/store.Store.add] unresolved=0",
		"grid_element": "calls=[] unresolved=0", "looped": "calls=[] unresolved=1",
		"wrap": "calls=[src.Wrap.add] unresolved=0", "missing": "calls=[] unresolved=1",
		"crate_path": "calls=[] unresolved=1",
	})
}

// viewpoint: behaviour
// target: the Rust reader
// What a local's value makes it, as in TypeScript: a reference to a value is that value, literals,
// vec!, format! and an operation on outside values are outside, a comparison is a bool whatever it
// compares, an arithmetic operation on values of unknown kind may be a trait of their type, and an
// iterator over a container takes out its elements as they are.
func TestRustValuesDecideWhatALocalIs(t *testing.T) {
	expect(t, kinds(t, "rust",
		"Cargo.toml", "[package]\nname = \"acme\"\n",
		"src/lib.rs", "pub struct Store;\nimpl Store { pub fn add(&self, n: i32) -> i32 { n } }\npub const STORE: Store = Store;\n",
		"tests/values.rs", `use acme::{Store, STORE};

fn load<T>() -> T { unimplemented!() }

#[test]
fn borrowed() { let s = Store; let u = &s; u.add(1); }

#[test]
fn sum() { let n = 1 + 2; n.pow(2); }

#[test]
fn compared() { let b = load::<i32>() == load::<i32>(); b.then(|| 1); }

#[test]
fn added() { let n = load::<i32>() + load::<i32>(); n.pow(2); }

#[test]
fn vector() { let v = vec![1, 2]; v.len(); }

#[test]
fn formatted() { let s = format!("{}", 1); s.len(); }

#[test]
fn iterated() { let v: Vec<Store> = load(); for e in v.iter() { e.add(1); } }

#[test]
fn consumed() { let v: Vec<Store> = load(); for e in v.into_iter() { e.add(1); } }

#[test]
fn constant() { STORE.add(1); }
`), `added calls=[] unresolved=1 reads=0
borrowed calls=[src.Store.add] unresolved=0 reads=1
compared calls=[] unresolved=0 reads=0
constant calls=[src.Store.add] unresolved=0 reads=0
consumed calls=[src.Store.add] unresolved=0 reads=0
formatted calls=[] unresolved=0 reads=0
iterated calls=[src.Store.add] unresolved=0 reads=0
sum calls=[] unresolved=0 reads=0
vector calls=[] unresolved=0 reads=0`)
}
