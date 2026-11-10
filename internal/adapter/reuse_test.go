package adapter_test

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/adapter"
	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
)

// mixed is a small repository in every language the analyzer reads: product code, tests that call
// it across files, helpers, module files, and SQL that a Go test's string calls.
var mixed = fstest.MapFS{
	"go.mod":               {Data: []byte("module example.com/shop\n\ngo 1.22\n")},
	"store/store.go":       {Data: []byte("package store\n\n// Append adds a row.\nfunc Append(row string) error { return nil }\n")},
	"store/helper_test.go": {Data: []byte("package store\n\nfunc add(row string) { _ = Append(row) }\n")},
	"store/store_test.go": {Data: []byte("package store\n\nimport \"testing\"\n\nfunc TestAppend(t *testing.T) {\n\tadd(\"a\")\n" +
		"\tq := \"SELECT billing.is_active(1)\"\n\tif q == \"\" {\n\t\tt.Fatal(\"empty\")\n\t}\n}\n")},
	"web/package.json":          {Data: []byte(`{"name":"web"}`)},
	"web/tsconfig.json":         {Data: []byte(`{"compilerOptions":{"baseUrl":"."}}`)},
	"web/src/cart.ts":           {Data: []byte("export function total(xs: number[]): number { return xs.length }\n")},
	"web/src/cart.test.ts":      {Data: []byte("import { total } from './cart'\ntest('total', () => { expect(total([1])).toBe(1) })\n")},
	"py/shop/tax.py":            {Data: []byte("def rate(x):\n    return x * 2\n")},
	"py/tests/test_tax.py":      {Data: []byte("from shop.tax import rate\n\ndef test_rate():\n    assert rate(1) == 2\n")},
	"kt/src/main/kotlin/Fee.kt": {Data: []byte("package shop\n\nfun fee(x: Int): Int = x + 1\n")},
	"kt/src/test/kotlin/FeeTest.kt": {Data: []byte("package shop\n\nimport kotlin.test.Test\nimport kotlin.test.assertEquals\n\n" +
		"class FeeTest {\n    @Test\n    fun fee() { assertEquals(2, fee(1)) }\n}\n")},
	"db/billing.sql": {Data: []byte("CREATE FUNCTION billing.is_active(id int) RETURNS boolean AS $$ SELECT true $$ LANGUAGE sql;\n")},
	"infra/main.tf":  {Data: []byte("resource \"null_resource\" \"x\" {}\n")},
	"README.md":      {Data: []byte("# shop\n")},
}

func clone(m fstest.MapFS) fstest.MapFS {
	out := fstest.MapFS{}
	for k, v := range m {
		c := *v
		out[k] = &c
	}
	return out
}

