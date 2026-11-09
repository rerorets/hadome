package tests

import (
	"testing/fstest"

	"github.com/rerorets/hadome/internal/engine"
)

// The SQL cases use Go tests, which every build of hadome reads. The tests pass SQL to run, a helper
// of their file that stands for a database client.

const sqlFunctions = "CREATE FUNCTION billing.is_active(account_id int, ttl int) RETURNS boolean\n" +
	"LANGUAGE sql AS $$ SELECT true $$;\n"

const sqlTables = "CREATE TABLE billing.accounts (id int);\n" +
	"CREATE FUNCTION billing.stamp() RETURNS trigger AS $$ BEGIN RETURN NEW; END $$ LANGUAGE plpgsql;\n" +
	"CREATE TRIGGER stamp AFTER INSERT ON billing.accounts FOR EACH ROW EXECUTE FUNCTION billing.stamp();\n"

// sqlTree is a Go module whose database is db/*.sql, with the given Go test bodies in app/db_test.go.
func sqlTree(sqlFiles map[string]string, tests ...string) fstest.MapFS {
	t := fstest.MapFS{"go.mod": {Data: []byte(goMod)}, "app/app.go": {Data: []byte("// Package app serves accounts.\npackage app\n")}}
	for p, s := range sqlFiles {
		t[p] = &fstest.MapFile{Data: []byte(s)}
	}
	src := "package app_test\n\nimport (\n\t\"errors\"\n\t\"testing\"\n)\n\n" +
		"func run(query string, args ...any) error {\n\tif query == \"\" {\n\t\treturn errors.New(\"no query\")\n\t}\n\treturn nil\n}\n"
	for _, body := range tests {
		src += "\n" + body
	}
	t["app/db_test.go"] = &fstest.MapFile{Data: []byte(src)}
	return t
}

func functionsOnly() map[string]string { return map[string]string{"db/functions.sql": sqlFunctions} }

func sqlTest(name, query string) string {
	return "func " + name + "(t *testing.T) {\n\tif err := run(" + query + "); err != nil {\n\t\tt.Fatal(err)\n\t}\n}\n"
}

var activeTest = sqlTest("TestActive", "\"SELECT billing.is_active(1, 3600)\"")

const otherTest = "func TestNothing(t *testing.T) {\n\tif err := run(\"x\"); err != nil {\n\t\tt.Fatal(err)\n\t}\n}\n"

func sqlScopeCases() []engine.Case {
	moved := map[string]string{"db/V1__functions.sql": sqlFunctions,
		"db/V2__again.sql": "CREATE OR REPLACE FUNCTION billing.is_active(account_id int, ttl int) RETURNS boolean\nLANGUAGE sql AS $$ SELECT false $$;\n"}
	renamed := map[string]string{"db/V1__functions.sql": sqlFunctions,
		"db/V2__rename.sql": "ALTER FUNCTION billing.is_active(int, int) RENAME TO is_open;\n"}
	tables := map[string]string{"db/tables.sql": sqlTables}
	insert := sqlTest("TestInsert", "\"INSERT INTO billing.accounts (id) VALUES (1)\"")
	pgtap := func(with bool) fstest.MapFS {
		t := sqlTree(functionsOnly(), otherTest)
		if with {
			t["test/accounts.sql"] = &fstest.MapFile{Data: []byte("SELECT plan(1);\nSELECT ok(billing.is_active(1, 3600));\nSELECT * FROM finish();\n")}
		}
		return t
	}
	return []engine.Case{
		{Name: "deleting the only Go test guarding an SQL function fails",
			Base: sqlTree(functionsOnly(), activeTest), Head: sqlTree(functionsOnly(), otherTest), WantFail: []string{"tests/scope-shrunk"}},
		{Name: "deleting an SQL function and its test passes",
			Base: sqlTree(functionsOnly(), activeTest), Head: sqlTree(map[string]string{"db/functions.sql": "SELECT 1;\n"}, otherTest)},
		{Name: "redefining the SQL function in a new migration passes",
			Base: sqlTree(map[string]string{"db/V1__functions.sql": sqlFunctions}, activeTest), Head: sqlTree(moved, activeTest)},
		{Name: "renaming the SQL function passes",
			Base: sqlTree(map[string]string{"db/V1__functions.sql": sqlFunctions}, activeTest), Head: sqlTree(renamed, otherTest)},
		{Name: "deleting the only test writing to a table with a trigger fails",
			Base: sqlTree(tables, insert), Head: sqlTree(tables, otherTest), WantFail: []string{"tests/scope-shrunk"}},
		{Name: "deleting the only pgTAP test fails", Base: pgtap(true), Head: pgtap(false), WantFail: []string{"tests/scope-shrunk"}},
	}
}

