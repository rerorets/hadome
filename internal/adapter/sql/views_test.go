package sql_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/rerorets/hadome/internal/adapter/sql"
	"github.com/rerorets/hadome/internal/config"
)

// viewpoint: behaviour
// target: sql.Adapter.Analyze
// In a product file, the query of a view or table function is not parsed: a syntax error inside it
// leaves the file read, the view a symbol, and the statements after it read. Every head form of the
// enabled dialects counts.
func TestProductViewQueriesAreNotParsed(t *testing.T) {
	const broken = "SELECT FROM FROM sales.orders WHERE"
	cases := []struct {
		name, src string
		c         config.Analysis
		want      []string
	}{
		{"postgresql view", "CREATE VIEW sales.daily AS " + broken + ";\nCREATE TABLE sales.t (id int);",
			postgres(), []string{".sql/sales.daily", ".sql/sales.t"}},
		{"postgresql view whose query is one word", "CREATE VIEW sales.daily AS x;\nCREATE TABLE sales.t (id int);",
			postgres(), []string{".sql/sales.daily", ".sql/sales.t"}},
		{"postgresql recursive view", "CREATE OR REPLACE RECURSIVE VIEW sales.daily (n) AS " + broken + ";",
			postgres(), []string{".sql/sales.daily"}},
		{"postgresql materialized view", "CREATE MATERIALIZED VIEW sales.daily AS " + broken + "\nWITH NO DATA;",
			postgres(), []string{".sql/sales.daily"}},
		{"mysql view with its options",
			"CREATE OR REPLACE ALGORITHM = MERGE DEFINER = 'admin'@'%' SQL SECURITY INVOKER VIEW daily AS " + broken +
				" WITH CHECK OPTION;\nCREATE TABLE t (id INT);",
			dialect("mysql"), []string{".sql/daily", ".sql/t"}},
		{"mysql view with a definer that calls", "CREATE DEFINER = CURRENT_USER() VIEW daily AS " + broken + ";",
			dialect("mysql"), []string{".sql/daily"}},
		{"bigquery view", "CREATE OR REPLACE VIEW sales.daily AS " + broken + ";\nCREATE TABLE sales.t (a INT64);",
			dialect("bigquery"), []string{".sql/sales.daily", ".sql/sales.t"}},
		{"bigquery materialized view", "CREATE MATERIALIZED VIEW sales.daily PARTITION BY CAST(day AS DATE) AS " + broken + ";",
			dialect("bigquery"), []string{".sql/sales.daily"}},
		{"bigquery table function", "CREATE OR REPLACE TABLE FUNCTION sales.top(n INT64) AS (" + broken + ");",
			dialect("bigquery"), []string{".sql/sales.top"}},
	}
	for _, c := range cases {
		an := analyze(t, sql.Adapter{}, version("db/V1__views.sql", c.src), c.c)
		if got := symbolIDs(an); !slices.Equal(got, c.want) || len(unread(an)) > 0 {
			t.Errorf("%s: symbols %v (unread %v), want %v and nothing unread", c.name, got, unread(an), c.want)
		}
	}
}

// viewpoint: behaviour
// target: sql.Adapter.Analyze
// What the query of a view does not hide still makes the file unread: an error before AS, a string
// or a comment in the query that never closes, a query with no token, and any error in a test file,
// which is parsed whole. A head that is not a view of the dialect is parsed whole too.
func TestErrorsAroundViewQueriesStillMakeTheFileUnread(t *testing.T) {
	cases := []struct{ name, path, src string }{
		{"an error before AS", "db/a.sql", "CREATE VIEW sales.daily daily AS SELECT 1;"},
		{"an unclosed string in the query", "db/a.sql", "CREATE VIEW sales.daily AS SELECT 'open;\nCREATE TABLE t (id int);"},
		{"an unclosed dollar-quoted string in the query", "db/a.sql", "CREATE VIEW sales.daily AS SELECT $$open;\nCREATE TABLE t (id int);"},
		{"an unclosed comment in the query", "db/a.sql", "CREATE VIEW sales.daily AS SELECT 1 /* open;"},
		{"a comment opened by the file's last bytes", "db/a.sql", "CREATE VIEW sales.daily AS SELECT FROM FROM t /*/"},
		{"a query with no token", "db/a.sql", "CREATE VIEW sales.daily AS /* nothing */;"},
		{"a broken query in a test file", "test/views.sql", "CREATE VIEW sales.daily AS SELECT FROM FROM t;"},
		{"a temporary view, which the table does not list", "db/a.sql", "CREATE TEMP VIEW sales.daily AS SELECT FROM FROM t;"},
	}
	for _, c := range cases {
		an := analyze(t, sql.Adapter{}, version(c.path, c.src), postgres())
		if got := unread(an); !slices.Equal(got, []string{c.path}) {
			t.Errorf("%s: unread %v, want [%s]", c.name, got, c.path)
		}
	}
}

