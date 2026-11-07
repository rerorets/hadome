//go:build cgo

package treesitter

import (
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	fswalk "github.com/rerorets/hadome/internal/adapter/walk"
	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
)

// analyzeWalked runs one language over the files the walk lists, as a check does: files in the
// directories it skips are in the tree but not read.
func analyzeWalked(t *testing.T, name string, tree fstest.MapFS) *facts.Analysis {
	t.Helper()
	var l *lang
	for _, c := range Languages() {
		if c.name == name {
			l = c
		}
	}
	all, err := fswalk.Files(tree)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, p := range all {
		if l.Claims(p) {
			paths = append(paths, p)
		}
	}
	a, err := l.Analyze(engine.Version{Label: "head", FS: tree}, config.Config{}, paths)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// unknownLines renders the imports as "from -> to unknown=… unread=… io=… text", sorted.
func unknownLines(a *facts.Analysis) string {
	var out []string
	for _, im := range a.Imports {
		out = append(out, strings.TrimSpace(im.FromUnit+" -> "+im.ToUnit+" internal="+tf(im.Internal)+
			" unknown="+tf(im.Unknown)+" unread="+tf(im.Unread)+" io="+tf(im.IO)+" "+im.Text))
	}
	slices.Sort(out)
	return strings.Join(out, "\n")
}

// viewpoint: behaviour
// target: the TypeScript reader
// An import naming a path inside a directory the walk skips (a build output, a hidden directory)
// reaches a file hadome did not read, so its unit cannot be told: it is an import whose destination
// is unknown, not one that performs input or output, whether the file is there or not. An alias
// into such a directory is the same. A test that calls a name taken from it has an unresolved call.
func TestTypeScriptImportIntoAnUnreadDirectoryIsUnknown(t *testing.T) {
	a := analyzeWalked(t, "typescript", tree(
		"tsconfig.json", `{ "compilerOptions": { "baseUrl": ".", "paths": { "@gen/*": [".generated/*"] } } }`,
		"dist/client.js", "export const client = () => 1;\n",
		".generated/api.ts", "export const api = 1;\n",
		"src/lib/calc.ts", "export const add = (a: number, b: number) => a + b;\n",
		"src/app/main.ts", "import { client } from '../../dist/client';\nimport { api } from '@gen/api';\nimport { gone } from '../../build/gone';\nimport { add } from '../lib/calc';\nexport const run = () => add(client(), api + gone);\n",
		"src/app/main.test.ts", "import { client } from '../../dist/client';\nit('runs', () => { expect(client()).toBe(1); });\n",
	))
	expect(t, unknownLines(a), `
src/app ->  internal=false unknown=true unread=true io=false ../../build/gone
src/app ->  internal=false unknown=true unread=true io=false ../../dist/client
src/app ->  internal=false unknown=true unread=true io=false @gen/api
src/app -> src/lib internal=true unknown=false unread=false io=false `)
	if len(a.Tests) != 1 || len(a.Tests[0].Calls) != 0 || a.Tests[0].Unresolved != 1 {
		t.Errorf("want one test with one unresolved call and no target, got %+v", a.Tests)
	}
}

// viewpoint: behaviour
// target: the Python reader
// A relative import into a directory the walk skips is an import whose destination is unknown, as
// in TypeScript. A relative import that names nothing elsewhere stays an ordinary missing import.
func TestPythonRelativeImportIntoAnUnreadDirectoryIsUnknown(t *testing.T) {
	a := analyzeWalked(t, "python", tree(
		"app/__init__.py", "",
		"app/build/__init__.py", "",
		"app/build/gen.py", "def gen():\n    return 1\n",
		"app/main.py", "from .build.gen import gen\nfrom .calc import add\n\n\ndef run():\n    return add(gen(), 1)\n",
		"app/calc.py", "def add(a, b):\n    return a + b\n",
	))
	expect(t, unknownLines(a), `
app ->  internal=false unknown=true unread=true io=false .build.gen
app -> app internal=true unknown=false unread=false io=false `)
}

// viewpoint: behaviour
// target: inUnreadDir
// ".." starts with a dot but is no hidden directory: a path that leaves the repository is not
// inside a directory hadome does not read. A directory hadome reads that merely contains a skipped
// name (distance/, builder/) is not either.
func TestParentDirectoryIsNoUnreadDirectory(t *testing.T) {
	for p, want := range map[string]bool{
		"../shared/a": false, "src/distance/a": false, "src/builder/a": false,
		"src/dist/a": true, "src/.cache/a": true, "node_modules/x/index": true, "dist": true,
	} {
		if got := inUnreadDir(p); got != want {
			t.Errorf("inUnreadDir(%q) = %v, want %v", p, got, want)
		}
	}
}

// viewpoint: behaviour
// target: the TypeScript reader
// A path is tried as each target of an alias in turn, and the first that names a file wins. A
// directory with no index names no file, so a test's call of a name taken from it is unresolved.
// An external scoped package is named by its scope and name.
func TestTypeScriptPathsResolveToFilesOnly(t *testing.T) {
	a := analyzeWalked(t, "typescript", tree(
		"tsconfig.json", `{ "compilerOptions": { "baseUrl": ".", "paths": { "@lib/*": ["gone/*", "src/lib/*"] } } }`,
		"src/lib/calc.ts", "export const add = (a: number, b: number) => a + b;\n",
		"src/widgets/a.ts", "export const a = 1;\n",
		"src/app/main.ts", "import { add } from '@lib/calc';\nimport { get } from '@acme/client/sub';\nexport const run = () => add(get(), 1);\n",
		"src/app/main.test.ts", "import { a } from '../widgets';\nit('reads', () => { expect(a()).toBe(1); });\n",
	))
	expect(t, unknownLines(a), `
src/app -> @acme/client internal=false unknown=false unread=false io=false
src/app -> src/lib internal=true unknown=false unread=false io=false`)
	if len(a.Tests) != 1 || len(a.Tests[0].Calls) != 0 || a.Tests[0].Unresolved != 1 {
		t.Errorf("want one test with one unresolved call and no target, got %+v", a.Tests)
	}
}

// viewpoint: behaviour
// target: the Python reader
// A dynamic import named by a string is an ordinary import: of the repository's module, or of an
// external one that performs input or output by the io table.
func TestPythonDynamicImportByNameIsAnOrdinaryImport(t *testing.T) {
	a := analyzeWalked(t, "python", tree(
		"app/__init__.py", "",
		"app/calc.py", "def add(a, b):\n    return a + b\n",
		"app/main.py", "import importlib\n\n\ndef run():\n    importlib.import_module(\"socket\")\n    return importlib.import_module(\"app.calc\")\n",
	))
	expect(t, unknownLines(a), `
app -> app internal=true unknown=false unread=false io=false
app -> importlib internal=false unknown=false unread=false io=false
app -> socket internal=false unknown=false unread=false io=true`)
}