func analyzeAlone(t *testing.T, v engine.Version, c config.Config) *facts.Analysis {
	t.Helper()
	a, err := adapter.New().WithoutReuse().Analyze(v, c)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// samePair checks that analysing base and head in one session gives each version the facts it has
// read alone, and returns the session's counts.
func samePair(t *testing.T, base, head fstest.MapFS, bc, hc config.Config) map[string]int {
	t.Helper()
	bv, hv := engine.Version{Label: "base", FS: base}, engine.Version{Label: "head", FS: head}
	b, h, stats, err := adapter.New().PairWithStats(bv, bc, hv, hc)
	if err != nil {
		t.Fatal(err)
	}
	if want := analyzeAlone(t, bv, bc); !reflect.DeepEqual(b, want) {
		t.Errorf("base read with head differs from base read alone:\n%s", diff(b, want))
	}
	if want := analyzeAlone(t, hv, hc); !reflect.DeepEqual(h, want) {
		t.Errorf("head read with base differs from head read alone:\n%s", diff(h, want))
	}
	hits := map[string]int{}
	for id, st := range stats {
		hits[id] = st.Hits
	}
	return hits
}

func diff(got, want *facts.Analysis) string {
	g, w := fmt.Sprintf("%+v", *got), fmt.Sprintf("%+v", *want)
	for i := 0; i < len(g) && i < len(w); i++ {
		if g[i] != w[i] {
			lo := max(0, i-200)
			return fmt.Sprintf("got  …%s…\nwant …%s…", g[lo:min(len(g), i+200)], w[lo:min(len(w), i+200)])
		}
	}
	return fmt.Sprintf("got %d chars, want %d", len(g), len(w))
}

// viewpoint: invariant
// target: adapter.Analyzer.AnalyzePair
// When head is base, every language's facts are served from base, and both get the facts they
// have read alone.
func TestAnUnchangedHeadIsServedFromBase(t *testing.T) {
	hits := samePair(t, mixed, clone(mixed), config.Config{}, config.Config{})
	if hits["adapter/language"] == 0 {
		t.Errorf("no language served from base: %v", hits)
	}
}

// viewpoint: invariant
// target: adapter.Analyzer.AnalyzePair
// A head that differs from base in exactly one input gets the facts it has read alone: whatever
// changed, nothing remembered from base hides it. The inputs are every file changed, removed and
// copied to another path, a file appearing beside every file, and every setting adapters read.
func TestAHeadWithOneInputChangedGetsItsOwnFacts(t *testing.T) {
	paths := make([]string, 0, len(mixed))
	for p := range mixed {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	type variant struct {
		name string
		head fstest.MapFS
		cfg  config.Config
	}
	var variants []variant
	for _, p := range paths {
		changed := clone(mixed)
		changed[p] = &fstest.MapFile{Data: append(append([]byte{}, mixed[p].Data...), changeFor(p)...)}
		variants = append(variants, variant{"change " + p, changed, config.Config{}})
		removed := clone(mixed)
		delete(removed, p)
		variants = append(variants, variant{"remove " + p, removed, config.Config{}})
		copied := clone(mixed)
		copied["copy/"+p] = mixed[p]
		variants = append(variants, variant{"copy " + p, copied, config.Config{}})
		dir := p[:strings.LastIndex(p, "/")+1]
		for _, marker := range []string{"dbt_project.yml", "sqitch.plan", "conftest.py", "Cargo.toml", "__init__.py"} {
			added := clone(mixed)
			added[dir+marker] = &fstest.MapFile{Data: []byte("name: x\n")}
			variants = append(variants, variant{"add " + dir + marker, added, config.Config{}})
		}
	}
	settings := []config.Config{
		{Languages: []string{"go"}},
		{TestFiles: []string{"store/**"}},
		{GeneratedFiles: []string{"web/**"}},
		{ProductFiles: []string{"store/helper_test.go"}},
		{AnsibleFiles: []string{"README.md"}},
		{IO: []string{"example.com/shop/store"}},
		{Assertions: []string{"add"}},
		{Hazards: map[string][]string{"sleep": {"store.Append"}}},
		{SQLSinks: map[string][]string{"go": {"query"}}},
		{Databases: []config.Database{{Name: "main", Dialect: "postgresql"}}},
	}
	for i, c := range settings {
		variants = append(variants, variant{fmt.Sprintf("setting %d", i), clone(mixed), c})
	}
	for _, v := range variants {
		t.Run(v.name, func(t *testing.T) { samePair(t, mixed, v.head, v.cfg, v.cfg) })
	}
}

// changeFor is a change that keeps the file readable in its language.
func changeFor(p string) string {
	switch {
	case strings.HasSuffix(p, ".py"), strings.HasSuffix(p, ".tf"), strings.HasSuffix(p, ".md"):
		return "\n# changed\n"
	case strings.HasSuffix(p, ".sql"):
		return "\nCREATE TABLE changed (id int);\n"
	case strings.HasSuffix(p, ".json"):
		return " "
	case strings.HasSuffix(p, ".mod"):
		return "\nrequire example.com/other v1.0.0\n"
	}
	return "\n// changed\nfunc Changed() {}\n"
}

func hasTexts(a *facts.Analysis) bool {
	for _, t := range a.Tests {
		if len(t.Texts) > 0 {
			return true
		}
	}
	return false
}

// viewpoint: invariant
// target: adapter.Analyzer.AnalyzePair
// Reading base, head and base again in one session gives base the same facts both times: no
// result was changed by a version it was served to.
func TestAResultIsNotChangedByTheVersionsItServes(t *testing.T) {
	head := clone(mixed)
	head["store/store.go"] = &fstest.MapFile{Data: []byte("package store\n\n// Append adds a row.\nfunc Append(row string) error { return nil }\n\nfunc Other() {}\n")}
	vs := []engine.Version{{Label: "base", FS: mixed}, {Label: "head", FS: head}, {Label: "base again", FS: mixed}}
	got, err := adapter.New().InOneSession(vs, config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got[0], got[2]) {
		t.Errorf("base read again differs:\n%s", diff(got[2], got[0]))
	}
	if want := analyzeAlone(t, vs[1], config.Config{}); !reflect.DeepEqual(got[1], want) {
		t.Errorf("head differs from head read alone:\n%s", diff(got[1], want))
	}
}
