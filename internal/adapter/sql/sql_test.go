package sql_test

import (
	"slices"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/adapter/sql"
	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
)

// version is a repository of the given files: path, then content.
func version(files ...string) engine.Version {
	tree := fstest.MapFS{}
	for i := 0; i+1 < len(files); i += 2 {
		tree[files[i]] = &fstest.MapFile{Data: []byte(files[i+1])}
	}
	return engine.Version{FS: tree}
}

// analyze reads every .sql file of the version.
func analyze(t *testing.T, a *sql.Adapter, v engine.Version, c config.Config) *facts.Analysis {
	t.Helper()
	var paths []string
	for p := range v.FS.(fstest.MapFS) {
		if a.Claims(p) {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	an, err := a.Analyze(v, c, paths)
	if err != nil {
		t.Fatal(err)
	}
	return an
}

func symbolIDs(an *facts.Analysis) []string {
	var out []string
	for _, s := range an.Symbols {
		out = append(out, s.ID)
	}
	return out
}

func unread(an *facts.Analysis) []string {
	var out []string
	for _, f := range an.Files {
		if f.Unread {
			out = append(out, f.Path)
		}
	}
	return out
}

func postgres() config.Config {
	return config.Config{Databases: []config.Database{{Name: "main", Dialect: "postgresql"}}}
}

func dialect(name string) config.Config {
	return config.Config{Databases: []config.Database{{Name: "main", Dialect: name}}}
}

// viewpoint: behaviour
// target: sql.Adapter.Claims
// A file is SQL by its extension alone.
func TestSQLClaimsFilesEndingInSQL(t *testing.T) {
	a := sql.New()
	for p, want := range map[string]bool{"db/schema.sql": true, "db/SCHEMA.SQL": true, "db/schema.sql.tmpl": false, "db/seed.psql": false} {
		if got := a.Claims(p); got != want {
			t.Errorf("Claims(%q) = %v, want %v", p, got, want)
		}
	}
}

// viewpoint: behaviour
// target: sql.Adapter.Analyze
// guards: same-rules-every-language
// Each dialect's definitions become symbols named schema.name inside the database's unit, with the
// dialect's rules for case and quotes, and the kinds of objects that run logic or hold values.
func TestDefinitionsBecomeSymbolsInEachDialect(t *testing.T) {
	cases := []struct {
		dialect, src string
		want         []string
	}{
		{"postgresql", "CREATE FUNCTION billing.is_active(account_id int) RETURNS boolean LANGUAGE sql AS $$ SELECT true $$;\n" +
			"CREATE VIEW reports.daily AS SELECT 1 AS n;\nCREATE TABLE accounts (id int);\nCREATE INDEX accounts_id ON accounts (id);\n",
			[]string{".sql/billing.is_active", ".sql/public.accounts", ".sql/reports.daily"}},
		{"mysql", "CREATE TABLE `accounts` (id INT AUTO_INCREMENT PRIMARY KEY) ENGINE=InnoDB;\n" +
			"CREATE PROCEDURE billing.Close_Account(IN account_id INT) BEGIN UPDATE accounts SET id = id; END;\n",
			[]string{".sql/accounts", ".sql/billing.close_account"}},
		{"sqlite", "CREATE TABLE accounts (id INTEGER PRIMARY KEY AUTOINCREMENT);\nCREATE VIEW Open_Accounts AS SELECT id FROM accounts;\n",
			[]string{".sql/main.accounts", ".sql/main.open_accounts"}},
		{"tsql", "CREATE PROCEDURE [dbo].[CloseAccount] @AccountId int AS BEGIN SELECT 1; SELECT 2; END\nGO\n" +
			"CREATE TABLE Accounts (id int)\nGO\n",
			[]string{".sql/dbo.accounts", ".sql/dbo.closeaccount"}},
		{"oracle", "CREATE OR REPLACE FUNCTION billing.is_active(p_id NUMBER) RETURN NUMBER IS BEGIN RETURN 1; END;\n/\n" +
			"CREATE TABLE \"Mixed\" (id NUMBER);\n",
			[]string{".sql/BILLING.IS_ACTIVE", ".sql/Mixed"}},
	}
	for _, c := range cases {
		an := analyze(t, sql.New(), version("db/schema.sql", c.src), dialect(c.dialect))
		if got := symbolIDs(an); !slices.Equal(got, c.want) {
			t.Errorf("%s: symbols %v, want %v (unread %v)", c.dialect, got, c.want, unread(an))
		}
	}
}

// viewpoint: behaviour
// target: sql.Adapter.Analyze
// Functions, procedures, views and triggers are routines or views a test calls; tables and sequences
// are relations it reads.
func TestObjectKindsDecideTheSymbolKind(t *testing.T) {
	an := analyze(t, sql.New(), version("db/schema.sql",
		"CREATE FUNCTION f() RETURNS int AS $$ SELECT 1 $$ LANGUAGE sql;\nCREATE VIEW v AS SELECT 1 AS n;\n"+
			"CREATE TABLE t (id int);\nCREATE SEQUENCE s;\n"), postgres())
	want := map[string]facts.SymbolKind{".sql/public.f": facts.SymbolRoutine, ".sql/public.v": facts.SymbolRelation,
		".sql/public.t": facts.SymbolRelation, ".sql/public.s": facts.SymbolRelation}
	for _, s := range an.Symbols {
		if want[s.ID] != s.Kind {
			t.Errorf("%s is %s, want %s", s.ID, s.Kind, want[s.ID])
		}
		delete(want, s.ID)
	}
	if len(want) > 0 {
		t.Errorf("missing symbols %v", want)
	}
}

// viewpoint: behaviour
// target: sql.Adapter.Analyze
// The objects that remain after applying the files in order are the symbols: a later drop removes
// one, V10__ comes after V2__, and a rename makes the object a symbol under its new name.
func TestMigrationsLeaveTheObjectsThatRemain(t *testing.T) {
	cases := []struct {
		name  string
		files []string
		want  []string
	}{
		{"a dropped function is not a symbol",
			[]string{"db/V1__a.sql", "CREATE FUNCTION f() RETURNS int AS $$ SELECT 1 $$ LANGUAGE sql;",
				"db/V2__b.sql", "DROP FUNCTION f();"}, nil},
		{"V10__ is applied after V2__",
			[]string{"db/V2__create.sql", "CREATE FUNCTION f() RETURNS int AS $$ SELECT 1 $$ LANGUAGE sql;",
				"db/V10__drop.sql", "DROP FUNCTION IF EXISTS f();"}, nil},
		{"a create after a drop remains",
			[]string{"db/V10__create.sql", "CREATE FUNCTION f() RETURNS int AS $$ SELECT 1 $$ LANGUAGE sql;",
				"db/V2__drop.sql", "DROP FUNCTION IF EXISTS f();"}, []string{".sql/public.f"}},
		{"a rename keeps the schema and takes the new name",
			[]string{"db/V1__a.sql", "CREATE TABLE billing.accounts (id int);\nALTER TABLE billing.accounts RENAME TO ledgers;"},
			[]string{".sql/billing.ledgers"}},
		{"a drop in a file whose order is not known leaves the object",
			[]string{"db/functions/f.sql", "CREATE FUNCTION f() RETURNS int AS $$ SELECT 1 $$ LANGUAGE sql;",
				"db/cleanup/drop.sql", "DROP FUNCTION f();"}, []string{".sql/public.f"}},
		{"the undo side of a migration is not read",
			[]string{"db/1_init.up.sql", "CREATE TABLE t (id int);", "db/1_init.down.sql", "DROP TABLE t;",
				"db/2_more.sql", "-- migrate:up\nCREATE TABLE u (id int);\n-- migrate:down\nDROP TABLE t;\n"},
			[]string{".sql/public.t", ".sql/public.u"}},
		{"overloads are one symbol, kept until the last is dropped",
			[]string{"db/V1__a.sql", "CREATE FUNCTION f(a int) RETURNS int AS $$ SELECT 1 $$ LANGUAGE sql;\n" +
				"CREATE FUNCTION f(a text) RETURNS int AS $$ SELECT 2 $$ LANGUAGE sql;"},
			[]string{".sql/public.f"}},
		{"no symbol comes from a CREATE inside a function body",
			[]string{"db/V1__a.sql", "CREATE FUNCTION f() RETURNS void AS $$ BEGIN EXECUTE 'CREATE TABLE hidden (id int)'; " +
				"CREATE TABLE inner_t (id int); END $$ LANGUAGE plpgsql;"},
			[]string{".sql/public.f"}},
	}
	for _, c := range cases {
		an := analyze(t, sql.New(), version(c.files...), postgres())
		if got := symbolIDs(an); !slices.Equal(got, c.want) {
			t.Errorf("%s: symbols %v, want %v (unread %v)", c.name, got, c.want, unread(an))
		}
	}
}

// viewpoint: behaviour
// target: sql.Adapter.Analyze
// What the client reads splits a file before the grammar: GO between T-SQL batches, DELIMITER around
// MySQL bodies, / after Oracle blocks.
func TestClientSeparatorsSplitStatements(t *testing.T) {
	cases := []struct {
		dialect, src string
		want         []string
	}{
		{"tsql", "CREATE PROCEDURE dbo.a AS SELECT 1\nGO\nCREATE PROCEDURE dbo.b AS SELECT 2\nGO 2\n",
			[]string{".sql/dbo.a", ".sql/dbo.b"}},
		{"mysql", "DELIMITER //\nCREATE PROCEDURE a() BEGIN SELECT 1; SELECT 2; END //\nDELIMITER ;\nCREATE TABLE t (id INT);\n",
			[]string{".sql/a", ".sql/t"}},
		{"oracle", "CREATE OR REPLACE PROCEDURE billing.a IS BEGIN NULL; NULL; END;\n/\n" +
			"CREATE OR REPLACE PACKAGE billing.accounts_pkg AS\n  FUNCTION is_active(p NUMBER) RETURN NUMBER;\n" +
			"  PROCEDURE close_it(p NUMBER);\nEND accounts_pkg;\n/\n" +
			"CREATE OR REPLACE PACKAGE BODY billing.accounts_pkg AS\n  FUNCTION is_active(p NUMBER) RETURN NUMBER IS BEGIN RETURN 1; END;\n" +
			"  PROCEDURE close_it(p NUMBER) IS BEGIN NULL; END;\n  PROCEDURE hidden IS BEGIN NULL; END;\nEND accounts_pkg;\n/\n",
			[]string{".sql/BILLING.A", ".sql/BILLING.ACCOUNTS_PKG.CLOSE_IT", ".sql/BILLING.ACCOUNTS_PKG.IS_ACTIVE"}},
		{"postgresql", "\\set ON_ERROR_STOP on\nCOPY t (id) FROM stdin;\n1\n2\n\\.\nCREATE TABLE t (id int);\n",
			[]string{".sql/public.t"}},
	}
	for _, c := range cases {
		an := analyze(t, sql.New(), version("db/schema.sql", c.src), dialect(c.dialect))
		if got := symbolIDs(an); !slices.Equal(got, c.want) {
			t.Errorf("%s: symbols %v, want %v (unread %v)", c.dialect, got, c.want, unread(an))
		}
	}
}

// viewpoint: behaviour
// target: sql.Adapter.Analyze
// The dialect comes from the configuration, else from markers. A file whose markers point to two
// dialects, or none in a database with no other marked file, is unread, and so is a file with a
// statement the grammar cannot read.
func TestDialectsAreNeverGuessed(t *testing.T) {
	cases := []struct {
		name  string
		c     config.Config
		files []string
		want  []string
	}{
		{"markers of two dialects", config.Config{},
			[]string{"db/a.sql", "CREATE TABLE `t` (id INT) ENGINE=InnoDB;\nCREATE FUNCTION f() RETURNS int AS $$ SELECT 1 $$ LANGUAGE sql;"},
			[]string{"db/a.sql"}},
		{"the configured dialect wins over markers", dialect("postgresql"),
			[]string{"db/a.sql", "CREATE TABLE t (id int);\nSELECT 1::text;"}, nil},
		{"a file without markers takes the dialect of the database's other files", config.Config{},
			[]string{"db/a.sql", "CREATE FUNCTION f() RETURNS int AS $$ SELECT 1 $$ LANGUAGE sql;", "db/b.sql", "CREATE TABLE t (id int);"},
			nil},
		{"a database whose files have no marker", config.Config{}, []string{"db/a.sql", "CREATE TABLE t (id int);"}, []string{"db/a.sql"}},
		{"a statement the grammar cannot read", postgres(),
			[]string{"db/a.sql", "CREATE TABLE t (id int);\nCREATE TABLE u (id int;\n"}, []string{"db/a.sql"}},
	}
	for _, c := range cases {
		an := analyze(t, sql.New(), version(c.files...), c.c)
		if got := unread(an); !slices.Equal(got, c.want) {
			t.Errorf("%s: unread %v, want %v", c.name, got, c.want)
		}
	}
}

// viewpoint: behaviour
// target: sql.Adapter.Analyze
// Test .sql files, fixtures and files that are not definitions give no product symbols.
func TestOnlyProductFilesDefineSymbols(t *testing.T) {
	an := analyze(t, sql.New(), version(
		"db/schema.sql", "CREATE TABLE accounts (id int);",
		"tests/fixtures.sql", "CREATE TABLE fixture (id int);",
		"warehouse/dbt_project.yml", "name: w\n",
		"warehouse/models/m.sql", "select * from {{ ref('x') }}",
		"db/revert/a.sql", "DROP TABLE accounts;", "sqitch.plan", "%project=a\n",
	), postgres())
	if got, want := symbolIDs(an), []string{".sql/public.accounts"}; !slices.Equal(got, want) {
		t.Errorf("symbols %v, want %v", got, want)
	}
	roles := map[string]facts.Role{}
	for _, f := range an.Files {
		roles[f.Path] = f.Role
	}
	for p, want := range map[string]facts.Role{"db/schema.sql": facts.RoleSource, "tests/fixtures.sql": facts.RoleTest,
		"warehouse/models/m.sql": facts.RoleOther, "db/revert/a.sql": facts.RoleOther} {
		if roles[p] != want {
			t.Errorf("%s has role %s, want %s", p, roles[p], want)
		}
	}
}

// viewpoint: behaviour
// target: sql.Adapter.Analyze
// Every .sql file uses the database, an input and output API, so a database unit is never pure.
func TestSQLFilesUseTheDatabase(t *testing.T) {
	an := analyze(t, sql.New(), version("db/schema.sql", "CREATE TABLE accounts (id int);"), postgres())
	if len(an.Imports) != 1 || !an.Imports[0].IO || !an.Imports[0].Global || an.Imports[0].FromUnit != ".sql" {
		t.Errorf("imports %+v, want one global input and output use from .sql", an.Imports)
	}
}

// linked reads the version's .sql files, then links one test holding the texts.
func linked(t *testing.T, c config.Config, texts []facts.Text, files ...string) facts.Test {
	t.Helper()
	a := sql.New()
	v := version(files...)
	an := analyze(t, a, v, c)
	an.Tests = append(an.Tests, facts.Test{Name: "TestX", Path: "app/x_test.go", Line: 1, Texts: texts})
	if err := a.Link(v, c, an); err != nil {
		t.Fatal(err)
	}
	return an.Tests[len(an.Tests)-1]
}

func text(value string) facts.Text {
	return facts.Text{Path: "app/x_test.go", Line: 3, Value: value, Times: 1}
}

func targets(t facts.Test) []string {
	var out []string
	for _, c := range t.Calls {
		out = append(out, c.Target)
	}
	return out
}

const schema = "CREATE FUNCTION billing.is_active(account_id int, ttl int) RETURNS boolean LANGUAGE sql AS $$ SELECT true $$;\n" +
	"CREATE TABLE billing.accounts (id int, is_active boolean);\n" +
	"CREATE VIEW reports.daily AS SELECT 1 AS n;\n"

// viewpoint: behaviour
// target: sql.Adapter.Link
// guards: same-rules-every-language
// SQL strings in tests call the routines and views they name and read the tables, wherever the test
// holds them; names that are not objects, holes where a name goes and values that are not literals
// are unresolved; built-ins count nothing.
func TestTestStringsBecomeCallsAndReads(t *testing.T) {
	cases := []struct {
		name       string
		texts      []facts.Text
		calls      []string
		reads      int
		unresolved int
	}{
		{"a function call", []facts.Text{text("SELECT billing.is_active(1, 3600) AS ok")}, []string{".sql/billing.is_active"}, 0, 0},
		{"a view", []facts.Text{text("select n from reports.daily")}, []string{".sql/reports.daily"}, 0, 0},
		{"a table", []facts.Text{text("INSERT INTO billing.accounts (id) VALUES (1)")}, nil, 1, 0},
		{"a hole where a value goes", []facts.Text{{Path: "a", Line: 1, Value: "SELECT billing.is_active(, 3600)", Holes: []int{25}, Times: 1}},
			[]string{".sql/billing.is_active"}, 0, 0},
		{"a hole where a name goes", []facts.Text{{Path: "a", Line: 1, Value: "SELECT * FROM ", Holes: []int{14}, Times: 1}}, nil, 0, 1},
		{"a value that is not a literal", []facts.Text{{Path: "a", Line: 1, Dynamic: true, Sink: facts.TextSinkSQL, Times: 1}}, nil, 0, 1},
		{"a name absent from the database", []facts.Text{text("SELECT * FROM ledgers")}, nil, 0, 1},
		{"only built-ins", []facts.Text{text("SELECT 1"), text("SELECT now(), lower('A')")}, nil, 0, 0},
		{"a column is not a call", []facts.Text{text("SELECT a.is_active FROM billing.accounts a")}, nil, 1, 0},
		{"a WITH name hides", []facts.Text{text("WITH accounts AS (SELECT 1 AS id) SELECT id FROM accounts")}, nil, 0, 0},
		{"a string that is not SQL", []facts.Text{text("select a plan"), text("is_active")}, nil, 0, 0},
		{"a driver's parameters", []facts.Text{text("SELECT billing.is_active($1, $2)")}, []string{".sql/billing.is_active"}, 0, 0},
		{"a string passed to a call that takes SQL without a statement head", []facts.Text{{Path: "a", Line: 1,
			Value: "{call billing.is_active(?, ?)}", Sink: facts.TextSinkSQL, Times: 1}}, []string{".sql/billing.is_active"}, 0, 0},
		{"a temporary table the test creates hides the product's", []facts.Text{text("CREATE TEMP TABLE accounts (id int)"),
			text("SELECT id FROM accounts")}, nil, 0, 0},
	}
	for _, c := range cases {
		got := linked(t, postgres(), c.texts, "db/schema.sql", schema)
		if !slices.Equal(targets(got), c.calls) || got.Reads != c.reads || got.Unresolved != c.unresolved {
			t.Errorf("%s: calls %v reads %d unresolved %d, want %v %d %d", c.name, targets(got), got.Reads, got.Unresolved,
				c.calls, c.reads, c.unresolved)
		}
	}
}

// viewpoint: behaviour
// target: sql.Adapter.Link
// Unqualified names resolve by the search order, the default schema, or the only object of that
// name; an unqualified name two schemas hold is unresolved.
func TestUnqualifiedNamesResolveAsTheDatabaseDoes(t *testing.T) {
	two := "CREATE FUNCTION billing.close(account_id int) RETURNS int AS $$ SELECT 1 $$ LANGUAGE sql;\n" +
		"CREATE FUNCTION audit.close(account_id int) RETURNS int AS $$ SELECT 2 $$ LANGUAGE sql;\n"
	got := linked(t, postgres(), []facts.Text{text("SELECT close(1)")}, "db/a.sql", two)
	if got.Unresolved != 1 || len(got.Calls) != 0 {
		t.Errorf("ambiguous: calls %v unresolved %d, want none and 1", targets(got), got.Unresolved)
	}
	c := config.Config{Databases: []config.Database{{Name: "main", Dialect: "postgresql", SearchPath: []string{"audit", "billing"}}}}
	got = linked(t, c, []facts.Text{text("SELECT close(1)")}, "db/a.sql", two)
	if want := []string{".sql/audit.close"}; !slices.Equal(targets(got), want) {
		t.Errorf("search order: calls %v, want %v", targets(got), want)
	}
	got = linked(t, postgres(), []facts.Text{text("SELECT is_active(1, 2)")}, "db/a.sql", schema)
	if want := []string{".sql/billing.is_active"}; !slices.Equal(targets(got), want) {
		t.Errorf("only one: calls %v, want %v", targets(got), want)
	}
}

// viewpoint: behaviour
// target: sql.Adapter.Link
// Names are compared by each dialect's rules: quotes keep case in PostgreSQL, Oracle upper-cases
// unquoted names, SQL Server's [dbo].[Proc] is dbo.proc.
func TestNamesFoldByTheDialectsRules(t *testing.T) {
	got := linked(t, postgres(), []facts.Text{text(`SELECT * FROM "Billing"."Accounts"`), text("SELECT * FROM BILLING.ACCOUNTS")},
		"db/a.sql", schema)
	if got.Reads != 1 || got.Unresolved != 1 {
		t.Errorf("postgresql: reads %d unresolved %d, want 1 and 1", got.Reads, got.Unresolved)
	}
	got = linked(t, dialect("oracle"), []facts.Text{text("SELECT billing.is_active(1) FROM dual")},
		"db/a.sql", "CREATE FUNCTION BILLING.IS_ACTIVE(p NUMBER) RETURN NUMBER IS BEGIN RETURN 1; END;\n/\n")
	if want := []string{".sql/BILLING.IS_ACTIVE"}; !slices.Equal(targets(got), want) {
		t.Errorf("oracle: calls %v, want %v", targets(got), want)
	}
	got = linked(t, dialect("tsql"), []facts.Text{text("EXEC [dbo].[CloseAccount] @AccountId = 1")},
		"db/a.sql", "CREATE PROCEDURE dbo.closeaccount @AccountId int AS SELECT 1\nGO\n")
	if want := []string{".sql/dbo.closeaccount"}; !slices.Equal(targets(got), want) {
		t.Errorf("tsql: calls %v, want %v", targets(got), want)
	}
}

// viewpoint: behaviour
// target: sql.Adapter.Link
// A write to a table runs its triggers for that write: the trigger itself, or the function a
// PostgreSQL trigger executes.
func TestWritesCallTheTriggersTheyRun(t *testing.T) {
	pg := "CREATE TABLE accounts (id int);\nCREATE FUNCTION audit() RETURNS trigger AS $$ BEGIN RETURN NEW; END $$ LANGUAGE plpgsql;\n" +
		"CREATE TRIGGER t AFTER INSERT ON accounts FOR EACH ROW EXECUTE FUNCTION audit();\n"
	got := linked(t, postgres(), []facts.Text{text("INSERT INTO accounts (id) VALUES (1)"), text("DELETE FROM accounts")}, "db/a.sql", pg)
	if want := []string{".sql/public.audit"}; !slices.Equal(targets(got), want) || got.Calls[0].Times != 1 || got.Reads != 2 {
		t.Errorf("postgresql: calls %+v reads %d, want one call of audit and 2 reads", got.Calls, got.Reads)
	}
	lite := "CREATE TABLE accounts (id INTEGER PRIMARY KEY AUTOINCREMENT);\nCREATE TRIGGER stamp AFTER UPDATE ON accounts BEGIN SELECT 1; END;\n"
	got = linked(t, dialect("sqlite"), []facts.Text{text("UPDATE accounts SET id = 2")}, "db/a.sql", lite)
	if want := []string{".sql/main.stamp"}; !slices.Equal(targets(got), want) {
		t.Errorf("sqlite: calls %v, want %v", targets(got), want)
	}
}

// viewpoint: behaviour
// target: sql.Adapter.Link
// The argument passed where the account goes is counted per distinct value, for the isolated
// invariant; a call inside a loop counts twice.
func TestScopesAndTimesOfSQLCalls(t *testing.T) {
	got := linked(t, postgres(), []facts.Text{text("SELECT billing.is_active(1, 60)"), text("SELECT billing.is_active(2, 60)"),
		{Path: "a", Line: 9, Value: "SELECT billing.is_active(1, 60)", Times: 2}}, "db/a.sql", schema)
	if len(got.Calls) != 1 || got.Calls[0].Scopes != 2 || got.Calls[0].Times != 4 {
		t.Errorf("calls %+v, want one call with 2 scopes, 4 times", got.Calls)
	}
}

// viewpoint: behaviour
// target: sql.Adapter.Link
// Test SQL that reads the clock or waits is a hazard, and recreating a product object is a mock; the
// same words in a string that is not SQL are not.
func TestHazardsInTestSQL(t *testing.T) {
	got := linked(t, postgres(), []facts.Text{text("SELECT now()"), text("SELECT pg_sleep(1)"),
		text("CREATE OR REPLACE FUNCTION billing.is_active(account_id int, ttl int) RETURNS boolean LANGUAGE sql AS $$ SELECT false $$"),
		text("call now() later")}, "db/a.sql", schema)
	var cats []string
	for _, h := range got.Hazards {
		cats = append(cats, string(h.Category))
	}
	sort.Strings(cats)
	if want := []string{"clock", "mock", "sleep"}; !slices.Equal(cats, want) {
		t.Errorf("hazards %v, want %v", cats, want)
	}
	got = linked(t, dialect("tsql"), []facts.Text{text("WAITFOR DELAY '00:00:01'")}, "db/a.sql", "CREATE TABLE dbo.t (id int)\nGO\n")
	if len(got.Hazards) != 1 || got.Hazards[0].Category != facts.HazardSleep {
		t.Errorf("tsql hazards %+v, want one sleep", got.Hazards)
	}
}

// viewpoint: behaviour
// target: sql.Adapter.Link
// Without a database (no .sql file and no configured database) test strings are not read.
func TestNoDatabaseNoTestSQL(t *testing.T) {
	got := linked(t, config.Config{}, []facts.Text{text("SELECT billing.is_active(1, 2)"),
		{Path: "a", Line: 1, Dynamic: true, Sink: facts.TextSinkSQL, Times: 1}}, "app/x.go", "package app\n")
	if len(got.Calls) != 0 || got.Unresolved != 0 || got.Reads != 0 {
		t.Errorf("calls %v unresolved %d reads %d, want nothing", targets(got), got.Unresolved, got.Reads)
	}
}

// viewpoint: behaviour
// target: sql.Adapter.Analyze
// Tests written in SQL are tests: a pgTAP file that plans, a tSQLt procedure of a test class, a
// utPLSQL --%test procedure; their assertions, skips and fakes count.
func TestTestsWrittenInSQL(t *testing.T) {
	an := analyze(t, sql.New(), version(
		"db/schema.sql", schema,
		"test/accounts.sql", "SELECT plan(2);\nSELECT ok(billing.is_active(1, 2));\nSELECT is(1, 1);\nSELECT * FROM finish();\n",
		"test/empty.sql", "SELECT plan(1);\nSELECT billing.is_active(1, 2);\n",
	), postgres())
	tests := map[string]facts.Test{}
	for _, ts := range an.Tests {
		tests[ts.Path] = ts
	}
	if ts := tests["test/accounts.sql"]; ts.Asserts != 2 || !slices.Equal(targets(ts), []string{".sql/billing.is_active"}) || ts.Unresolved != 0 {
		t.Errorf("pgTAP: %+v, want 2 asserts and a call of billing.is_active", ts)
	}
	if ts := tests["test/empty.sql"]; ts.Asserts != 0 {
		t.Errorf("pgTAP without asserts: %d asserts", ts.Asserts)
	}

	an = analyze(t, sql.New(), version(
		"db/schema.sql", "CREATE PROCEDURE dbo.CloseAccount @AccountId int AS SELECT 1\nGO\n",
		"tests/close.sql", "EXEC tSQLt.NewTestClass 'CloseTests';\nGO\n"+
			"CREATE PROCEDURE CloseTests.[test closes] AS BEGIN EXEC tSQLt.FakeTable 'dbo.Accounts'; EXEC dbo.CloseAccount 1; "+
			"EXEC tSQLt.AssertEquals 1, 1; END\nGO\n",
	), dialect("tsql"))
	if len(an.Tests) != 1 || an.Tests[0].Asserts != 1 || !slices.Equal(targets(an.Tests[0]), []string{".sql/dbo.closeaccount"}) ||
		len(an.Tests[0].Hazards) != 1 || an.Tests[0].Hazards[0].Category != facts.HazardMock {
		t.Errorf("tSQLt: %+v, want one test calling dbo.closeaccount with one assert and one mock", an.Tests)
	}

	an = analyze(t, sql.New(), version(
		"db/schema.sql", "CREATE OR REPLACE FUNCTION billing.is_active(p NUMBER) RETURN NUMBER IS BEGIN RETURN 1; END;\n/\n",
		"tests/accounts_test.sql", "CREATE OR REPLACE PACKAGE accounts_test AS\n  --%suite\n\n  --%test\n  PROCEDURE active;\n\n"+
			"  --%test\n  --%disabled\n  PROCEDURE later;\nEND;\n/\n"+
			"CREATE OR REPLACE PACKAGE BODY accounts_test AS\n  PROCEDURE active IS BEGIN ut.expect(billing.is_active(1)).to_equal(1); END;\n"+
			"  PROCEDURE later IS BEGIN NULL; END;\nEND;\n/\n",
	), dialect("oracle"))
	byName := map[string]facts.Test{}
	for _, ts := range an.Tests {
		byName[ts.Name] = ts
	}
	if ts := byName["ACCOUNTS_TEST.ACTIVE"]; ts.Asserts != 1 || !slices.Equal(targets(ts), []string{".sql/BILLING.IS_ACTIVE"}) {
		t.Errorf("utPLSQL active: %+v", ts)
	}
	if ts := byName["ACCOUNTS_TEST.LATER"]; !ts.Skip.Skipped || ts.Skip.Reason != "" {
		t.Errorf("utPLSQL later: skip %+v, want skipped without a reason", ts.Skip)
	}
}

// viewpoint: behaviour
// target: sql.EnabledDialects
// The dialects the configuration accepts are exactly the enabled ones: a dialect that has not passed
// its trial cannot be chosen.
func TestConfigurationAcceptsTheEnabledDialects(t *testing.T) {
	var accepted []string
	for _, v := range config.SQLDialects {
		accepted = append(accepted, v.Name)
	}
	sort.Strings(accepted)
	if got := sql.EnabledDialects(); !slices.Equal(got, accepted) {
		t.Errorf("enabled %v, configuration accepts %v", got, accepted)
	}
}

// viewpoint: behaviour
// target: sql.Adapter.Analyze
// guards: same-rules-every-language
// The same files give the same facts every time they are read.
func TestReadingIsDeterministic(t *testing.T) {
	v := version("db/schema.sql", schema, "db/more.sql", "CREATE TABLE t (id int);\nCREATE VIEW w AS SELECT id FROM t;")
	first := analyze(t, sql.New(), v, postgres())
	for i := 0; i < 3; i++ {
		again := analyze(t, sql.New(), v, postgres())
		if strings.Join(symbolIDs(again), ",") != strings.Join(symbolIDs(first), ",") {
			t.Fatalf("read %d gave %v, first %v", i, symbolIDs(again), symbolIDs(first))
		}
	}
}
