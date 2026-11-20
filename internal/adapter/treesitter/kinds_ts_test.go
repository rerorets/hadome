// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

//go:build cgo

package treesitter

import (
	"fmt"
	"strings"
	"testing"
)

// typedTests writes, for each written type, a test that calls a method on a local of that type and
// one that calls a method on an element taken out of it by a loop. declare spells the local of a
// type with a value of unknown kind, and loop spells the loop over it.
func typedTests(types []string, declare, loop func(t string) string) string {
	var b strings.Builder
	for _, t := range types {
		fmt.Fprintf(&b, "%s\n%s\n", declare(t), loop(t))
	}
	return b.String()
}

// viewpoint: behaviour
// target: the TypeScript reader
// A local's written type decides what it is: a type of the language, a literal type or a type of an
// external library is outside, as is a container of them and its elements; one of the repository's
// classes, interfaces or generic classes is that type; an alias of the repository is what it names;
// a union is its one member that holds a value, outside when all its members are, else unknown; a
// container of the repository's type is outside, its elements of that type; a container holding
// the repository's types otherwise is outside, its elements unknown; any, unknown, object, a name
// nothing declares and an alias that names itself say nothing, so the value decides, here unknown.
func TestTypeScriptWrittenTypesDecideWhatALocalIs(t *testing.T) {
	types := []string{"string", "any", "unknown", "object", "'a' | 'b'", "(s: string) => void", "Store", "Store | null",
		"Store | undefined | null", "string | number", "string | Store", "Store[]", "Array<Store>", "ReadonlyArray<string>",
		"readonly Store[]", "(Store)", "Map<string, Store>", "Map<string, number>", "[string, number]", "[string, Store]",
		"[string, ...number[]]", "[string, number?]", "Box<Store>", "Promise<Store>", "st.Store", "ext.Thing", "Thing",
		"Grid", "Stores", "Color", "Port", "Missing", "Local", "Cycle", "nope.Store"}
	test := typedTests(types,
		func(t string) string {
			return fmt.Sprintf("test(\"%s\", () => { const x: %s = load(); x.add(1) })", t, t)
		},
		func(t string) string {
			return fmt.Sprintf("test(\"%s element\", () => { const x: %s = load(); for (const e of x) e.add(1) })", t, t)
		})
	got := kinds(t, "typescript",
		"src/store.ts", `export class Store { add(n: number) { return n } }
export class Box<T> { add(n: number) { return n } }
export interface Port { add(n: number): number }
export enum Color { Red }
export type Grid = string[][]
export type Stores = Store[]
`,
		"src/store.test.ts", `import { Store, Box, Port, Color, Grid, Stores } from './store'
import * as st from './store'
import * as ext from 'ext-lib'
import { Thing } from 'ext-lib'
type Local = Store[]
type Cycle = Again
type Again = Cycle
function load(): any { return null }
`+test)
	want := map[string]string{
		"string": "calls=[] unresolved=0", "string element": "calls=[] unresolved=0",
		"any": "calls=[] unresolved=1", "any element": "calls=[] unresolved=1",
		"unknown": "calls=[] unresolved=1", "object": "calls=[] unresolved=1",
		"'a' | 'b'": "calls=[] unresolved=0", "'a' | 'b' element": "calls=[] unresolved=0",
		"(s: string) => void": "calls=[] unresolved=0",
		"Store":               "calls=[src.Store.add] unresolved=0", "Store element": "calls=[] unresolved=1",
		"Store | null": "calls=[src.Store.add] unresolved=0", "Store | undefined | null": "calls=[src.Store.add] unresolved=0",
		"string | number": "calls=[] unresolved=0", "string | number element": "calls=[] unresolved=0",
		"string | Store": "calls=[] unresolved=1", "string | Store element": "calls=[] unresolved=1",
		"Store[]": "calls=[] unresolved=0", "Store[] element": "calls=[src.Store.add] unresolved=0",
		"Array<Store>": "calls=[] unresolved=0", "Array<Store> element": "calls=[src.Store.add] unresolved=0",
		"ReadonlyArray<string> element": "calls=[] unresolved=0",
		"readonly Store[] element":      "calls=[src.Store.add] unresolved=0",
		"(Store)":                       "calls=[src.Store.add] unresolved=0",
		"Map<string, Store>":            "calls=[] unresolved=0", "Map<string, Store> element": "calls=[] unresolved=1",
		"Map<string, number> element": "calls=[] unresolved=0",
		"[string, number] element":    "calls=[] unresolved=0", "[string, Store] element": "calls=[] unresolved=1",
		"[string, ...number[]] element": "calls=[] unresolved=0", "[string, number?] element": "calls=[] unresolved=0",
		"Box<Store>":     "calls=[src.Box.add] unresolved=0",
		"Promise<Store>": "calls=[] unresolved=0", "Promise<Store> element": "calls=[] unresolved=1",
		"st.Store": "calls=[src.Store.add] unresolved=0", "nope.Store": "calls=[] unresolved=1",
		"ext.Thing": "calls=[] unresolved=0", "ext.Thing element": "calls=[] unresolved=0",
		"Thing": "calls=[] unresolved=0", "Thing element": "calls=[] unresolved=0",
		"Grid": "calls=[] unresolved=0", "Grid element": "calls=[] unresolved=0",
		"Stores": "calls=[] unresolved=0", "Stores element": "calls=[src.Store.add] unresolved=0",
		"Color": "calls=[] unresolved=0", "Port": "calls=[src.Port.add] unresolved=0",
		"Missing": "calls=[] unresolved=1", "Missing element": "calls=[] unresolved=1",
		"Local element": "calls=[src.Store.add] unresolved=0", "Cycle": "calls=[] unresolved=1",
	}
	expectEach(t, got, want)
}

