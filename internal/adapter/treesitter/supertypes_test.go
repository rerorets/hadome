// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

//go:build cgo

package treesitter

import (
	"fmt"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/facts"
)

// callFacts writes a symbol's facts for the call diagram.
func callFacts(t *testing.T, a *facts.Analysis, id string) string {
	t.Helper()
	for _, s := range a.Symbols {
		if s.ID == id {
			return fmt.Sprintf("through=%v supertypes=%v", s.Through, s.Supertypes)
		}
	}
	t.Fatalf("no symbol %s", id)
	return ""
}

// viewpoint: behaviour
// target: java.trace
// A Java type's supertypes are the repository's types it names after extends and implements,
// generic or qualified, resolved by its imports and package; a library's type is left out.
func TestJavaSupertypes(t *testing.T) {
	const src = "src/main/java/com/ex/"
	a := analyze(t, "java", fstest.MapFS{
		src + "port/Finder.java": {Data: []byte("package com.ex.port;\n\npublic interface Finder<T> {}\n")},
		src + "port/Store.java":  {Data: []byte("package com.ex.port;\n\npublic interface Store extends Finder<String> {}\n")},
		src + "base/Base.java":   {Data: []byte("package com.ex.base;\n\npublic abstract class Base {}\n")},
		src + "svc/Svc.java": {Data: []byte("package com.ex.svc;\n\nimport com.ex.port.Store;\nimport java.io.Serializable;\n\n" +
			"public class Svc extends com.ex.base.Base implements Store, Serializable {}\n")},
	})
	if got := callFacts(t, a, "com.ex.port.Store"); got != "through=[] supertypes=[com.ex.port.Finder]" {
		t.Errorf("Store: %s", got)
	}
	if got := callFacts(t, a, "com.ex.svc.Svc"); got != "through=[] supertypes=[com.ex.port.Store]" {
		t.Errorf("Svc: %s", got)
	}
}

// viewpoint: behaviour
// target: kotlin.trace
// A Kotlin class's supertypes are the repository's types after the colon, called as a constructor
// or not, and delegated or not.
func TestKotlinSupertypes(t *testing.T) {
	const src = "src/main/kotlin/com/ex/"
	a := analyze(t, "kotlin", fstest.MapFS{
		src + "port/Port.kt": {Data: []byte("package com.ex.port\n\ninterface Finder\ninterface Saver\nopen class Base\n")},
		src + "svc/Svc.kt": {Data: []byte("package com.ex.svc\n\nimport com.ex.port.Base\nimport com.ex.port.Finder\nimport com.ex.port.Saver\n\n" +
			"class Svc(s: Saver) : Base(), Finder, Saver by s\n")},
	})
	if got := callFacts(t, a, "com.ex.svc.Svc"); got != "through=[] supertypes=[com.ex.port.Base com.ex.port.Finder com.ex.port.Saver]" {
		t.Errorf("Svc: %s", got)
	}
}

// viewpoint: behaviour
// target: typescript.trace
// TypeScript's calls through a value's declared type are the methods called on fields, constructor
// parameter properties, parameters and locals whose written type is one of the repository's types,
// interface or class, imported type only or not; an unannotated value and a library's type give
// none. Supertypes are what a class extends and implements, and what an interface extends.
func TestTypeScriptThroughAndSupertypes(t *testing.T) {
	a := analyze(t, "typescript", fstest.MapFS{
		"package.json":  {Data: []byte(`{"name":"app"}`)},
		"src/port/p.ts": {Data: []byte("export interface Finder { find(id: string): string }\nexport interface Store extends Finder {}\n")},
		"src/svc/s.ts": {Data: []byte("import type * as port from '../port/p'\n\nexport class Base {}\n" +
			"export class Svc extends Base implements port.Store { find(id: string): string { return id } }\n")},
		"src/api/c.ts": {Data: []byte("import type { Finder, Store } from '../port/p'\nimport { Svc } from '../svc/s'\nimport { Logger } from 'pino'\n\n" +
			"export class Api {\n  private cache: Finder | null = null\n  constructor(private finder: Finder, svc: Svc, private log: Logger, private any, readonly owned: Svc) {}\n" +
			"  show(other: Store, plain): string {\n    const local: Svc = new Svc()\n    this.log.info('x')\n    plain.find('x')\n" +
			"    return this.finder.find('a') + this.cache!.find('b') + other.find('c') + local.find('d') + this.any.find('e') + this.owned.save()\n  }\n}\n")},
	})
	if got := callFacts(t, a, "src/api.Api.show"); got != "through=[src/port.Finder.find src/port.Store.find src/svc.Svc src/svc.Svc.find src/svc.Svc.save] supertypes=[]" {
		t.Errorf("Api.show: %s", got)
	}
	if got := callFacts(t, a, "src/svc.Svc"); got != "through=[] supertypes=[src/port.Store src/svc.Base]" {
		t.Errorf("Svc: %s", got)
	}
	if got := callFacts(t, a, "src/port.Store"); got != "through=[] supertypes=[src/port.Finder]" {
		t.Errorf("Store: %s", got)
	}
}

