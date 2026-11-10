package sql

import (
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
)

// viewpoint: behaviour
// target: sql.Adapter.Analyze
// The catalog Analyze keeps for Link holds the databases' objects and nothing of the files: no
// text, definition or tree stays while the other languages are read and linked. A test file's
// trees are read for its tests before they go.
func TestTheCatalogKeptForLinkHoldsNoFile(t *testing.T) {
	tree := fstest.MapFS{
		"db/billing.sql": {Data: []byte("CREATE FUNCTION billing.is_active(id int) RETURNS boolean AS $$ SELECT true $$ LANGUAGE sql;\n")},
		"tests/billing_test.sql": {Data: []byte("SELECT plan(1);\nSELECT ok(billing.is_active(1));\n")},
	}
	a := New()
	an, err := a.Analyze(engine.Version{FS: tree}, config.Config{}, []string{"db/billing.sql", "tests/billing_test.sql"})
	if err != nil {
		t.Fatal(err)
	}
	if len(an.Tests) != 1 || len(an.Tests[0].Calls) != 1 || an.Tests[0].Calls[0].Target != ".sql/billing.is_active" {
		t.Errorf("the test file's test %+v, want one that calls .sql/billing.is_active", an.Tests)
	}
	if len(a.cache) != 1 {
		t.Fatalf("%d catalogs kept, want 1", len(a.cache))
	}
	for _, cat := range a.cache {
		for _, db := range cat.databases {
			if len(db.objects) != 1 {
				t.Errorf("the database's objects %v, want billing.is_active", db.objects)
			}
			if len(db.files) != 0 {
				t.Errorf("the catalog keeps %d files", len(db.files))
			}
		}
	}
}