func sqlGuardsCases() []engine.Case {
	add := func(name, test string, fails bool) engine.Case {
		c := engine.Case{Name: name, Base: sqlTree(functionsOnly(), activeTest), Head: sqlTree(functionsOnly(), activeTest, test)}
		if fails {
			c.WantFail = []string{"tests/guards-nothing"}
		}
		return c
	}
	constant := "const isActive = \"SELECT billing.is_active(2, 60)\"\n\n" + sqlTest("TestConst", "isActive")
	helper := "func active(id int) error {\n\treturn run(\"SELECT billing.is_active($1, 60)\", id)\n}\n\n" +
		"func TestHelper(t *testing.T) {\n\tif err := active(3); err != nil {\n\t\tt.Fatal(err)\n\t}\n}\n"
	noSQL := sqlTree(nil, activeTest)
	return []engine.Case{
		add("a test passing SQL that calls an SQL function passes", sqlTest("TestOther", "\"SELECT billing.is_active(9, 60)\""), false),
		add("a test passing SQL held in a constant passes", constant, false),
		add("a test passing SQL inside a helper passes", helper, false),
		add("a new test passing SQL that calls only built-ins fails", sqlTest("TestBuiltin", "\"SELECT now() IS NOT NULL\""), true),
		{Name: "in a repository without .sql files a test passing SQL fails", Base: sqlTree(nil, otherTest), Head: noSQL,
			WantFail: []string{"tests/guards-nothing"}},
	}
}

func sqlUnknownCases() []engine.Case {
	add := func(name, test string, review bool) engine.Case {
		c := engine.Case{Name: name, Base: sqlTree(functionsOnly(), activeTest), Head: sqlTree(functionsOnly(), activeTest, test)}
		if review {
			c.WantReview = []string{"tests/target-unknown"}
		}
		return c
	}
	dynamic := "func TestDynamic(t *testing.T) {\n\tq := query()\n\tif err := run(q); err != nil {\n\t\tt.Fatal(err)\n\t}\n}\n\n" +
		"func query() string { return \"SELECT 1\" }\n"
	sinks := add("a new test passing only a value that is not a literal to a call that takes SQL needs review", dynamic, true)
	for _, side := range []fstest.MapFS{sinks.Base.(fstest.MapFS), sinks.Head.(fstest.MapFS)} {
		side[".hadome.yml"] = &fstest.MapFile{Data: []byte("schema: 1\nrules:\n  tests:\n    sql-sinks:\n      go: [run]\n")}
	}
	return []engine.Case{
		add("a new test naming only names absent from the database needs review", sqlTest("TestAbsent", "\"SELECT billing.is_closed(1)\""), true),
		sinks,
		add("a new test passing SQL with a driver's parameter where a value goes passes", sqlTest("TestParam", "\"SELECT billing.is_active($1, $2)\", 1, 2"), false),
	}
}

func sqlClockCases() []engine.Case {
	return []engine.Case{
		{Name: "now() in test SQL fails", Base: sqlTree(functionsOnly(), activeTest),
			Head:     sqlTree(functionsOnly(), activeTest, sqlTest("TestNow", "\"SELECT billing.is_active(1, 3600) AND now() > now()\"")),
			WantFail: []string{"tests/no-clock"}},
		{Name: "now() in a string that is not SQL passes", Base: sqlTree(functionsOnly(), activeTest),
			Head: sqlTree(functionsOnly(), activeTest, "func TestLog(t *testing.T) {\n\tt.Log(\"call now() later\")\n\tif err := run(\"SELECT billing.is_active(1, 2)\"); err != nil {\n\t\tt.Fatal(err)\n\t}\n}\n")},
	}
}

func sqlSleepCases() []engine.Case {
	return []engine.Case{
		{Name: "pg_sleep in test SQL fails", Base: sqlTree(functionsOnly(), activeTest),
			Head:     sqlTree(functionsOnly(), activeTest, sqlTest("TestWait", "\"SELECT billing.is_active(1, 3600) FROM pg_sleep(1)\"")),
			WantFail: []string{"tests/no-sleep"}},
	}
}

func sqlNoAssertionCases() []engine.Case {
	plan := func(body string) fstest.MapFS {
		t := sqlTree(functionsOnly(), activeTest)
		t["test/accounts.sql"] = &fstest.MapFile{Data: []byte("SELECT plan(1);\n" + body + "SELECT * FROM finish();\n")}
		return t
	}
	return []engine.Case{
		{Name: "a pgTAP file without asserts fails", Base: sqlTree(functionsOnly(), activeTest),
			Head: plan("SELECT billing.is_active(1, 3600);\n"), WantFail: []string{"tests/no-assertion"}},
		{Name: "a pgTAP file with an assert passes", Base: sqlTree(functionsOnly(), activeTest),
			Head: plan("SELECT ok(billing.is_active(1, 3600));\n")},
	}
}

func sqlSkipCases() []engine.Case {
	plan := func(skip string) fstest.MapFS {
		t := sqlTree(functionsOnly(), activeTest)
		t["test/accounts.sql"] = &fstest.MapFile{Data: []byte("SELECT plan(1);\n" + skip + "SELECT * FROM finish();\n")}
		return t
	}
	return []engine.Case{
		{Name: "a pgTAP skip without a reason fails", Base: plan("SELECT ok(billing.is_active(1, 3600));\n"),
			Head: plan("SELECT skip(1);\n"), WantFail: []string{"tests/skip-without-reason"}},
		{Name: "a pgTAP skip with a reason passes", Base: plan("SELECT ok(billing.is_active(1, 3600));\n"),
			Head: plan("SELECT skip('needs the billing schema', 1);\n")},
	}
}
