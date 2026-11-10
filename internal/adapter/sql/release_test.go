package sql

import (
	"reflect"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/adapter/reuse"
	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/facts"
)

// viewpoint: behaviour
// target: sql.Adapter.Analyze
// The catalog Analyze keeps for Link holds the databases' objects and nothing of the files: no
// text, definition or tree stays while the other languages are read and linked. A test file's
// trees are read for its tests before they go.
func TestTheCatalogKeptForLinkHoldsNoFile(t *testing.T) {
	tree := fstest.MapFS{
		"db/billing.sql":         {Data: []byte("CREATE FUNCTION billing.is_active(id int) RETURNS boolean AS $$ SELECT true $$ LANGUAGE sql;\n")},
		"tests/billing_test.sql": {Data: []byte("SELECT plan(1);\nSELECT ok(billing.is_active(1));\n")},
	}
	paths := []string{"db/billing.sql", "tests/billing_test.sql"}
	r := reuse.Lone(tree)
	an, err := New().Analyze(r, config.Analysis{}, paths)
	if err != nil {
		t.Fatal(err)
	}
	if len(an.Tests) != 1 || len(an.Tests[0].Calls) != 1 || an.Tests[0].Calls[0].Target != ".sql/billing.is_active" {
		t.Errorf("the test file's test %+v, want one that calls .sql/billing.is_active", an.Tests)
	}
	b, err := buildStage.Do(r, buildInput{Paths: paths})
	if err != nil {
		t.Fatal(err)
	}
	for _, db := range b.cat.databases {
		if len(db.objects) != 1 {
			t.Errorf("the database's objects %v, want billing.is_active", db.objects)
		}
		if len(db.files) != 0 {
			t.Errorf("the catalog keeps %d files", len(db.files))
		}
	}
}

// viewpoint: failure
// target: sql.Adapter.Link
// A catalog Analyze built is used only for the same files read the same way: the same .sql files
// read under configuration that makes one of them a test file are another catalog, whose test file
// defines no product object. A string naming that object is then unresolved, as it is for a
// version whose catalog was never built.
func TestACatalogIsNotUsedForFilesReadAnotherWay(t *testing.T) {
	tree := fstest.MapFS{
		"db/billing.sql": {Data: []byte("CREATE FUNCTION billing.is_active(id int) RETURNS boolean AS $$ SELECT true $$ LANGUAGE sql;\n")},
	}
	asTests := config.Analysis{TestFiles: []string{"db/**"}}
	link := func(r *reuse.Run) facts.Test {
		an := &facts.Analysis{
			Files: []facts.File{{Path: "db/billing.sql", Language: "sql"}},
			Tests: []facts.Test{{Name: "TestActive", Path: "app/billing_test.go",
				Texts: []facts.Text{{Path: "app/billing_test.go", Line: 3, Value: "SELECT billing.is_active(1)", Times: 1}}}},
		}
		if err := New().Link(r, asTests, an); err != nil {
			t.Fatal(err)
		}
		return an.Tests[0]
	}
	kept := reuse.Lone(tree)
	if _, err := New().Analyze(kept, config.Analysis{}, []string{"db/billing.sql"}); err != nil {
		t.Fatal(err)
	}
	got, want := link(kept), link(reuse.Lone(tree))
	if !reflect.DeepEqual(got, want) {
		t.Errorf("linked with the catalog built under other configuration\n%+v\nwant\n%+v", got, want)
	}
}
