package golang

import (
	"strings"
	"testing"

	"github.com/rerorets/hadome/internal/config"
)

// sinkTexts lists the strings of a test file that were passed to a call that takes SQL, as
// "<value> dynamic=<dynamic>".
func sinkTexts(t *testing.T, c config.Config, test string) []string {
	t.Helper()
	var out []string
	for _, line := range textsOf(t, c, "package store\n\n// Store keeps rows.\ntype Store struct{}\n\n// Exec runs nothing.\nfunc (Store) Exec(q string) {}\n", test) {
		if !strings.Contains(line, "sink=sql") {
			continue
		}
		value := line[strings.Index(line, `"`):strings.Index(line, " holes=")]
		dynamic := line[strings.Index(line, "dynamic="):strings.LastIndex(line, " ×")]
		out = append(out, value+" "+dynamic)
	}
	return out
}

// viewpoint: behaviour
// target: golang.walker.sinkArg
// A method counts as taking SQL only for a database client: the file imports one (sqlx itself for
// Select and Get, any subpackage of pgx), the receiver is not the testing tool, an imported package
// or the repository's own type. A leading context is skipped, whatever it is named or made by.
// fmt.Sprintf's format string is the SQL; another package's Sprintf is not.
func TestGoCallsThatTakeSQL(t *testing.T) {
	cases := []struct {
		name, imports, body string
		want                []string
	}{
		{"database/sql's Exec", `"database/sql"`, `var db *sql.DB; db.Exec("SELECT 1")`, []string{`"SELECT 1" dynamic=false`}},
		{"a pgx subpackage", `"github.com/jackc/pgx/v5"`, `var c *pgx.Conn; var ctx any; c.Exec(ctx, "SELECT 2")`, []string{`"SELECT 2" dynamic=false`}},
		{"Select needs sqlx", `"database/sql"`, `var db *sql.DB; var rows []int; db.Select(&rows, "SELECT 3")`, nil},
		{"sqlx's Select skips the destination", `"github.com/jmoiron/sqlx"`, `var db *sqlx.DB; var rows []int; db.Select(&rows, "SELECT 4")`,
			[]string{`"SELECT 4" dynamic=false`}},
		{"no database client imported", `"strings"`, `var db store.Store; _ = strings.ToUpper; db.Exec("SELECT 5")`, nil},
		{"the repository's own type", `"database/sql"`, `var s Store; var _ *sql.DB; s.Exec("SELECT 6")`, nil},
		{"a function of an imported package", `"database/sql"`, `_ = sql.ErrNoRows; sql.Exec("SELECT 7")`, nil},
		{"a context named reqCtx", `"database/sql"`, `var db *sql.DB; var reqCtx any; db.QueryContext(reqCtx, "SELECT 8")`, []string{`"SELECT 8" dynamic=false`}},
		{"a context made by context.Background", `"context"
	"database/sql"`, `var db *sql.DB; db.QueryContext(context.Background(), "SELECT 9")`, []string{`"SELECT 9" dynamic=false`}},
		{"a context made by t.Context", `"database/sql"`, `var db *sql.DB; db.QueryContext(t.Context(), "SELECT 10")`, []string{`"SELECT 10" dynamic=false`}},
		{"a first argument that is not a context", `"database/sql"`, `var db *sql.DB; var limit int; db.Query("SELECT 11", limit)`, []string{`"SELECT 11" dynamic=false`}},
		{"fmt.Sprintf's format", `"database/sql"
	"fmt"`, `var db *sql.DB; db.Exec(fmt.Sprintf("SELECT %d", 12))`, []string{`"SELECT %d" dynamic=false`}},
		{"a call that is not Sprintf", `"database/sql"`, `var db *sql.DB; db.Exec(build("SELECT 14"))`, []string{`"" dynamic=true`}},
		{"a first argument that is a name, not a context", `"database/sql"`, `var db *sql.DB; var limit int; q := "SELECT 16"; db.Query(q, limit)`,
			[]string{`"SELECT 16" dynamic=false`}},
		{"a name set again to another string", `"database/sql"`, `var db *sql.DB; q := "SELECT 17"; q = "SELECT 18"; db.Exec(q)`,
			[]string{`"SELECT 18" dynamic=false`}},
		{"a name declared again in a function literal", `"database/sql"`,
			`var db *sql.DB; q := "SELECT 19"; func() { q := "SELECT 20"; _ = q }(); db.Exec(q)`, []string{`"SELECT 19" dynamic=false`}},
		{"a name declared with var", `"database/sql"`, `var db *sql.DB; var q = "SELECT 21"; db.Exec(q)`, []string{`"SELECT 21" dynamic=false`}},
		{"another package's Sprintf", `"database/sql"
	"example.com/app/fmtx"`, `var db *sql.DB; db.Exec(fmtx.Sprintf("SELECT %d", 13))`, []string{`"" dynamic=true`}},
	}
	for _, c := range cases {
		test := "package store\n\nimport (\n\t\"testing\"\n\t" + c.imports + "\n)\n\nfunc build(s string) string { return s }\n\nfunc TestX(t *testing.T) {\n\t" +
			strings.ReplaceAll(c.body, "; ", "\n\t") + "\n}\n"
		got := sinkTexts(t, config.Config{}, test)
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// viewpoint: behaviour
// target: golang.walker.sinkArg
// A configured call that takes SQL matches by its name or as written, a method or a function.
func TestGoConfiguredSinksMatchByNameOrAsWritten(t *testing.T) {
	test := "package store\n\nimport \"testing\"\n\ntype runner struct{}\n\nfunc (runner) Run(q string) {}\nfunc (runner) Do(q string) {}\n\n" +
		"func TestX(t *testing.T) {\n\tvar r runner\n\tr.Run(\"SELECT 1\")\n\tr.Do(\"SELECT 2\")\n}\n"
	got := sinkTexts(t, config.Config{SQLSinks: map[string][]string{"go": {"r.Run"}}}, test)
	if want := []string{`"SELECT 1" dynamic=false`}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("as written: got %q, want %q", got, want)
	}
	got = sinkTexts(t, config.Config{SQLSinks: map[string][]string{"go": {"Do"}}}, test)
	if want := []string{`"SELECT 2" dynamic=false`}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("by name: got %q, want %q", got, want)
	}
	fn := "package store\n\nimport \"testing\"\n\nfunc exec(q string) {}\n\nfunc TestX(t *testing.T) {\n\texec(\"SELECT 3\")\n}\n"
	got = sinkTexts(t, config.Config{SQLSinks: map[string][]string{"go": {"exec"}}}, fn)
	if want := []string{`"SELECT 3" dynamic=false`}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("a function: got %q, want %q", got, want)
	}
}
