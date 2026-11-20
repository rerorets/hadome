// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

//go:build cgo

package deps

import "github.com/rerorets/hadome/internal/engine"

func init() {
	languageCycleCases = []engine.Case{
		{Name: "a type-only import closing a cycle passes",
			Base: tree("package.json", `{"name":"app"}`, "src/ui/a.ts", "import { b } from '../lib/b';\nexport const a = () => b();\n",
				"src/lib/b.ts", "export type Row = { id: number };\nexport const b = () => 1;\n"),
			Head: tree("package.json", `{"name":"app"}`, "src/ui/a.ts", "import { b } from '../lib/b';\nexport const a = () => b();\n",
				"src/lib/b.ts", "import type { A } from '../ui/a';\nexport type Row = { id: number; a?: A };\nexport const b = () => 1;\n")},
		{Name: "an import under Python's TYPE_CHECKING closing a cycle passes",
			Base: tree("ui/__init__.py", "", "ui/view.py", "from lib.rows import Row\n\ndef show(r: Row): return r\n",
				"lib/__init__.py", "", "lib/rows.py", "class Row: pass\n"),
			Head: tree("ui/__init__.py", "", "ui/view.py", "from lib.rows import Row\n\ndef show(r: Row): return r\n",
				"lib/__init__.py", "", "lib/rows.py", "from typing import TYPE_CHECKING\nif TYPE_CHECKING:\n    from ui.view import show\n\nclass Row: pass\n")},
		{Name: "a cycle between a TypeScript parent and child fails",
			Base: tree("package.json", `{"name":"app"}`, "src/index.ts", "export { x } from './a/x';\n", "src/a/x.ts", "export const x = 1;\n"),
			Head: tree("package.json", `{"name":"app"}`, "src/index.ts", "export { x } from './a/x';\nexport const y = 2;\n",
				"src/a/x.ts", "import { y } from '../index';\nexport const x = y;\n"),
			WantFail: []string{"deps/cycle"}},
		{Name: "a cycle between a Rust crate root and its module passes",
			Base: tree("Cargo.toml", "[package]\nname = \"app\"\n", "src/lib.rs", "mod store;\npub struct Config;\n",
				"src/store.rs", "pub struct Row;\n"),
			Head: tree("Cargo.toml", "[package]\nname = \"app\"\n", "src/lib.rs", "mod store;\nuse crate::store::Row;\npub struct Config;\n",
				"src/store.rs", "use crate::Config;\npub struct Row;\n")},
		{Name: "a cycle between a Kotlin package and its subpackage passes",
			Base: tree("src/main/kotlin/acme/App.kt", "package acme\n\nclass App\n", "src/main/kotlin/acme/store/Store.kt", "package acme.store\n\nclass Store\n"),
			Head: tree("src/main/kotlin/acme/App.kt", "package acme\n\nimport acme.store.Store\n\nclass App(val s: Store)\n",
				"src/main/kotlin/acme/store/Store.kt", "package acme.store\n\nimport acme.App\n\nclass Store(val a: App?)\n")},
	}
}