// expectEach checks the lines of got, one per test named first, against what want says of the
// tests it names.
func expectEach(t *testing.T, got string, want map[string]string) {
	t.Helper()
	seen := map[string]bool{}
	for _, line := range strings.Split(got, "\n") {
		for name, w := range want {
			if strings.HasPrefix(line, name+" calls=") {
				seen[name] = true
				if !strings.HasPrefix(line, name+" "+w) {
					t.Errorf("%s: got %q, want %q", name, strings.TrimPrefix(line, name+" "), w)
				}
			}
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("%s: no such test in\n%s", name, got)
		}
	}
}

// viewpoint: behaviour
// target: the TypeScript reader
// A variable imported from the repository is what its declaration makes it: its written type, else
// its value read without running anything, where a cast names its type, as const keeps the value,
// another variable or an external import gives its kind, a literal is outside and holds what it
// names, and new makes a built-in, external or repository class. A variable passed on by another
// file's export is read where it is declared. Variables that name each other,
// and a class nothing declares, stay unknown: their methods are named after them, as before.
func TestTypeScriptImportedVariablesAreWhatTheirDeclarationsMake(t *testing.T) {
	names := []string{"CAST", "CONSTED", "ALIAS", "EXT", "MAP", "MADE", "LOOP_A", "HOLDS", "UNKNOWN_NEW", "TYPED", "VIA"}
	test := typedTests(names,
		func(n string) string { return fmt.Sprintf("test('%s', () => { %s.add(1) })", n, n) },
		func(n string) string {
			return fmt.Sprintf("test('%s element', () => { for (const e of %s) e.add(1) })", n, n)
		})
	got := kinds(t, "typescript",
		"src/store.ts", "export class Store { add(n: number) { return n } }\n",
		"src/data.ts", `import { Store } from './store'
import { z } from 'zod'
function load(): any { return null }
export const NAMES = ['a']
export const CAST = load() as Store
export const CONSTED = ['a'] as const
export const ALIAS = NAMES
export const EXT = z
export const MAP = new Map<string, number>()
export const MADE = new Store()
export const LOOP_A = LOOP_B
export const LOOP_B = LOOP_A
export const HOLDS = [1, MADE]
export const UNKNOWN_NEW = new Missing()
export const TYPED: Store[] = []
`,
		"src/index.ts", "export { TYPED as VIA } from './data'\n",
		"src/data.test.ts", "import { "+strings.Join(names[:len(names)-1], ", ")+" } from './data'\nimport { VIA } from './index'\n"+test)
	expectEach(t, got, map[string]string{
		"CAST": "calls=[src.Store.add] unresolved=0 reads=0", "CAST element": "calls=[] unresolved=1",
		"CONSTED": "calls=[] unresolved=0 reads=1", "CONSTED element": "calls=[] unresolved=0",
		"ALIAS": "calls=[] unresolved=0 reads=1", "ALIAS element": "calls=[] unresolved=0",
		"EXT": "calls=[] unresolved=0 reads=1",
		"MAP": "calls=[] unresolved=0 reads=1", "MAP element": "calls=[] unresolved=0",
		"MADE":   "calls=[src.Store.add] unresolved=0 reads=0",
		"LOOP_A": "calls=[src.LOOP_A.add] unresolved=0", "LOOP_A element": "calls=[] unresolved=1",
		"HOLDS": "calls=[] unresolved=0 reads=1", "HOLDS element": "calls=[] unresolved=1",
		"UNKNOWN_NEW": "calls=[src.UNKNOWN_NEW.add] unresolved=0",
		"TYPED":       "calls=[] unresolved=0 reads=1", "TYPED element": "calls=[src.Store.add] unresolved=0",
		"VIA element": "calls=[src.Store.add] unresolved=0",
	})
}
