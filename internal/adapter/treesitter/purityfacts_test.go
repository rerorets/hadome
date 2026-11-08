//go:build cgo

package treesitter

import (
	"slices"
	"strings"
	"testing"

	"github.com/rerorets/hadome/internal/facts"
)

// importLines renders the imports as "from -> to io=… global=…", sorted.
func importLines(a *facts.Analysis) string {
	var out []string
	for _, im := range a.Imports {
		out = append(out, im.FromUnit+" -> "+im.ToUnit+" internal="+tf(im.Internal)+" io="+tf(im.IO)+" global="+tf(im.Global))
	}
	slices.Sort(out)
	return strings.Join(out, "\n")
}

func tf(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// mocked reports whether the only test of the analysis has a mock hazard.
func mocked(t *testing.T, a *facts.Analysis) bool {
	t.Helper()
	if len(a.Tests) != 1 {
		t.Fatalf("want one test, got %d", len(a.Tests))
	}
	return slices.ContainsFunc(a.Tests[0].Hazards, func(h facts.Hazard) bool { return h.Category == facts.HazardMock })
}

// viewpoint: behaviour
// target: the TypeScript reader
// An import or export of types only is erased when TypeScript compiles, so it is no dependency:
// `import type`, `export type … from`, and named imports that are all marked type. An import that
// brings in one value, or a default, is.
func TestTypeOnlyImportsAreNoDependency(t *testing.T) {
	a := analyze(t, "typescript", tree(
		"src/lib/b.ts", "export type Row = { id: number };\nexport type Size = number;\nexport const b = 1;\nexport default b;\n",
		"src/ui/types.ts", "import type { Row } from '../lib/b';\nimport { type Size } from '../lib/b';\nexport type { Row as R } from '../lib/b';\nexport const n = (r: Row, s: Size) => [r, s];\n",
		"src/ui/value.ts", "import { type Row, b } from '../lib/b';\nexport const v = (r: Row) => [r, b];\n",
		"src/app/main.ts", "import b, { type Size } from '../lib/b';\nexport const m = (s: Size) => [s, b];\n",
	))
	expect(t, importLines(a), `
src/app -> src/lib internal=true io=false global=false
src/ui -> src/lib internal=true io=false global=false`)
	for _, im := range a.Imports {
		if im.Path == "src/ui/types.ts" {
			t.Errorf("a type-only import counts: %+v", im)
		}
	}
}

// viewpoint: behaviour
// target: the TypeScript reader
// A declaration file is never run, so nothing it imports is a dependency, a value import of a
// database client included. The same import in an ordinary file is one.
func TestDeclarationFileImportsAreNoDependency(t *testing.T) {
	a := analyze(t, "typescript", tree(
		"src/types/auth.d.ts", "import { Role } from 'pg';\nimport { b } from '../lib/b';\ndeclare module 'auth' { interface Session { role: Role } }\n",
		"src/types/env.d.mts", "import { Pool } from 'pg';\ndeclare global { var pool: Pool }\n",
		"src/ui/theme.d.css.ts", "import { Pool } from 'pg';\nexport declare const p: Pool;\n",
		"src/lib/b.ts", "export const b = 1;\n",
		"src/lib/db.ts", "import { Pool } from 'pg';\nexport const p = new Pool();\n",
	))
	expect(t, importLines(a), `
src/lib -> pg internal=false io=true global=false`)
}

// viewpoint: behaviour
// target: the TypeScript reader
// Code that calls fetch, opens a WebSocket or an XMLHttpRequest, beacons, or reads localStorage,
// sessionStorage or indexedDB does input or output without importing anything. Each API counts once
// per file, with or without window. or globalThis. in front; a method of the same name on another
// object does not count.
func TestGlobalInputOutputAPIsAreFacts(t *testing.T) {
	a := analyze(t, "typescript", tree(
		"src/api/load.ts", `export const load = async () => (await fetch('/rows')).status;
export const again = () => window.fetch('/rows');
export const keep = (v: string) => localStorage.setItem('k', v);
export const tab = () => window.sessionStorage.getItem('k');
export const db = () => indexedDB.open('d');
export const ws = () => new WebSocket('ws://x');
export const xhr = () => new XMLHttpRequest();
export const bye = () => navigator.sendBeacon('/b');
`,
		"src/calc/calc.ts", "export const get = (api: { fetch(): number }) => api.fetch();\nexport const storage = { localStorage: 1 };\n",
	))
	expect(t, importLines(a), `
src/api -> WebSocket internal=false io=true global=true
src/api -> XMLHttpRequest internal=false io=true global=true
src/api -> fetch internal=false io=true global=true
src/api -> indexedDB internal=false io=true global=true
src/api -> localStorage internal=false io=true global=true
src/api -> navigator.sendBeacon internal=false io=true global=true
src/api -> sessionStorage internal=false io=true global=true`)
}

// viewpoint: behaviour
// target: the TypeScript reader
// vi.spyOn, jest.spyOn and vi.stubGlobal replace behaviour the test does not own, as jest.mock does.
func TestSpiesAndStubbedGlobalsAreMocks(t *testing.T) {
	for _, call := range []string{"vi.spyOn(Math, 'max')", "jest.spyOn(Math, 'max')", "vi.stubGlobal('fetch', () => 1)"} {
		a := analyze(t, "typescript", tree("src/calc.ts", "export const add = (a: number, b: number) => a + b;\n",
			"src/calc.test.ts", "import { add } from './calc';\nit('adds', () => {\n  "+call+";\n  expect(add(1, 2)).toBe(3);\n});\n"))
		if !mocked(t, a) {
			t.Errorf("%s: want a mock hazard", call)
		}
	}
}

// viewpoint: behaviour
// target: the Python reader
// monkeypatch.setattr and monkeypatch.delattr replace code the test calls; monkeypatch.setenv
// does not, so only the first two are mocks.
func TestMonkeypatchingAnAttributeIsAMock(t *testing.T) {
	cases := map[string]bool{
		`monkeypatch.setattr("app.calc.limit", lambda: 1)`: true,
		`monkeypatch.delattr("app.calc.limit")`:            true,
		`monkeypatch.setenv("MODE", "x")`:                  false,
	}
	for call, want := range cases {
		a := analyze(t, "python", tree("app/calc.py", "def add(a, b):\n    return a + b\n",
			"tests/test_calc.py", "from app.calc import add\n\n\ndef test_add(monkeypatch):\n    "+call+"\n    assert add(1, 2) == 3\n"))
		if got := mocked(t, a); got != want {
			t.Errorf("%s: want mock %t, got %t", call, want, got)
		}
	}
}

// viewpoint: behaviour
// target: the Java reader
// Quarkus's @InjectMock and @InjectSpy put a mock in place of a bean, as Mockito's @Mock does.
func TestQuarkusInjectedMocksAreMocks(t *testing.T) {
	for _, ann := range []string{"InjectMock", "InjectSpy"} {
		a := analyze(t, "java", tree(
			"src/main/java/com/acme/calc/Calc.java", "package com.acme.calc;\n\n/** Calc adds. */\npublic class Calc {\n  /** Adds. */\n  public int add(int a, int b) { return a + b; }\n}\n",
			"src/test/java/com/acme/calc/CalcTest.java", "package com.acme.calc;\n\nimport org.junit.jupiter.api.Test;\n\nclass CalcTest {\n  @"+ann+"\n  Clock clock;\n\n  @Test\n  void adds() {\n    new Calc().add(1, 2);\n  }\n}\n"))
		if !mocked(t, a) {
			t.Errorf("@%s: want a mock hazard", ann)
		}
	}
}

// viewpoint: behaviour
// target: the Java, Kotlin and Python readers
// Persistence, cloud clients and HTTP clients perform input or output.
func TestPersistenceAndClientLibrariesPerformInputOrOutput(t *testing.T) {
	for _, pkg := range []string{"jakarta.persistence.EntityManager", "javax.persistence.EntityManager", "org.hibernate.Session",
		"io.quarkus.hibernate.orm.panache.PanacheRepository", "com.google.cloud.storage.Storage", "jakarta.ws.rs.client.Client",
		"javax.ws.rs.client.Client"} {
		for _, l := range []struct{ lang, path, src string }{
			{"java", "src/main/java/com/acme/rows/Rows.java", "package com.acme.rows;\n\nimport " + pkg + ";\n\npublic class Rows {}\n"},
			{"kotlin", "src/main/kotlin/com/acme/rows/Rows.kt", "package com.acme.rows\n\nimport " + pkg + "\n\nclass Rows\n"},
		} {
			a := analyze(t, l.lang, tree(l.path, l.src))
			if len(a.Imports) != 1 || !a.Imports[0].IO {
				t.Errorf("%s %s: want one import that performs input or output, got %+v", l.lang, pkg, a.Imports)
			}
		}
	}
	for _, mod := range []string{"requests", "httpx", "sqlite3", "boto3", "sqlalchemy", "google.cloud.storage"} {
		a := analyze(t, "python", tree("app/rows.py", "import "+mod+"\n"))
		if len(a.Imports) != 1 || !a.Imports[0].IO {
			t.Errorf("python %s: want one import that performs input or output, got %+v", mod, a.Imports)
		}
	}
}

// viewpoint: behaviour
// target: the TypeScript, Python and Java readers
// A mock outside the tests applies to every test of the file and is marked file-wide: a module
// mocked at the top of a TypeScript file, an import of a mocking library. A mock inside a test is
// that test's own.
func TestFileLevelMocksAreFileWide(t *testing.T) {
	wide := func(a *facts.Analysis) string {
		var out []string
		for _, ts := range a.Tests {
			for _, h := range ts.Hazards {
				if h.Category == facts.HazardMock {
					out = append(out, ts.Name+" "+h.Evidence+" wide="+tf(h.FileWide))
				}
			}
		}
		slices.Sort(out)
		return strings.Join(out, "\n")
	}
	expect(t, wide(analyze(t, "typescript", tree(
		"src/calc.ts", "export const add = (a: number, b: number) => a + b\n",
		"src/calc.test.ts", "import { add } from './calc'\nvi.mock('fancy-lib')\n\nit('adds', () => { vi.spyOn(Math, 'max'); expect(add(1, 2)).toBe(3) })\n",
	))), `
adds vi.mock('fancy-lib') wide=true
adds vi.spyOn(Math, 'max') wide=false`)
	expect(t, wide(analyze(t, "python", tree(
		"app/calc.py", "def add(a, b):\n    return a + b\n",
		"tests/test_calc.py", "from unittest import mock\nfrom app.calc import add\n\n\ndef test_adds():\n    assert add(1, 2) == 3\n",
	))), "test_adds from unittest import mock wide=true")
	expect(t, wide(analyze(t, "java", tree(
		"src/main/java/acme/Calc.java", "package acme;\n\npublic class Calc { public int add(int a, int b) { return a + b; } }\n",
		"src/test/java/acme/CalcTest.java", "package acme;\n\nimport org.junit.jupiter.api.Test;\nimport org.mockito.Mockito;\n\nclass CalcTest {\n    @Test void adds() { new Calc().add(1, 2); }\n}\n",
	))), "CalcTest.adds import org.mockito.Mockito; wide=true")
}
