//go:build sqlgrammars

package sql_test

import (
	"slices"
	"testing"

	"github.com/rerorets/hadome/internal/adapter/sql"
	"github.com/rerorets/hadome/internal/facts"
)

// The readers of the dialects that are not enabled. Their grammars are linked only with
// -tags sqlgrammars: go test -tags sqlgrammars ./internal/adapter/sql/

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
// target: sql.Adapter.Link
// Oracle upper-cases unquoted names, and SQL Server's [dbo].[Proc] is dbo.proc.
func TestNamesFoldInDialectsNotEnabled(t *testing.T) {
	var got facts.Test
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
// A write to a table runs the trigger of a dialect that writes the body inline (SQLite).
func TestInlineTriggersRun(t *testing.T) {
	var got facts.Test
	lite := "CREATE TABLE accounts (id INTEGER PRIMARY KEY AUTOINCREMENT);\nCREATE TRIGGER stamp AFTER UPDATE ON accounts BEGIN SELECT 1; END;\n"
	got = linked(t, dialect("sqlite"), []facts.Text{text("UPDATE accounts SET id = 2")}, "db/a.sql", lite)
	if want := []string{".sql/main.stamp"}; !slices.Equal(targets(got), want) {
		t.Errorf("sqlite: calls %v, want %v", targets(got), want)
	}
}

// viewpoint: behaviour
// target: sql.Adapter.Link
// SQL Server's WAITFOR DELAY is a sleep.
func TestWaitforIsASleep(t *testing.T) {
	var got facts.Test
	got = linked(t, dialect("tsql"), []facts.Text{text("WAITFOR DELAY '00:00:01'")}, "db/a.sql", "CREATE TABLE dbo.t (id int)\nGO\n")
	if len(got.Hazards) != 1 || got.Hazards[0].Category != facts.HazardSleep {
		t.Errorf("tsql hazards %+v, want one sleep", got.Hazards)
	}
}

// viewpoint: behaviour
// target: sql.Adapter.Analyze
// A tSQLt procedure of a test class and a utPLSQL --%test procedure are tests, with their assertions,
// skips and fakes.
func TestTSQLtAndUtPLSQLTests(t *testing.T) {
	var an *facts.Analysis

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
