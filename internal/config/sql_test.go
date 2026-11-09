package config

import (
	"strings"
	"testing"
)

const sqlConfig = `schema: 1
rules:
  tests:
    sql-sinks:
      typescript: [db.run]
      python: [run_sql]
sql:
  databases:
    - name: main
      dialect: postgresql
      paths: ["db/**/*.sql"]
      search-path: [billing, public]
      order: ["db/migrations/**", "db/views/**"]
    - name: analytics
      dialect: postgresql
      catalog: example-project
      default-schema: reports
      paths: ["warehouse/**/*.sql"]
      tests: ["warehouse/tests/**"]
`

func TestSQLDatabasesAndSinksAreRead(t *testing.T) {
	c := mustLoad(t, sqlConfig, t.TempDir())
	if len(c.Databases) != 2 {
		t.Fatalf("want two databases in order, got %+v", c.Databases)
	}
	main, an := c.Databases[0], c.Databases[1]
	if main.Name != "main" || main.Dialect != "postgresql" || strings.Join(main.Paths, ",") != "db/**/*.sql" ||
		strings.Join(main.SearchPath, ",") != "billing,public" || strings.Join(main.Order, ",") != "db/migrations/**,db/views/**" {
		t.Errorf("main: %+v", main)
	}
	if an.Name != "analytics" || an.Dialect != "postgresql" || an.Catalog != "example-project" ||
		an.DefaultSchema != "reports" || strings.Join(an.Tests, ",") != "warehouse/tests/**" {
		t.Errorf("analytics: %+v", an)
	}
	if strings.Join(c.SQLSinks["typescript"], ",") != "db.run" || strings.Join(c.SQLSinks["python"], ",") != "run_sql" ||
		len(c.SQLSinks["go"]) != 0 {
		t.Errorf("sql-sinks: %v", c.SQLSinks)
	}
}

func TestWithoutSQLThereIsNoDatabaseAndNoSink(t *testing.T) {
	c := mustLoad(t, "schema: 1\n", t.TempDir())
	if len(c.Databases) != 0 || len(c.SQLSinks) != 0 {
		t.Errorf("want neither, got %+v %v", c.Databases, c.SQLSinks)
	}
}

// Invariant 2 of docs/design/configuration.md: an unknown dialect, or two databases with one name,
// never runs a check.
func TestSQLDatabasesAreValidated(t *testing.T) {
	dir := t.TempDir()
	for yml, key := range map[string]string{
		"    - name: main\n      dialect: db2\n":           "sql.databases[0].dialect",
		"    - name: main\n    - name: main\n":             "sql.databases[1]",
		"    - dialect: postgresql\n":                      "sql.databases[0].name",
		"    - name: Main\n":                               "sql.databases[0].name",
		"    - name: main\n      paths: [\"db/[x\"]\n":     "sql.databases[0].paths[0]",
		"    - name: main\n      search-paths: [public]\n": "sql.databases[0].search-paths",
	} {
		es := loadErrors(t, "schema: 1\nsql:\n  databases:\n"+yml, dir)
		if es[0].Key != key {
			t.Errorf("%q: want an error at %s, got %v", yml, key, es)
		}
	}
	if es := loadErrors(t, "schema: 1\nrules:\n  tests:\n    sql-sinks:\n      cobol: [run]\n", dir); es[0].Key != "rules.tests.sql-sinks.cobol" {
		t.Errorf("a language that hands over no SQL: %v", es)
	}
}

// Invariant 5 of docs/design/configuration.md: a change that loosens the rules needs review. Keys
// that change how names resolve go to review whichever way they change; adding paths reads more.
func TestSQLChangesThatNeedReview(t *testing.T) {
	dir := t.TempDir()
	base := mustLoad(t, sqlConfig, dir)
	cases := []struct {
		name, head string
		wantLooser int
	}{
		{"unchanged", sqlConfig, 0},
		{"removing a dialect", strings.Replace(sqlConfig, "      dialect: postgresql\n      catalog", "      catalog", 1), 1},
		{"changing the search path", strings.Replace(sqlConfig, "[billing, public]", "[public, billing]", 1), 1},
		{"changing the default schema", strings.Replace(sqlConfig, "default-schema: reports", "default-schema: other", 1), 1},
		{"removing the catalog", strings.Replace(sqlConfig, "      catalog: example-project\n", "", 1), 1},
		{"changing the order", strings.Replace(sqlConfig, `"db/migrations/**", "db/views/**"`, `"db/views/**", "db/migrations/**"`, 1), 1},
		{"adding test globs", strings.Replace(sqlConfig, `tests: ["warehouse/tests/**"]`, `tests: ["warehouse/tests/**", "it/**"]`, 1), 1},
		{"removing a paths entry", strings.Replace(sqlConfig, `paths: ["db/**/*.sql"]`, `paths: []`, 1), 1},
		{"adding a paths entry", strings.Replace(sqlConfig, `paths: ["db/**/*.sql"]`, `paths: ["db/**/*.sql", "sql/**"]`, 1), 0},
		{"removing a database", sqlConfig[:strings.Index(sqlConfig, "    - name: analytics")], 1},
		{"adding a database", sqlConfig + "    - name: third\n", 1},
		{"adding a sink", strings.Replace(sqlConfig, "[db.run]", "[db.run, db.all]", 1), 1},
		{"removing a sink", strings.Replace(sqlConfig, "      python: [run_sql]\n", "", 1), 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := looser(Diff(base, mustLoad(t, tc.head, dir))); len(got) != tc.wantLooser {
				t.Errorf("want %d looser changes, got %v", tc.wantLooser, got)
			}
		})
	}
}

// A change inside one database names the database and the key it is under.
func TestASQLChangeNamesItsDatabase(t *testing.T) {
	dir := t.TempDir()
	base := mustLoad(t, sqlConfig, dir)
	head := mustLoad(t, strings.Replace(sqlConfig, "default-schema: reports", "default-schema: other", 1), dir)
	ch := Diff(base, head)
	if len(ch) != 1 || ch[0].Key != "sql.databases.default-schema" || ch[0].Removed != "name=analytics: reports" ||
		ch[0].Added != "name=analytics: other" || ch[0].Line == 0 {
		t.Errorf("got %+v", ch)
	}
}
