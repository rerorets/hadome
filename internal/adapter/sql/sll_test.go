package sql

import (
	"testing"

	"github.com/antlr4-go/antlr/v4"
)

// viewpoint: behaviour
// target: sql.antlrGrammar.Parse
// Statements whose grammar rules once needed the full LL prediction read with the fast SLL one: a
// parenthesised query where an expression may also start, a call where a name may also end a
// statement, a set operation inside a parenthesised table, and a parenthesised OR after AND. A
// statement SLL cannot read is read again with LL, which costs the whole piece once more.
func TestStatementsWithParenthesesReadInSLL(t *testing.T) {
	cases := []struct{ dialect, text string }{
		{"mysql", "SET @x := (SELECT 1);"},
		{"mysql", "SET @n := (SELECT COUNT(*) FROM information_schema.tables WHERE table_name = 'a');"},
		{"mysql", "SELECT a.id, NOW(6) FROM a;"},
		{"mysql", "SELECT COALESCE(MIN(a.created_at), CURRENT_TIMESTAMP) FROM a;"},
		{"mysql", "SELECT NULLIF(JSON_MERGE_PATCH('{}', a.doc), '{}') FROM a;"},
		{"mysql", "SELECT tap.plan(1);"},
		{"mysql", "WITH a AS (SELECT 1 AS id) SELECT is_open(id) FROM a"},
		{"mysql", "SELECT a FROM t WHERE f(a) = 1 AND s.g(a, h(b)) IS NULL;"},
		{"mysql", "SELECT SCHEMA, CURRENT_USER, a.b.c FROM a;"},
		{"bigquery", "SELECT a FROM t WHERE b = 1 AND (c IS NULL OR c = 1)"},
		{"bigquery", "SELECT a FROM (SELECT * FROM t WHERE (p IS NULL OR q = p) AND d >= DATE '2020-01-01') o"},
		{"bigquery", "SELECT SUM(IF(a = 1 AND (b IS NULL OR b > 0), b, 0)) AS s FROM t"},
		{"bigquery", "SELECT (a OR b OR c), (a AND b), (CASE WHEN a OR b THEN 1 END), x IN (a OR b) FROM t"},
	}
	for _, tc := range cases {
		g := grammars[tc.dialect].(antlrGrammar)
		if _, _, err := g.run(tc.text, antlr.PredictionModeSLL); err != nil {
			t.Errorf("%s: %q does not read in SLL: %v", tc.dialect, tc.text, err)
		}
		if _, _, err := g.run(tc.text, antlr.PredictionModeLL); err != nil {
			t.Errorf("%s: %q does not read: %v", tc.dialect, tc.text, err)
		}
	}
}
