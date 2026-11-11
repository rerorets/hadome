package sql

import (
	"strings"
	"testing"
)

// viewpoint: behaviour
// target: sql.tableDialect.withoutViewQueries
// The dialects whose grammars only a build with -tags sqlgrammars has: SQLite and Snowflake replace
// a view's query by SELECT 1 in a text of the same length with the same line breaks; SQL Server and
// Oracle, where the tokens cannot tell where a query ends, give the statement unchanged.
func TestViewQueriesOfEveryDialect(t *testing.T) {
	cases := []struct {
		name string
		d    tableDialect
		src  string
		want string
	}{
		{"sqlite", sqliteDialect, "CREATE VIEW daily AS SELECT x\nFROM orders;",
			// " x" and "FROM orders" become blanks, but the last byte of orders, which becomes 1.
			"CREATE VIEW daily AS SELECT" + strings.Repeat(" ", len(" x")) + "\n" + strings.Repeat(" ", len("FROM order")) + "1;"},
		{"snowflake secure materialized", snowflakeDialect, "CREATE OR REPLACE SECURE MATERIALIZED VIEW s.daily AS SELECT x FROM t;",
			"CREATE OR REPLACE SECURE MATERIALIZED VIEW s.daily AS SELECT" + strings.Repeat(" ", len(" x FROM ")) + "1;"},
		{"sql server", tsqlDialect, "CREATE VIEW dbo.daily AS SELECT x FROM t;", "CREATE VIEW dbo.daily AS SELECT x FROM t;"},
		{"oracle", oracleDialect, "CREATE VIEW daily AS SELECT x FROM t;", "CREATE VIEW daily AS SELECT x FROM t;"},
	}
	for _, c := range cases {
		got := c.d.withoutViewQueries(Statement{Text: c.src, Start: 7, Line: 3})
		if got.Text != c.want || got.Start != 7 || got.Line != 3 {
			t.Errorf("%s: %q at %d line %d, want %q at 7 line 3", c.name, got.Text, got.Start, got.Line, c.want)
		}
	}
}
