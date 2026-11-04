//go:build cgo

package treesitter

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"testing/fstest"
)

// dynamicImports renders each import read from one file: unknown ones with their text, the others
// with their destination.
func dynamicImports(t *testing.T, name, path string, tr fstest.MapFS) string {
	t.Helper()
	var out []string
	for _, im := range analyze(t, name, tr).Imports {
		if im.Path != path {
			continue
		}
		if im.Unknown {
			out = append(out, fmt.Sprintf("unknown %q to=%q", im.Text, im.ToUnit))
		} else {
			out = append(out, fmt.Sprintf("to %s internal=%v", im.ToUnit, im.Internal))
		}
	}
	sort.Strings(out)
	return strings.Join(out, "\n")
}

// viewpoint: behaviour
// target: the readers of TypeScript, Python, Java and Kotlin
// An import whose argument is not a string literal has an unknown destination and keeps its text;
// one whose argument is a literal resolves like any other import.
func TestDynamicImports(t *testing.T) {
	t.Run("typescript", func(t *testing.T) {
		expect(t, dynamicImports(t, "typescript", "src/load.ts", tree(
			"src/plugins/a.ts", "export const a = 1;\n",
			"src/load.ts", "export async function load(n: string) {\n"+
				"  await import(\"./plugins/a\");\n"+
				"  await import(`./plugins/${n}`);\n"+
				"  require(n);\n"+
				"}\n")), `to src/plugins internal=true
unknown "import(`+"`./plugins/${n}`"+`)" to=""
unknown "require(n)" to=""`)
	})
	t.Run("python", func(t *testing.T) {
		expect(t, dynamicImports(t, "python", "src/app/load.py", tree(
			"src/app/__init__.py", "",
			"src/app/plugins.py", "A = 1\n",
			"src/app/load.py", "import importlib\n\n\ndef load(name):\n"+
				"    importlib.import_module(\"app.plugins\")\n"+
				"    importlib.import_module(name)\n"+
				"    __import__(f\"app.{name}\")\n")), `to importlib internal=false
to src/app internal=true
unknown "__import__(f\"app.{name}\")" to=""
unknown "importlib.import_module(name)" to=""`)
	})
	t.Run("java", func(t *testing.T) {
		expect(t, dynamicImports(t, "java", "src/main/java/app/Load.java", tree(
			"src/main/java/app/plugins/A.java", "package app.plugins;\n\npublic class A {}\n",
			"src/main/java/app/Load.java", "package app;\n\npublic class Load {\n"+
				"  void load(String n) throws Exception {\n"+
				"    Class.forName(\"app.plugins.A\");\n"+
				"    Class.forName(n);\n"+
				"  }\n}\n")), `to app.plugins internal=true
unknown "Class.forName(n)" to=""`)
	})
	t.Run("kotlin", func(t *testing.T) {
		expect(t, dynamicImports(t, "kotlin", "src/main/kotlin/app/Load.kt", tree(
			"src/main/kotlin/app/plugins/A.kt", "package app.plugins\n\nclass A\n",
			"src/main/kotlin/app/Load.kt", "package app\n\nfun load(n: String) {\n"+
				"    Class.forName(\"app.plugins.A\")\n"+
				"    Class.forName(\"app.$n\")\n"+
				"}\n")), `to app.plugins internal=true
unknown "Class.forName(\"app.$n\")" to=""`)
	})
}
