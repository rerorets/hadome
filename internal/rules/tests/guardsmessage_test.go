package tests_test

import (
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/adapter"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/rules/tests"
)

// viewpoint: external contract
// target: tests/guards-nothing through engine.Engine.Check
// The finding says what the test calls: a product test that calls only a test support package is
// told apart from one that calls no code of the repository. The support package's own test is not
// reported.
func TestGuardsNothingSaysWhetherTheTestCallsSupportCode(t *testing.T) {
	base := fstest.MapFS{
		"go.mod":                    {Data: []byte("module example.com/app\n")},
		"store/store.go":            {Data: []byte("package store\n\n// Open opens.\nfunc Open() int { return 1 }\n")},
		"internal/testutil/zone.go": {Data: []byte("package testutil\n\nfunc Zone() string { return \"UTC\" }\n")},
	}
	head := fstest.MapFS{}
	for p, f := range base {
		head[p] = f
	}
	head["internal/testutil/zone_test.go"] = &fstest.MapFile{Data: []byte("package testutil_test\n\nimport (\n\t\"testing\"\n\n" +
		"\t\"example.com/app/internal/testutil\"\n)\n\nfunc TestZone(t *testing.T) {\n\tif testutil.Zone() != \"UTC\" {\n\t\tt.Fatal(\"x\")\n\t}\n}\n")}
	head["store/store_test.go"] = &fstest.MapFile{Data: []byte("package store_test\n\nimport (\n\t\"strings\"\n\t\"testing\"\n\n" +
		"\t\"example.com/app/internal/testutil\"\n)\n\nfunc TestStoreZone(t *testing.T) {\n\tif testutil.Zone() != \"UTC\" {\n\t\tt.Fatal(\"x\")\n\t}\n}\n\n" +
		"func TestUpper(t *testing.T) {\n\tif strings.ToUpper(\"a\") != \"A\" {\n\t\tt.Fatal(\"x\")\n\t}\n}\n")}
	e := &engine.Engine{Analyzer: adapter.New(), Rules: tests.All()}
	r, err := e.Check(trees{base: base, head: head})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range r.Regressions {
		if f.Rule == "tests/guards-nothing" {
			got = append(got, f.Message.In("en"))
		}
	}
	sort.Strings(got)
	want := "TestStoreZone calls only test support code, and no product code nor its values\n" +
		"TestUpper calls no code of this repository and reads none of its values"
	if g := strings.Join(got, "\n"); g != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", g, want)
	}
}
