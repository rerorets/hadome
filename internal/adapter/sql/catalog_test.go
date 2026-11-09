package sql_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/rerorets/hadome/internal/adapter/sql"
	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/facts"
)

// symbolFacts lists each symbol with whether it is documented and exported, its number of
// parameters and whether one of them is a scope.
func symbolFacts(an *facts.Analysis) []string {
	var out []string
	for _, s := range an.Symbols {
		out = append(out, fmt.Sprintf("%s doc=%v exported=%v params=%d scoped=%v", s.ID, s.Doc, s.Exported, s.Params, s.Scoped))
	}
	return out
}

// viewpoint: behaviour
// target: sql.database.symbols
// An object is documented by a comment on the line before it, by COMMENT ON, or by a description in
// its own statement (MySQL's COMMENT, BigQuery's description option); a COMMENT ON an object that
// does not exist documents nothing. Every object is exported, and a routine has its parameters,
// scoped when one names whose data it works on.
func TestSymbolsAreDocumentedExportedAndScoped(t *testing.T) {
	cases := []struct {
		name string
		c    config.Config
		src  string
		want []string
	}{
		{"postgresql", postgres(), "-- rows of a\nCREATE TABLE a (id int);\nCREATE TABLE b (id int);\n" +
			"COMMENT ON TABLE b IS 'rows';\nCOMMENT ON TABLE missing IS 'none';\nCREATE TABLE c (id int);\n" +
			"CREATE FUNCTION f(tenant_id int, n int) RETURNS int AS $$ SELECT 1 $$ LANGUAGE sql;\n\n" +
			"CREATE FUNCTION g(n int) RETURNS int AS $$ SELECT 1 $$ LANGUAGE sql;\n",
			[]string{
				".sql/public.a doc=true exported=true params=0 scoped=false",
				".sql/public.b doc=true exported=true params=0 scoped=false",
				".sql/public.c doc=false exported=true params=0 scoped=false",
				".sql/public.f doc=false exported=true params=2 scoped=true",
				".sql/public.g doc=false exported=true params=1 scoped=false",
			}},
		{"mysql", dialect("mysql"), "CREATE TABLE t (\n  id int\n) COMMENT = 'rows';\nCREATE TABLE u (id int);\n" +
			"CREATE TABLE w (id int) COMMENT 'last'",
			[]string{
				".sql/t doc=true exported=true params=0 scoped=false",
				".sql/u doc=false exported=true params=0 scoped=false",
				".sql/w doc=true exported=true params=0 scoped=false",
			}},
		{"bigquery", dialect("bigquery"), "CREATE TABLE ds.t (id INT64) OPTIONS(description = \"rows\");\n" +
			"CREATE TABLE ds.u (id INT64);\n",
			[]string{
				".sql/ds.t doc=true exported=true params=0 scoped=false",
				".sql/ds.u doc=false exported=true params=0 scoped=false",
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			an := analyze(t, sql.New(), version("db/schema.sql", tc.src), tc.c)
			if got := symbolFacts(an); !slices.Equal(got, tc.want) {
				t.Errorf("symbols\n%v\nwant\n%v\n(unread %v)", got, tc.want, unread(an))
			}
		})
	}
}

// viewpoint: behaviour
// target: sql.database.apply
// A trigger follows its table: it stays with a table that is renamed, and goes with one that is
// dropped, leaving the triggers of other tables.
func TestTriggersFollowTheirTable(t *testing.T) {
	files := []string{"db/schema.sql", "CREATE TABLE t (id int);\nCREATE TABLE t2 (id int);\n" +
		"CREATE TRIGGER trg BEFORE INSERT ON t FOR EACH ROW SET NEW.id = NEW.id;\n" +
		"CREATE TRIGGER trg2 BEFORE INSERT ON t2 FOR EACH ROW SET NEW.id = NEW.id;\n" +
		"RENAME TABLE t TO u;\nDROP TABLE t2;\n"}
	an := analyze(t, sql.New(), version(files...), dialect("mysql"))
	if got, want := symbolIDs(an), []string{".sql/trg", ".sql/u"}; !slices.Equal(got, want) {
		t.Errorf("symbols %v, want %v", got, want)
	}
	got := linked(t, dialect("mysql"), []facts.Text{text("INSERT INTO u (id) VALUES (1)")}, files...)
	if !slices.Contains(targets(got), ".sql/trg") {
		t.Errorf("calls %v, want the trigger of u", targets(got))
	}
}

