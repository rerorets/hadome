package sql_test

import (
	"fmt"
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
// psql's meta-commands and the data lines after COPY … FROM stdin are not SQL the grammar reads.
func TestPsqlFormsAreNotRead(t *testing.T) {
	an := analyze(t, sql.New(), version("db/schema.sql", "\\set ON_ERROR_STOP on\nCOPY t (id) FROM stdin;\n1\n2\n\\.\nCREATE TABLE t (id int);\n"), postgres())
	if got, want := symbolIDs(an), []string{".sql/public.t"}; !slices.Equal(got, want) {
		t.Errorf("symbols %v, want %v (unread %v)", got, want, unread(an))
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
		{"a string without a statement head passed to SQL that does not read", []facts.Text{{Path: "a", Line: 1, Value: "job-",
			Holes: []int{4}, Sink: facts.TextSinkSQL, Times: 1}}, nil, 0, 0},
		{"a word like a statement head that does not start a statement", []facts.Text{{Path: "a", Line: 1, Value: "refresh-",
			Holes: []int{8}, Times: 1}, {Path: "a", Line: 2, Value: "table=", Holes: []int{6}, Times: 1}}, nil, 0, 0},
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
}

// viewpoint: behaviour
// target: sql.Adapter.Analyze
// MySQL: bodies between DELIMITER lines keep their own ;, a handler or a routine may be one RETURN,
// and words the grammar has tokens for but no rule (SEQUENCE, SKIP) are names.
func TestMySQLDefinitions(t *testing.T) {
	src := "DELIMITER //\nCREATE FUNCTION billing.skip(p INT) RETURNS INT DETERMINISTIC\nBEGIN\n" +
		"  DECLARE EXIT HANDLER FOR NOT FOUND RETURN 0;\n  RETURN p;\nEND //\nDELIMITER ;\n" +
		"CREATE FUNCTION billing.one() RETURNS INT DETERMINISTIC RETURN 1;\n" +
		"CREATE TABLE `sequence` (id INT AUTO_INCREMENT PRIMARY KEY) ENGINE=InnoDB;\n"
	an := analyze(t, sql.New(), version("db/V1__a.sql", src), config.Config{})
	if got, want := symbolIDs(an), []string{".sql/billing.one", ".sql/billing.skip", ".sql/sequence"}; !slices.Equal(got, want) {
		t.Errorf("symbols %v, want %v (unread %v)", got, want, unread(an))
	}
}

// viewpoint: behaviour
// target: sql.Adapter.Link
// MySQL test SQL: a WITH name hides, a backquoted name is the same object, a function is called.
func TestMySQLTestStrings(t *testing.T) {
	src := "CREATE TABLE `accounts` (id INT AUTO_INCREMENT PRIMARY KEY) ENGINE=InnoDB;\n" +
		"CREATE FUNCTION is_open(account_id INT) RETURNS INT DETERMINISTIC RETURN 1;\n"
	got := linked(t, dialect("mysql"), []facts.Text{text("WITH a AS (SELECT 1 AS id) SELECT is_open(id) FROM a"),
		text("SELECT * FROM `accounts` WHERE id = ?")}, "db/V1__a.sql", src)
	if want := []string{".sql/is_open"}; !slices.Equal(targets(got), want) || got.Reads != 1 || got.Unresolved != 0 {
		t.Errorf("calls %v reads %d unresolved %d, want %v, 1, 0", targets(got), got.Reads, got.Unresolved, want)
	}
}

// viewpoint: behaviour
// target: sql.Adapter.Analyze
// BigQuery: definitions inside a script's blocks count, a routine's body does not, and the forms real
// code writes read: QUALIFY without WHERE, USING with a list, ARRAY<STRUCT<…>> closed by >>, a
// table-valued function in FROM.
func TestBigQueryDefinitions(t *testing.T) {
	src := "CREATE PROCEDURE ds.p() BEGIN CREATE TABLE ds.inner_t (a INT64); END;\n" +
		"IF TRUE THEN CREATE TABLE ds.made (a INT64); END IF;\n" +
		"CREATE TABLE `proj.ds.t` (a ARRAY<STRUCT<x INT64, y STRING>>) OPTIONS(description = 'rows');\n" +
		"CREATE TABLE FUNCTION ds.tvf(n INT64) AS SELECT n AS a;\n" +
		"CREATE VIEW ds.v AS SELECT a.x FROM `proj.ds.t` a JOIN ds.made USING (a, b) QUALIFY ROW_NUMBER() OVER (PARTITION BY a.x) = 1;\n" +
		"CREATE VIEW ds.w AS SELECT * FROM ds.tvf(1);\n"
	an := analyze(t, sql.New(), version("bq/V1__a.sql", src), config.Config{})
	want := []string{".sql/ds.made", ".sql/ds.p", ".sql/ds.t", ".sql/ds.tvf", ".sql/ds.v", ".sql/ds.w"}
	if got := symbolIDs(an); !slices.Equal(got, want) {
		t.Errorf("symbols %v, want %v (unread %v)", got, want, unread(an))
	}
}

// viewpoint: behaviour
// target: sql.Adapter.Link
// BigQuery test SQL: a table-valued function in FROM is called, and `proj.ds.v` is ds.v when the
// configuration names no catalog.
func TestBigQueryTestStrings(t *testing.T) {
	src := "CREATE TABLE FUNCTION ds.tvf(account_id INT64) AS SELECT account_id AS a;\nCREATE VIEW `proj.ds.v` AS SELECT 1 AS a;\n"
	got := linked(t, dialect("bigquery"), []facts.Text{text("SELECT * FROM `other.ds.tvf`(@account)"), text("SELECT a FROM ds.v")},
		"bq/V1__a.sql", src)
	if want := []string{".sql/ds.tvf", ".sql/ds.v"}; !slices.Equal(targets(got), want) || got.Unresolved != 0 {
		t.Errorf("calls %v unresolved %d, want %v and none", targets(got), got.Unresolved, want)
	}
}

// viewpoint: external contract
// target: sql.Adapter.Name
// The adapter is the language sql of the configuration's languages.
func TestTheAdapterIsTheLanguageSQL(t *testing.T) {
	if got := sql.New().Name(); got != "sql" || !slices.ContainsFunc(config.Languages, func(v config.Value) bool { return v.Name == got }) {
		t.Errorf("name %q is not a language of the configuration", got)
	}
}

// viewpoint: behaviour
// target: sql.Adapter.Analyze
// A product file belongs to its database's unit; a file that is not read belongs to none; a file
// the configuration says is generated gives no symbol; a file starting with a byte order mark reads.
func TestFilesTheirUnitsAndRoles(t *testing.T) {
	c := postgres()
	c.GeneratedFiles = []string{"db/gen/**"}
	an := analyze(t, sql.New(), version(
		"db/schema.sql", "\xef\xbb\xbfCREATE TABLE accounts (id int);",
		"db/gen/out.sql", "CREATE TABLE generated_rows (id int);",
		"db/1_a.down.sql", "DROP TABLE accounts;",
	), c)
	files := map[string]facts.File{}
	for _, f := range an.Files {
		files[f.Path] = f
	}
	if f := files["db/schema.sql"]; f.Unit != ".sql" || f.Role != facts.RoleSource || f.Unread {
		t.Errorf("schema.sql: %+v, want a readable product file of unit .sql", f)
	}
	if f := files["db/1_a.down.sql"]; f.Unit != "" || f.Role != facts.RoleOther {
		t.Errorf("the undo file: %+v, want role other and no unit", f)
	}
	if f := files["db/gen/out.sql"]; f.Role != facts.RoleGenerated {
		t.Errorf("the generated file: %+v, want role generated", f)
	}
	if got, want := symbolIDs(an), []string{".sql/public.accounts"}; !slices.Equal(got, want) {
		t.Errorf("symbols %v, want %v", got, want)
	}
}

// viewpoint: behaviour
// target: sql.Adapter.Link
// Link resolves against the files of the version it is given, even after the same adapter read
// other contents at the same paths.
func TestLinkUsesTheVersionItIsGiven(t *testing.T) {
	a := sql.New()
	before := version("db/schema.sql", "CREATE FUNCTION old_f() RETURNS int AS $$ SELECT 1 $$ LANGUAGE sql;")
	analyze(t, a, before, postgres())
	after := version("db/schema.sql", "CREATE FUNCTION new_f() RETURNS int AS $$ SELECT 1 $$ LANGUAGE sql;")
	an := analyze(t, sql.New(), after, postgres())
	an.Tests = []facts.Test{{Name: "TestX", Path: "app/x_test.go", Line: 1, Texts: []facts.Text{text("SELECT new_f()")}}}
	if err := a.Link(after, postgres(), an); err != nil {
		t.Fatal(err)
	}
	if got := targets(an.Tests[0]); !slices.Equal(got, []string{".sql/public.new_f"}) || an.Tests[0].Unresolved != 0 {
		t.Errorf("calls %v unresolved %d, want new_f only", got, an.Tests[0].Unresolved)
	}
	other := postgres()
	other.Databases[0].SearchPath = []string{"public"}
	an.Tests[0] = facts.Test{Name: "TestX", Path: "app/x_test.go", Line: 1, Texts: []facts.Text{text("SELECT new_f()")}}
	if err := a.Link(after, other, an); err != nil {
		t.Fatal(err)
	}
	if got := targets(an.Tests[0]); !slices.Equal(got, []string{".sql/public.new_f"}) {
		t.Errorf("with another configuration: calls %v, want new_f", got)
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

// viewpoint: behaviour
// target: sql.comments
// The comments of a SQL file are read by its dialect's rules, and those that instruct a tool are
// directives. A file without a dialect, such as one with the markers of two, is read with nested
// block comments and dollar quotes.
func TestCommentsOfSQLFiles(t *testing.T) {
	cases := []struct {
		name string
		c    config.Config
		src  string
		want []string
	}{
		{"mysql", dialect("mysql"), "# keeps accounts\nCREATE TABLE accounts (id int); -- noqa: L010\n",
			[]string{`db/schema.sql:1 "keeps accounts" directive=false`, `db/schema.sql:2 "noqa: L010" directive=true`}},
		{"markers of two dialects", config.Config{}, "CREATE TASK t AS SELECT $$ -- quoted $$; /* outer /* inner */ still */\n",
			[]string{`db/schema.sql:1 "outer /* inner */ still" directive=false`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			an := analyze(t, sql.New(), version("db/schema.sql", tc.src), tc.c)
			var got []string
			for _, c := range an.Comments {
				got = append(got, fmt.Sprintf("%s:%d %q directive=%v", c.Path, c.Line, strings.TrimSpace(c.Text), c.Directive))
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("comments %v, want %v", got, tc.want)
			}
		})
	}
}

// viewpoint: failure
// target: sql.Adapter.Link
// Link fails when a SQL file of the analysis cannot be read from the version.
func TestLinkFailsOnAFileItCannotRead(t *testing.T) {
	an := &facts.Analysis{Files: []facts.File{{Path: "db/gone.sql", Language: "sql"}},
		Tests: []facts.Test{{Name: "TestX", Path: "app/x_test.go", Line: 1, Texts: []facts.Text{text("SELECT 1")}}}}
	if err := sql.New().Link(version(), postgres(), an); err == nil {
		t.Error("no error for a file the version does not have")
	}
}

// viewpoint: behaviour
// target: sql.Adapter.Link
// Link resolves against the version's own files even when another version the adapter read holds
// the same bytes split differently among paths: a path that runs into the contents, contents that
// run into the next path, or the same contents at paths applied in another order.
func TestLinkTellsVersionsApart(t *testing.T) {
	const create = "CREATE FUNCTION new_f() RETURNS int AS $$ SELECT 1 $$ LANGUAGE sql;"
	const drop = "DROP FUNCTION new_f();"
	cases := []struct {
		name         string
		read, linkTo []string
	}{
		{"a path that runs into the contents",
			[]string{"db/a.sql", ".sql" + create},
			[]string{"db/a.sql.sql", create}},
		{"contents that run into the next path",
			[]string{"db/a.sql", "-- one\ndb/b.sql\x00" + create},
			[]string{"db/a.sql", "-- one\n", "db/b.sql", create}},
		{"the same contents applied in another order",
			[]string{"db/1.sql", create, "db/2.sql", drop},
			[]string{"db/10.sql", create, "db/9.sql", drop}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := sql.New()
			analyze(t, a, version(tc.read...), postgres())
			v := version(tc.linkTo...)
			an := analyze(t, sql.New(), v, postgres())
			an.Tests = []facts.Test{{Name: "TestX", Path: "app/x_test.go", Line: 1, Texts: []facts.Text{text("SELECT new_f()")}}}
			if err := a.Link(v, postgres(), an); err != nil {
				t.Fatal(err)
			}
			if got := targets(an.Tests[0]); !slices.Equal(got, []string{".sql/public.new_f"}) {
				t.Errorf("calls %v, want new_f", got)
			}
		})
	}
}
