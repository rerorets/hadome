//go:build cgo

package treesitter

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/facts"
)

// viewpoint: behaviour
// target: the tree-sitter reader with files.tests and files.generated
// Globs add roles the conventions do not give: a file under files.tests is read as tests, and one
// under files.generated is generated, also when it matches both. A glob never takes a role away.
func TestRolesFromConfiguration(t *testing.T) {
	c := config.Config{TestFiles: []string{"checks/**", "gen/**"}, GeneratedFiles: []string{"gen/**", "src/*.test.ts"}}
	a := analyzeWith(t, "typescript", tree(
		"src/store.ts", "export function save(r: number) { return r; }\n",
		"src/store.test.ts", "import { save } from \"./store\";\nit(\"saves\", () => { expect(save(1)).toBe(1); });\n",
		"checks/store.ts", "import { save } from \"../src/store\";\nit(\"checks\", () => { expect(save(2)).toBe(2); });\n",
		"gen/client.ts", "export function call() { return 1; }\n",
	), c)
	var got []string
	for _, f := range a.Files {
		got = append(got, fmt.Sprintf("%s %s", f.Path, f.Role))
	}
	for _, ts := range a.Tests {
		got = append(got, "test "+ts.Name+" in "+ts.Path)
	}
	for _, s := range a.Symbols {
		got = append(got, "symbol "+s.ID)
	}
	sort.Strings(got)
	want := strings.Join([]string{
		"checks/store.ts test",
		"gen/client.ts generated",
		"src/store.test.ts test",
		"src/store.ts source",
		"symbol src.save",
		"test checks in checks/store.ts",
		"test saves in src/store.test.ts",
	}, "\n")
	if g := strings.Join(got, "\n"); g != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", g, want)
	}
}

// viewpoint: behaviour
// target: the tree-sitter reader with files.product
// files.product takes back the test support role a directory or a name gives by default: a server
// the end-to-end tests run against, kept under e2e/, is product code with symbols, and a test that
// calls it targets it. It never makes a test file, or a files.tests file, product code.
func TestProductFilesAreNotTestSupport(t *testing.T) {
	c := config.Config{TestFiles: []string{"e2e/checks/**"},
		ProductFiles: []string{"e2e/server/**", "e2e/app.test.ts", "e2e/checks/**", "tests/helpers.py"}}
	ts := analyzeWith(t, "typescript", tree(
		"e2e/server/main.ts", "export function serve() { return 1 }\n",
		"e2e/fixtures.ts", "export function fixture() { return 2 }\n",
		"e2e/app.test.ts", "import { serve } from './server/main'\nit('serves', () => { expect(serve()).toBe(1) })\n",
		"e2e/checks/smoke.ts", "it('smoke', () => { expect(1).toBe(1) })\n",
	), c)
	py := analyzeWith(t, "python", tree(
		"tests/helpers.py", "def build():\n    return 1\n",
		"tests/conftest.py", "def fixture():\n    return 2\n",
	), c)
	var got []string
	for _, a := range []*facts.Analysis{ts, py} {
		for _, f := range a.Files {
			got = append(got, fmt.Sprintf("%s %s", f.Path, f.Role))
		}
		for _, s := range a.Symbols {
			got = append(got, "symbol "+s.ID)
		}
		for _, t := range a.Tests {
			var calls []string
			for _, c := range t.Calls {
				calls = append(calls, c.Target)
			}
			got = append(got, fmt.Sprintf("test %s in %s calls %v", t.Name, t.Path, calls))
		}
	}
	sort.Strings(got)
	want := strings.Join([]string{
		"e2e/app.test.ts test",
		"e2e/checks/smoke.ts test",
		"e2e/fixtures.ts test",
		"e2e/server/main.ts source",
		"symbol e2e/server.serve",
		"symbol tests/helpers.build",
		"test serves in e2e/app.test.ts calls [e2e/server.serve]",
		"test smoke in e2e/checks/smoke.ts calls []",
		"tests/conftest.py test",
		"tests/helpers.py source",
	}, "\n")
	if g := strings.Join(got, "\n"); g != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", g, want)
	}
}
