package sql

import "testing"

// viewpoint: behaviour
// target: sql.prepare
// The holes of the host language become 0 where they stand outside string literals, each once, in
// the order of the text; the drivers' parameters become 0 too, except where the dialect writes the
// same form in its own syntax: jsonb's ?| and ?&, a :: cast, a:b after a word, and the @name
// variables of SQL Server and MySQL.
func TestPrepareTurnsHolesAndParametersIntoValues(t *testing.T) {
	cases := []struct {
		name  string
		d     tableDialect
		text  string
		holes []int
		want  string
	}{
		{"numbered and question-mark parameters", postgresqlDialect, "SELECT a FROM t WHERE b = $1 AND c = ?", nil,
			"SELECT a FROM t WHERE b = 0 AND c = 0"},
		{"jsonb operators", postgresqlDialect, "SELECT a ?| b, a ?& c, a ? d", nil, "SELECT a ?| b, a ?& c, a 0 d"},
		{"a question mark at the end", postgresqlDialect, "SELECT ?", nil, "SELECT 0"},
		{"casts and a:b after each kind of word character", postgresqlDialect,
			"SELECT x::int, a:b, z:b, A:b, Z:b, _:b, 0:b, 9:b", nil, "SELECT x::int, a:b, z:b, A:b, Z:b, _:b, 0:b, 9:b"},
		{"named parameters", postgresqlDialect, ":first, (:id), =:b, +:c, `:d, {:e, [:f, @:g, /:h", nil,
			"0, (0), =0, +0, `0, {0, [0, @0, /0"},
		{"format parameters", postgresqlDialect, "WHERE a = %s AND b = %(id)s", nil, "WHERE a = 0 AND b = 0"},
		{"@name in PostgreSQL", postgresqlDialect, "SELECT @p", nil, "SELECT 0"},
		{"@name in SQL Server", tsqlDialect, "SELECT @p", nil, "SELECT @p"},
		{"@name in MySQL", mysqlDialect, "SELECT @p", nil, "SELECT @p"},
		{"a parameter inside a string", postgresqlDialect, "SELECT '?'", nil, "SELECT '?'"},
		{"a hole", postgresqlDialect, "SELECT a FROM t WHERE b = ", []int{26}, "SELECT a FROM t WHERE b =  0 "},
		{"a hole inside a string", postgresqlDialect, "SELECT 'x'", []int{8}, "SELECT 'x'"},
		{"holes repeated, out of the text, and out of order", postgresqlDialect, "abcdefghijkl",
			[]int{10, 5, 10, -1, 99}, "abcde 0 fghij 0 kl"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := prepare(tc.text, tc.holes, tc.d); got != tc.want {
				t.Errorf("prepare(%q, %v) = %q, want %q", tc.text, tc.holes, got, tc.want)
			}
		})
	}
}

// viewpoint: behaviour
// target: sql.firstWord
// The first word is the one a statement could start with: followed by the end of the text, a space,
// ( or ;, after comments and opening parentheses.
func TestFirstWordOfATestString(t *testing.T) {
	cases := map[string]string{
		"SELECT":                    "SELECT",
		"-- note\n (select 1)":      "select",
		"refresh-":                  "",
		"INSERT;":                   "INSERT",
		"\tUPDATE(t)":               "UPDATE",
		"table=":                    "",
		"/* a */ DELETE FROM t":     "DELETE",
		"with_cte AS (SELECT 1) ab": "with_cte",
	}
	for text, want := range cases {
		if got := firstWord(text, postgresqlDialect.lex); got != want {
			t.Errorf("firstWord(%q) = %q, want %q", text, got, want)
		}
	}
}

// viewpoint: behaviour
// target: sql.callEscape
// JDBC's {call p(?)} and {? = call f(?)} become the dialect's call statement; other text stays.
func TestJDBCCallEscapes(t *testing.T) {
	cases := []struct{ text, dialect, want string }{
		{"{call p(?)}", "postgresql", "CALL p(?)"},
		{"{ ? = call f(?) }", "mysql", "CALL f(?)"},
		{"{call p(?)}", "tsql", "EXEC p(?)"},
		{"SELECT 1", "tsql", "SELECT 1"},
	}
	for _, tc := range cases {
		if got := callEscape(tc.text, tc.dialect); got != tc.want {
			t.Errorf("callEscape(%q, %s) = %q, want %q", tc.text, tc.dialect, got, tc.want)
		}
	}
}

// viewpoint: behaviour
// target: sql.mentions
// A text mentions the database when one of its words, folded by the dialect, is the last part of an
// object's name.
func TestMentionsOfObjects(t *testing.T) {
	db := &database{objects: map[string]*object{"public.accounts": {}}}
	cases := map[string]bool{"load ACCOUNTS now": true, "accounts": true, "public": false, "nothing here": false}
	for text, want := range cases {
		if got := mentions(db, postgresqlDialect, text); got != want {
			t.Errorf("mentions(%q) = %v, want %v", text, got, want)
		}
	}
}
