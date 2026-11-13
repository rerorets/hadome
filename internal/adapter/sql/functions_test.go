package sql_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/rerorets/hadome/internal/adapter/sql"
)

// viewpoint: behaviour
// target: sql.Adapter.Analyze
// In a product file, the expression of a BigQuery SQL function is not parsed: a syntax error inside
// it leaves the file read, the function a symbol, and the statements after it read. Every head form
// counts, the expression may hold nested parentheses, and parentheses inside its strings, comments
// and quoted names do not end it.
func TestProductFunctionExpressionsAreNotParsed(t *testing.T) {
	const broken = "x + * WHERE"
	cases := []struct{ name, src string }{
		{"a function", "CREATE FUNCTION ds.f(x INT64) AS (" + broken + ");"},
		{"a replaced function with its return type", "CREATE OR REPLACE FUNCTION ds.f(x INT64) RETURNS INT64 AS (" + broken + ");"},
		{"an aggregate function", "CREATE AGGREGATE FUNCTION ds.f(x INT64) AS (SUM(" + broken + "));"},
		{"nested parentheses", "CREATE FUNCTION ds.f(x INT64) AS (((SELECT COUNT(*) FROM UNNEST([x]))) " + broken + ");"},
		{"parentheses in a string, a comment and a quoted name",
			"CREATE FUNCTION ds.f(x INT64) AS (CONCAT(')', '((') /* ) ( */ + `a)b`.c " + broken + ");"},
		{"OPTIONS after the expression",
			"CREATE FUNCTION ds.f(x INT64) AS (" + broken + ")\nOPTIONS(description = \"Adds (one)\");"},
	}
	for _, c := range cases {
		an := analyze(t, sql.Adapter{}, version("bq/V1__f.sql", c.src+"\nCREATE TABLE ds.t (a INT64);\n"), dialect("bigquery"))
		if got, want := symbolIDs(an), []string{".sql/ds.f", ".sql/ds.t"}; !slices.Equal(got, want) || len(unread(an)) > 0 {
			t.Errorf("%s: symbols %v (unread %v), want %v and nothing unread", c.name, got, unread(an), want)
		}
	}
}

// viewpoint: behaviour
// target: sql.Adapter.Analyze
// What the expression of a function does not hide still makes the file unread: an error outside it,
// an unclosed string or comment in it, parentheses that do not close before a ; or the end of the
// file, parentheses with no token, and any error in a test file, which is parsed whole. A function
// whose AS is not followed by ( is parsed whole too.
func TestErrorsAroundFunctionExpressionsStillMakeTheFileUnread(t *testing.T) {
	cases := []struct{ name, path, src string }{
		{"an error in the parameters", "bq/a.sql", "CREATE FUNCTION ds.f(x INT64 INT64) AS (x + 1);"},
		{"an error in OPTIONS", "bq/a.sql", "CREATE FUNCTION ds.f(x INT64) AS (x + 1) OPTIONS(description);"},
		{"an unclosed string in the expression", "bq/a.sql", "CREATE FUNCTION ds.f(x INT64) AS ('open);\nCREATE TABLE ds.t (a INT64);"},
		{"an unclosed comment in the expression", "bq/a.sql", "CREATE FUNCTION ds.f(x INT64) AS (x /* open);"},
		{"a ; before the closing parenthesis", "bq/a.sql", "CREATE FUNCTION ds.f(x INT64) AS ((x + * 1);\nSELECT (1));"},
		{"no closing parenthesis before the end", "bq/a.sql", "CREATE FUNCTION ds.f(x INT64) AS (x + * 1"},
		{"parentheses with no token", "bq/a.sql", "CREATE FUNCTION ds.f(x INT64) AS ( /* nothing */ );"},
		{"a broken expression in a test file", "test/f.sql", "CREATE FUNCTION ds.f(x INT64) AS (x + * 1);"},
		{"a broken procedure, which is no function", "bq/a.sql", "CREATE PROCEDURE ds.p(x INT64) AS (x + * 1);"},
	}
	for _, c := range cases {
		an := analyze(t, sql.Adapter{}, version(c.path, c.src), dialect("bigquery"))
		if got := unread(an); !slices.Equal(got, []string{c.path}) {
			t.Errorf("%s: unread %v, want [%s]", c.name, got, c.path)
		}
	}
}

// viewpoint: behaviour
// target: sql.Adapter.Analyze
// A function whose expression is not parsed keeps the lines of its own statement, and the
// statements after it keep theirs and their doc comments. A JavaScript function, whose body is a
// string after AS, is read as a whole statement and still defines its symbol.
func TestFunctionExpressionsKeepPositions(t *testing.T) {
	an := analyze(t, sql.Adapter{}, version("bq/V1__f.sql",
		"CREATE FUNCTION ds.f(x INT64) AS (\n  x + *\n  WHERE\n)\nOPTIONS(description = 'one');\n-- Rows.\nCREATE TABLE ds.t (a INT64);\n"),
		dialect("bigquery"))
	var got []string
	for _, s := range an.Symbols {
		got = append(got, fmt.Sprintf("%s %d-%d doc=%v", s.ID, s.Line, s.EndLine, s.Doc))
	}
	if want := []string{".sql/ds.f 1-5 doc=true", ".sql/ds.t 7-7 doc=true"}; !slices.Equal(got, want) {
		t.Errorf("symbols %v (unread %v), want %v", got, unread(an), want)
	}

	an = analyze(t, sql.Adapter{}, version("bq/V1__js.sql",
		"CREATE FUNCTION ds.js(x FLOAT64) RETURNS FLOAT64 LANGUAGE js AS r\"\"\"return (x;\"\"\";\nCREATE TABLE ds.t (a INT64);\n"),
		dialect("bigquery"))
	if got, want := symbolIDs(an), []string{".sql/ds.js", ".sql/ds.t"}; !slices.Equal(got, want) {
		t.Errorf("symbols %v (unread %v), want %v", got, unread(an), want)
	}
}