// viewpoint: behaviour
// target: sql.database.rename
// SET SCHEMA moves an object and keeps its name; a rename of another kind of object leaves it.
func TestRenamesMoveOnlyTheirKind(t *testing.T) {
	an := analyze(t, sql.New(), version("db/schema.sql", "CREATE TABLE billing.a (id int);\n"+
		"ALTER TABLE billing.a SET SCHEMA other;\nCREATE TABLE s (id int);\nALTER SEQUENCE s RENAME TO s2;\n"), postgres())
	if got, want := symbolIDs(an), []string{".sql/other.a", ".sql/public.s"}; !slices.Equal(got, want) {
		t.Errorf("symbols %v, want %v (unread %v)", got, want, unread(an))
	}
}

// viewpoint: behaviour
// target: sql.database.applyOrder
// Files are applied in the configured order, then in the natural order of their paths, whatever
// order they are given in. A drop removes an object created in a file whose order against it is
// known: the configuration orders them, they share a directory, or both names start with a version.
// Otherwise the object remains.
func TestTheOrderFilesAreAppliedIn(t *testing.T) {
	const create = "CREATE FUNCTION f() RETURNS int AS $$ SELECT 1 $$ LANGUAGE sql;"
	const drop = "DROP FUNCTION f();"
	ordered := postgres()
	ordered.Databases[0].Order = []string{"db/later/**"}
	cases := []struct {
		name  string
		c     config.Config
		files []string
		want  []string
	}{
		{"names without numbers, given out of order",
			postgres(), []string{"db/b.sql", drop, "db/a.sql", create}, nil},
		{"numbers of one length, given out of order",
			postgres(), []string{"db/V3__a.sql", drop, "db/V2__z.sql", create}, nil},
		{"directories whose order is not known",
			postgres(), []string{"db/a/f.sql", create, "db/b/drop.sql", drop}, []string{".sql/public.f"}},
		{"versioned names in two directories",
			postgres(), []string{"db/a/V1__f.sql", create, "db/b/V2__drop.sql", drop}, nil},
		{"a configured glob before the paths it does not match",
			ordered, []string{"db/later/f.sql", create, "db/other/drop.sql", drop}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := version(tc.files...)
			var paths []string
			for i := 0; i < len(tc.files); i += 2 {
				paths = append(paths, tc.files[i])
			}
			an, err := sql.New().Analyze(v, tc.c, paths)
			if err != nil {
				t.Fatal(err)
			}
			if got := symbolIDs(an); !slices.Equal(got, tc.want) {
				t.Errorf("symbols %v, want %v (unread %v)", got, tc.want, unread(an))
			}
		})
	}
}

// viewpoint: behaviour
// target: sql.database.defKey
// An unqualified definition is in the configured default schema, else in the first schema of
// PostgreSQL's search path; a file without markers takes the dialect the other files' markers point
// to; a name with a catalog defines an object only in the configured catalog.
func TestWhereDefinitionsGo(t *testing.T) {
	withDefault := postgres()
	withDefault.Databases[0].DefaultSchema = "billing"
	withPath := postgres()
	withPath.Databases[0].SearchPath = []string{"app", "public"}
	withCatalog := postgres()
	withCatalog.Databases[0].Catalog = "main"
	cases := []struct {
		name  string
		c     config.Config
		files []string
		want  []string
	}{
		{"the default schema", withDefault, []string{"db/a.sql", "CREATE TABLE t (id int);"}, []string{".sql/billing.t"}},
		{"the search path", withPath, []string{"db/a.sql", "CREATE TABLE t (id int);"}, []string{".sql/app.t"}},
		{"the dialect of the other files", config.Config{},
			[]string{"db/a.sql", "CREATE FUNCTION f() RETURNS int AS $$ SELECT 1 $$ LANGUAGE sql;", "db/b.sql", "CREATE TABLE t (id int);"},
			[]string{".sql/public.f", ".sql/public.t"}},
		{"the configured catalog", withCatalog,
			[]string{"db/a.sql", "CREATE TABLE main.billing.t (id int);\nCREATE TABLE other.billing.u (id int);"},
			[]string{".sql/billing.t"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			an := analyze(t, sql.New(), version(tc.files...), tc.c)
			if got := symbolIDs(an); !slices.Equal(got, tc.want) {
				t.Errorf("symbols %v, want %v (unread %v)", got, tc.want, unread(an))
			}
		})
	}
}