// viewpoint: behaviour
// target: typescript.trace
// An array of one of the repository's types, written T[] or T<U>[], and a member's type T["key"]
// are not the type: their methods are not the type's.
func TestTypeScriptArraysAreNotTheirElementType(t *testing.T) {
	a := analyze(t, "typescript", fstest.MapFS{
		"package.json": {Data: []byte(`{"name":"app"}`)},
		"src/svc/s.ts": {Data: []byte("export class Svc<T = number> { map(): void {} filter(): void {} call(): void {} find(): void {} }\n")},
		"src/api/c.ts": {Data: []byte("import { Svc } from '../svc/s'\n\n" +
			"export function show(all: Svc[], typed: Svc<string>[] | null, key: Svc[\"call\"], one: Svc): void {\n" +
			"  all.map()\n  typed!.filter()\n  key.call()\n  one.find()\n}\n")},
	})
	if got := callFacts(t, a, "src/api.show"); got != "through=[src/svc.Svc.find] supertypes=[]" {
		t.Errorf("show: %s", got)
	}
}

// viewpoint: behaviour
// target: rust.trace
// A Rust type's supertypes are the traits it has an impl of, and a trait's are the traits it
// requires. Calls through a trait are the methods called on fields of Box, Arc or Rc of dyn Trait
// and on parameters bound by a trait, in the type parameters or a where clause; those on &dyn and
// impl parameters are already among the calls.
func TestRustThroughAndSupertypes(t *testing.T) {
	a := analyze(t, "rust", tree(
		"Cargo.toml", "[package]\nname = \"app\"\n",
		"src/lib.rs", "pub mod port;\npub mod svc;\n",
		"src/port/mod.rs", "pub trait Named { fn name(&self) -> String; }\n"+
			"pub trait Finder: Named + Send { fn find(&self, id: &str) -> String; }\n"+
			"pub struct Api { finder: Box<dyn Finder>, shared: std::sync::Arc<dyn Finder + Sync> }\n\n"+
			"impl Api {\n    pub fn show(&self) -> String { self.finder.find(\"a\") + &self.shared.name() }\n}\n\n"+
			"pub fn bound<T: Finder>(f: &T) -> String { f.find(\"b\") }\n"+
			"pub fn clause<T>(f: &mut T) -> String where T: Named { f.name() }\n"+
			"pub fn plain(f: &dyn Finder) -> String { f.find(\"c\") }\n",
		"src/svc/mod.rs", "use crate::port::{Finder, Named};\n\npub struct Svc;\n\n"+
			"impl Finder for Svc { fn find(&self, id: &str) -> String { id.into() } }\n"+
			"impl Named for Svc { fn name(&self) -> String { String::new() } }\n"+
			"impl Svc { pub fn new() -> Self { Svc } }\n",
	))
	cases := map[string]string{
		"src/port.Api.show": "through=[src/port.Finder.find src/port.Finder.name] supertypes=[]",
		"src/port.bound":    "through=[src/port.Finder.find] supertypes=[]",
		"src/port.clause":   "through=[src/port.Named.name] supertypes=[]",
		"src/port.plain":    "through=[] supertypes=[]",
		"src/port.Finder":   "through=[] supertypes=[src/port.Named]",
		"src/svc.Svc":       "through=[] supertypes=[src/port.Finder src/port.Named]",
	}
	for id, want := range cases {
		if got := callFacts(t, a, id); got != want {
			t.Errorf("%s:\n got  %s\n want %s", id, got, want)
		}
	}
}

// viewpoint: behaviour
// target: python.trace
// A Python class's supertypes are the repository's classes it names as bases, and the Protocols
// whose methods it has by name and number of parameters, inherited ones included. Calls through a
// class are the methods called on annotated parameters, on attributes the class annotates, and on
// attributes __init__ sets from an annotated parameter; an unannotated one gives none.
func TestPythonThroughAndSupertypes(t *testing.T) {
	a := analyze(t, "python", tree(
		"app/__init__.py", "",
		"app/port.py", "from typing import Protocol, Optional\n\n\nclass Finder(Protocol):\n    def find(self, id: str) -> str: ...\n\n\n"+
			"class Saver:\n    def save(self, x) -> None: ...\n\n\n"+
			"class Api:\n    cache: \"Finder\"\n\n    def __init__(self, finder: Finder, saver: Optional[Saver], repo) -> None:\n"+
			"        self.finder = finder\n        self.saver = saver\n        self.repo = repo\n\n"+
			"    def show(self, other: Saver | None) -> str:\n        self.repo.load()\n        self.saver.save(1)\n"+
			"        other.save(2)\n        return self.finder.find('a')\n\n"+
			"    def cached(self):\n        return self.cache.find('c')\n",
		"app/svc.py", "from app import port\nfrom app.port import Saver\n\n\nclass Base:\n    def find(self, id):\n        return id\n\n\n"+
			"class Svc(Base):\n    pass\n\n\nclass Store(port.Saver):\n    def save(self, x) -> None:\n        pass\n\n\n"+
			"class Wrong:\n    def find(self, id, extra):\n        return id\n",
	))
	cases := map[string]string{
		"app/port.Api.show":   "through=[app/port.Finder.find app/port.Saver.save] supertypes=[]",
		"app/port.Api.cached": "through=[app/port.Finder.find] supertypes=[]",
		"app/svc.Svc":         "through=[] supertypes=[app/port.Finder app/svc.Base]",
		"app/svc.Base":        "through=[] supertypes=[app/port.Finder]",
		"app/svc.Store":       "through=[] supertypes=[app/port.Saver]",
		"app/svc.Wrong":       "through=[] supertypes=[]",
	}
	for id, want := range cases {
		if got := callFacts(t, a, id); got != want {
			t.Errorf("%s:\n got  %s\n want %s", id, got, want)
		}
	}
}
