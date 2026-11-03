//go:build cgo

package adapter_test

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/adapter"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/rules/tests"
)

// project is one language's smallest project: a product file and a test file whose test body is
// given, with %s for an optional skip.
type project struct {
	lang, product, productPath, testPath string
	test                                 func(skip, body string) string
	call, sleep, bareSkip, reasonedSkip  string
}

var projects = []project{
	{lang: "go", productPath: "store/store.go", product: "package store\n\n// Append adds a row.\nfunc Append(row string) {}\n",
		testPath: "store/store_test.go", call: `store.Append("a")`, sleep: "time.Sleep(time.Millisecond)",
		bareSkip: "t.Skip()", reasonedSkip: `t.Skip("needs a database")`,
		test: func(skip, body string) string {
			return "package store_test\n\nimport (\n\t\"testing\"\n\t\"time\"\n\n\t\"example.com/app/store\"\n)\n\nvar _ = time.Second\n\n" +
				"func TestAppend(t *testing.T) {\n\t" + skip + "\n\t" + body + "\n}\n"
		}},
	{lang: "typescript", productPath: "src/store.ts", product: "/** Appends a row. */\nexport function append(row: string) {}\n",
		testPath: "src/store.test.ts", call: `append("a")`, sleep: "setTimeout(() => {}, 10)",
		bareSkip: "it.skip", reasonedSkip: "// needs a database\nit.skip",
		test: func(skip, body string) string {
			if skip == "" {
				skip = "it"
			}
			return "import { append } from './store'\n\n" + skip + "('appends', () => {\n  " + body + "\n})\n"
		}},
	{lang: "python", productPath: "app/store.py", product: "def append(row):\n    \"\"\"Append a row.\"\"\"\n",
		testPath: "tests/test_store.py", call: `append("a")`, sleep: "time.sleep(1)",
		bareSkip: "@pytest.mark.skip", reasonedSkip: `@pytest.mark.skip(reason="needs a database")`,
		test: func(skip, body string) string {
			body = strings.ReplaceAll(body, "\n", "\n    ") // Python's block is its indentation
			return "import time\nimport pytest\nfrom app.store import append\n\n\n" + skip + "\ndef test_append():\n    " + body + "\n"
		}},
	{lang: "java", productPath: "src/main/java/app/Store.java",
		product:  "package app;\n\n/** Rows. */\npublic class Store {\n  /** Adds. */\n  public static void append(String row) {}\n}\n",
		testPath: "src/test/java/app/StoreTest.java", call: `Store.append("a");`, sleep: "Thread.sleep(10);",
		bareSkip: "@Disabled", reasonedSkip: `@Disabled("needs a database")`,
		test: func(skip, body string) string {
			return "package app;\n\nimport org.junit.jupiter.api.Test;\n\nclass StoreTest {\n  " + skip + "\n  @Test\n  void appends() throws Exception {\n    " + body + "\n  }\n}\n"
		}},
	{lang: "kotlin", productPath: "src/main/kotlin/app/Store.kt", product: "package app\n\n/** Appends a row. */\nfun append(row: String) {}\n",
		testPath: "src/test/kotlin/app/StoreTest.kt", call: `append("a")`, sleep: "Thread.sleep(10)",
		bareSkip: "@Disabled", reasonedSkip: `@Disabled("needs a database")`,
		test: func(skip, body string) string {
			return "package app\n\nimport org.junit.jupiter.api.Test\n\nclass StoreTest {\n    " + skip + "\n    @Test\n    fun appends() {\n        " + body + "\n    }\n}\n"
		}},
	{lang: "rust", productPath: "src/store.rs", product: "/// Appends a row.\npub fn append(row: &str) {}\n",
		testPath: "tests/store.rs", call: `append("a");`, sleep: "std::thread::sleep(d);",
		bareSkip: "#[ignore]", reasonedSkip: `#[ignore = "needs a database"]`,
		test: func(skip, body string) string {
			return "use crate::store::append;\n\n#[test]\n" + skip + "\nfn appends() {\n    " + body + "\n}\n"
		}},
}

func (p project) tree(test string) fstest.MapFS {
	t := fstest.MapFS{
		"go.mod": {Data: []byte("module example.com/app\n")}, "Cargo.toml": {Data: []byte("[package]\nname = \"app\"\n")},
		"src/lib.rs":  {Data: []byte("pub mod store;\n")},
		p.productPath: {Data: []byte(p.product)},
	}
	if test != "" {
		t[p.testPath] = &fstest.MapFile{Data: []byte(test)}
	}
	return t
}

type trees struct{ base, head fs.FS }

func (s trees) Base() (engine.Version, error)       { return engine.Version{Label: "base", FS: s.base}, nil }
func (s trees) Head() (engine.Version, error)       { return engine.Version{Label: "head", FS: s.head}, nil }
func (s trees) Renames() (map[string]string, error) { return nil, nil }

// viewpoint: external contract
// target: adapter.Analyzer through engine.Engine.Check
// Treesitter design invariant 1: every language fills the same facts, so the same change fails the
// same rule whatever the language.
// guards: same-rules-every-language
func TestTheSameRegressionFailsInEveryLanguage(t *testing.T) {
	for _, p := range projects {
		withTest := p.tree(p.test("", p.call))
		cases := []struct {
			name, want, notWant string
			head                fstest.MapFS
		}{
			{"deleting the only test", "tests/scope-shrunk", "", p.tree("")},
			{"skipping it without a reason", "tests/skip-without-reason", "", p.tree(p.test(p.bareSkip, p.call))},
			{"skipping it with a reason", "tests/scope-shrunk", "tests/skip-without-reason", p.tree(p.test(p.reasonedSkip, p.call))},
			{"sleeping in it", "tests/no-sleep", "", p.tree(p.test("", p.call+"\n"+p.sleep))},
			{"changing nothing", "", "", withTest},
		}
		for _, tc := range cases {
			t.Run(p.lang+"/"+tc.name, func(t *testing.T) {
				e := &engine.Engine{Analyzer: adapter.New(), Rules: tests.All()}
				r, err := e.Check(trees{base: withTest, head: tc.head})
				if err != nil {
					t.Fatal(err)
				}
				var got []string
				for _, f := range r.Regressions {
					got = append(got, f.Rule)
				}
				all := strings.Join(got, ",")
				if !strings.Contains(all, tc.want) || (tc.want == "" && len(got) > 0) ||
					(tc.notWant != "" && strings.Contains(all, tc.notWant)) {
					t.Errorf("want %q (and not %q) among the regressions, got %v", tc.want, tc.notWant, got)
				}
			})
		}
	}
}

// viewpoint: external contract
// target: tests/skip-without-reason through engine.Engine.Check
// The fix is read by agents in every language; an example in another language's syntax sends them
// to write something their test runner ignores.
func TestTheSkipFixShowsTheTestsOwnLanguage(t *testing.T) {
	for _, p := range projects {
		t.Run(p.lang, func(t *testing.T) {
			e := &engine.Engine{Analyzer: adapter.New(), Rules: tests.All()}
			r, err := e.Check(trees{base: p.tree(p.test("", p.call)), head: p.tree(p.test(p.bareSkip, p.call))})
			if err != nil {
				t.Fatal(err)
			}
			skip := strings.TrimRight(p.bareSkip, "()]")
			for _, f := range r.Regressions {
				if f.Rule == "tests/skip-without-reason" {
					if !strings.Contains(f.Fix, skip) {
						t.Errorf("fix %q does not show %q", f.Fix, skip)
					}
					return
				}
			}
			t.Errorf("no tests/skip-without-reason among %v", r.Regressions)
		})
	}
}
