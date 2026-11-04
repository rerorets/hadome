//go:build cgo

package unknown

import (
	"testing/fstest"

	"github.com/rerorets/hadome/internal/engine"
)

func init() { languageImportCases = tsImportCases() }

func tsImportCases() []engine.Case {
	ts := func(files ...string) fstest.MapFS {
		t := fstest.MapFS{"package.json": {Data: []byte(`{"name":"app"}`)}}
		for i := 0; i+1 < len(files); i += 2 {
			t[files[i]] = &fstest.MapFile{Data: []byte(files[i+1])}
		}
		return t
	}
	const named = "export async function load() { return import(\"./plugins/a\"); }\n"
	const dynamic = "export async function load(name: string) { return import(`./plugins/${name}`); }\n"
	const plugin = "export const a = 1;\n"
	return []engine.Case{
		{Name: "a new import whose destination is unknown needs review",
			Base: ts("src/load.ts", named, "src/plugins/a.ts", plugin),
			Head: ts("src/load.ts", dynamic, "src/plugins/a.ts", plugin), WantReview: []string{"unknown/import"}},
		{Name: "one already in base does not",
			Base: ts("src/load.ts", dynamic, "src/plugins/a.ts", plugin), Head: ts("src/load.ts", dynamic, "src/plugins/a.ts", plugin)},
		{Name: "a dynamic import with a string argument is an ordinary import",
			Base: ts("src/plugins/a.ts", plugin), Head: ts("src/load.ts", named, "src/plugins/a.ts", plugin)},
		{Name: "one inside a test file does not",
			Base: ts("src/plugins/a.ts", plugin),
			Head: ts("src/plugins/a.ts", plugin, "src/load.test.ts", "it(\"loads\", async () => { const n = \"a\"; await import(`./plugins/${n}`); });\n")},
	}
}
