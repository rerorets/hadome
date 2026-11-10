package sql_test

import (
	"slices"
	"sort"
	"testing"

	"github.com/rerorets/hadome/internal/adapter/sql"
	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/facts"
)

// linkAt links one test at path, holding texts, against the version's files.
func linkAt(t *testing.T, c config.Analysis, path string, texts []facts.Text, files ...string) facts.Test {
	t.Helper()
	a := sql.New()
	v := version(files...)
	an := analyze(t, a, v, c)
	an.Tests = append(an.Tests, facts.Test{Name: "TestX", Path: path, Line: 1, Texts: texts})
	if err := a.Link(run(v), c, an); err != nil {
		t.Fatal(err)
	}
	return an.Tests[len(an.Tests)-1]
}

// viewpoint: behaviour
// target: sql.database.resolve
// A name in a test resolves the way the database finds it: an unqualified one in the search path,
// else in the default schema, else as the only object of that name; one with a catalog only in the
// configured catalog. Names in system schemas and names that start like built-ins count nothing.
func TestHowNamesInTestsResolve(t *testing.T) {
	pgFiles := []string{"db/a.sql", "CREATE TABLE public.accounts (id int);\nCREATE TABLE other.accounts (id int);\n" +
		"CREATE TABLE billing.ledgers (id int);\n"}
	withPath := postgres()
	withPath.Databases[0].SearchPath = []string{"app", "billing"}
	withCatalog := postgres()
	withCatalog.Databases[0].Catalog = "main"
	cases := []struct {
		name              string
		c                 config.Analysis
		files             []string
		text              string
		reads, unresolved int
	}{
		{"the default schema before another schema", postgres(), pgFiles, "SELECT id FROM accounts", 1, 0},
		{"the search path", withPath, pgFiles, "SELECT id FROM ledgers", 1, 0},
		{"only the search path", withPath, pgFiles, "SELECT id FROM accounts", 0, 1},
		{"the configured catalog", withCatalog, pgFiles, "SELECT id FROM main.billing.ledgers", 1, 0},
		{"another catalog", withCatalog, pgFiles, "SELECT id FROM other.billing.ledgers", 0, 1},
		{"a system catalog", postgres(), pgFiles, "SELECT relname FROM pg_catalog.pg_class", 0, 0},
		{"the information schema", postgres(), pgFiles, "SELECT table_name FROM information_schema.tables", 0, 0},
		{"a name like a built-in", postgres(), pgFiles, "SELECT pg_my_function()", 0, 0},
		{"a database without a default schema", dialect("mysql"),
			[]string{"db/a.sql", "CREATE TABLE accounts (id INT);\nCREATE TABLE other.accounts (id INT);\n"},
			"SELECT id FROM accounts", 1, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := linkAt(t, tc.c, "app/x_test.go", []facts.Text{text(tc.text)}, tc.files...)
			if got.Reads != tc.reads || got.Unresolved != tc.unresolved {
				t.Errorf("reads %d unresolved %d, want %d and %d", got.Reads, got.Unresolved, tc.reads, tc.unresolved)
			}
		})
	}
}

// viewpoint: behaviour
// target: sql.scopeArg
// The scope a call passes is the argument at the routine's scope parameter, by position or by name,
// whole even when it holds commas inside parentheses or brackets. A routine without a scope parameter
// is called with no scope.
func TestScopesPassedToRoutines(t *testing.T) {
	files := []string{"db/a.sql", schema + "CREATE FUNCTION billing.no_scope(n int) RETURNS int LANGUAGE sql AS $$ SELECT 1 $$;\n"}
	got := linkAt(t, postgres(), "app/x_test.go", []facts.Text{
		text("SELECT billing.is_active(f(1, 2), 60)"), text("SELECT billing.is_active(f(1, 3), 60)"),
		text("SELECT billing.is_active(ARRAY[1, 2], 60)"), text("SELECT billing.is_active(ARRAY[1, 3], 60)"),
		text("SELECT billing.is_active(ttl => 60, account_id => 7)"), text("SELECT billing.is_active(7, 60)"),
		text("SELECT billing.no_scope(1)"),
	}, files...)
	scopes := map[string]int{}
	for _, c := range got.Calls {
		scopes[c.Target] = c.Scopes
	}
	if scopes[".sql/billing.is_active"] != 5 || scopes[".sql/billing.no_scope"] != 0 || len(scopes) != 2 {
		t.Errorf("scopes %v, want 5 for is_active and 0 for no_scope", scopes)
	}
}

