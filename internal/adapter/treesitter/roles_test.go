//go:build cgo

package treesitter

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/rerorets/hadome/internal/config"
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