// viewpoint: behaviour
// target: sql.Adapter.Analyze
// A view whose query is not parsed keeps the lines of its own statement, ending on the query's last
// line, and the statements after it keep theirs, also past blank lines between the query and its ;. A ; inside a string of the query does not end it,
// and an AS inside parentheses or a string of the head does not start it: the head is read whole,
// with the doc its OPTIONS give.
func TestViewQueriesKeepPositionsAndTheirHead(t *testing.T) {
	an := analyze(t, sql.Adapter{}, version("db/a.sql",
		"CREATE VIEW sales.daily AS\nSELECT FROM FROM\n  sales.orders\n  WHERE;\nCREATE TABLE sales.t (id int);\n"), postgres())
	var got []string
	for _, s := range an.Symbols {
		got = append(got, fmt.Sprintf("%s %d-%d", s.ID, s.Line, s.EndLine))
	}
	if want := []string{".sql/sales.daily 1-4", ".sql/sales.t 5-5"}; !slices.Equal(got, want) {
		t.Errorf("symbols %v, want %v", got, want)
	}

	an = analyze(t, sql.Adapter{}, version("db/c.sql",
		"CREATE VIEW sales.daily AS SELECT FROM FROM sales.orders\n\n;\nCREATE TABLE sales.t (id int);\n"), postgres())
	if got := symbolLines(an); !slices.Equal(got, []string{".sql/sales.daily line=1 params=0", ".sql/sales.t line=4 params=0"}) {
		t.Errorf("symbols %v, want the table on line 4", got)
	}

	an = analyze(t, sql.Adapter{}, version("db/b.sql",
		"CREATE VIEW sales.daily AS SELECT a FROM FROM sales.orders WHERE a > 1;\n-- Orders per day.\nCREATE TABLE sales.t (id int);\n"), postgres())
	got = nil
	for _, s := range an.Symbols {
		got = append(got, fmt.Sprintf("%s %d doc=%v", s.ID, s.Line, s.Doc))
	}
	if want := []string{".sql/sales.daily 1 doc=false", ".sql/sales.t 3 doc=true"}; !slices.Equal(got, want) {
		t.Errorf("symbols %v, want %v", got, want)
	}

	an = analyze(t, sql.Adapter{}, version("bq/V1__a.sql",
		"CREATE VIEW ds.v OPTIONS(description = \"a AS b\") AS SELECT ';' FROM FROM ds.t;\n"+
			"CREATE TABLE ds.t2 (a INT64);\n"), dialect("bigquery"))
	got = nil
	for _, s := range an.Symbols {
		got = append(got, fmt.Sprintf("%s doc=%v", s.ID, s.Doc))
	}
	if want := []string{".sql/ds.t2 doc=false", ".sql/ds.v doc=true"}; !slices.Equal(got, want) {
		t.Errorf("symbols %v (unread %v), want %v", got, unread(an), want)
	}

	an = analyze(t, sql.Adapter{}, version("db/a.sql",
		"CREATE VIEW sales.daily WITH (security_barrier = 'AS') AS SELECT FROM FROM t;\n"), postgres())
	if got := symbolIDs(an); !slices.Equal(got, []string{".sql/sales.daily"}) {
		t.Errorf("symbols %v (unread %v), want [.sql/sales.daily]", got, unread(an))
	}
}