// viewpoint: behaviour
// target: sql.databasesFor
// A test's strings resolve against the databases whose tests globs hold the test, else against the
// databases with no tests globs; a name two of those databases have is unresolved.
func TestTestsUseTheirDatabases(t *testing.T) {
	create := "CREATE FUNCTION f() RETURNS int LANGUAGE sql AS $$ SELECT 1 $$;"
	files := []string{"db/main/a.sql", create, "db/other/a.sql", create}
	scoped := config.Analysis{Databases: []config.Database{
		{Name: "main", Dialect: "postgresql", Paths: []string{"db/main/**"}, Tests: []string{"app/**"}},
		{Name: "other", Dialect: "postgresql", Paths: []string{"db/other/**"}}}}
	unscoped := config.Analysis{Databases: []config.Database{
		{Name: "main", Dialect: "postgresql", Paths: []string{"db/main/**"}},
		{Name: "other", Dialect: "postgresql", Paths: []string{"db/other/**"}}}}
	cases := []struct {
		name, path string
		c          config.Analysis
		want       []string
		unresolved int
	}{
		{"a test the tests globs hold", "app/x_test.go", scoped, []string{".sql/main/public.f"}, 0},
		{"a test no tests globs hold", "svc/y_test.go", scoped, []string{".sql/other/public.f"}, 0},
		{"two databases without tests globs", "app/x_test.go", unscoped, nil, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := linkAt(t, tc.c, tc.path, []facts.Text{text("SELECT f()")}, files...)
			if !slices.Equal(targets(got), tc.want) || got.Unresolved != tc.unresolved {
				t.Errorf("calls %v unresolved %d, want %v and %d", targets(got), got.Unresolved, tc.want, tc.unresolved)
			}
		})
	}
}

// viewpoint: behaviour
// target: sql.merge
// A routine the test's language already found called is one call, its times added.
func TestSQLCallsMergeWithTheLanguagesCalls(t *testing.T) {
	a := sql.New()
	v := version("db/a.sql", "CREATE FUNCTION f() RETURNS int LANGUAGE sql AS $$ SELECT 1 $$;")
	an := analyze(t, a, v, postgres())
	an.Tests = []facts.Test{{Name: "TestX", Path: "app/x_test.go", Line: 1,
		Calls: []facts.Call{{Target: ".sql/public.f", Times: 1}}, Texts: []facts.Text{text("SELECT f()")}}}
	if err := a.Link(run(v), postgres(), an); err != nil {
		t.Fatal(err)
	}
	if c := an.Tests[0].Calls; len(c) != 1 || c[0].Times != 2 {
		t.Errorf("calls %+v, want one call of f twice", c)
	}
}

// viewpoint: behaviour
// target: sql.tapFramework.Tests
// A pgTAP or myTAP file is a test once it plans: its assertions count, pgTAP's own functions and
// built-ins count nothing, the objects it creates are its setup, a name that is neither is
// unresolved, and skip gives the reason it is written with (none for a count or no argument).
func TestTAPTests(t *testing.T) {
	an := analyze(t, sql.New(), version(
		"db/schema.sql", schema,
		"test/a.sql", "SELECT no_plan();\nCREATE TABLE fixture_t (id int);\nINSERT INTO fixture_t VALUES (1);\n"+
			"SELECT ok(billing.is_active(1, 2));\nSELECT diag('note');\nSELECT length('x');\nSELECT unknown_fn();\nSELECT other_unknown();\n"+
			"SELECT skip('not ready');\n",
		"test/b.sql", "SELECT plan(1);\nSELECT skip(2);\nSELECT pass();\n",
		"test/c.sql", "SELECT plan(1);\nSELECT skip();\n",
		"test/d.sql", "SELECT skip('no plan');\nSELECT pass();\n",
	), postgres())
	tests := map[string]facts.Test{}
	for _, ts := range an.Tests {
		tests[ts.Path] = ts
	}
	if ts := tests["test/a.sql"]; ts.Asserts != 1 || ts.Unresolved != 2 || ts.Reads != 0 ||
		!slices.Equal(targets(ts), []string{".sql/billing.is_active"}) || ts.Skip != (facts.Skip{Skipped: true, Reason: "not ready", Line: 9}) {
		t.Errorf("test/a.sql: %+v", ts)
	}
	if ts := tests["test/b.sql"]; ts.Asserts != 1 || ts.Skip != (facts.Skip{Skipped: true, Line: 2}) {
		t.Errorf("test/b.sql: %+v", ts)
	}
	if ts := tests["test/c.sql"]; ts.Skip != (facts.Skip{Skipped: true, Line: 2}) {
		t.Errorf("test/c.sql: %+v", ts)
	}
	if _, ok := tests["test/d.sql"]; ok || len(an.Tests) != 3 {
		t.Errorf("tests %v, want a, b and c", an.Tests)
	}
	my := analyze(t, sql.New(), version("db/schema.sql", "CREATE TABLE t (id INT);",
		"test/t.sql", "SELECT tap.plan(1);\nSELECT tap.ok(1, 'one');\n"), dialect("mysql"))
	if len(my.Tests) != 1 || my.Tests[0].Asserts != 1 || my.Tests[0].Unresolved != 0 {
		t.Errorf("myTAP: %+v, want one test with one assertion", my.Tests)
	}
}

// viewpoint: behaviour
// target: sql.EnabledDialects
// The dialects that can be chosen are the configuration's dialects, in any build.
func TestEnabledDialectsAreTheConfigurationsDialects(t *testing.T) {
	var want []string
	for _, v := range config.SQLDialects {
		want = append(want, v.Name)
	}
	sort.Strings(want)
	if got := sql.EnabledDialects(); !slices.Equal(got, want) {
		t.Errorf("enabled %v, want %v", got, want)
	}
}
