package golang

import (
	"go/ast"
	"go/types"
	"strings"

	"github.com/rerorets/hadome/internal/facts"
)

// sinkRule says where a call that takes SQL has its SQL argument. ctx is true when a context may
// come first (pgx's Query(ctx, sql), database/sql's QueryContext(ctx, sql)); dest is true when the
// destination comes before the SQL (sqlx's Select(dest, sql)).
type sinkRule struct {
	ctx, dest bool
	// sqlx marks the methods whose names are too common to take for SQL unless sqlx is imported.
	sqlx bool
}

// sinks are the methods of Go's database clients that take SQL. The adapter types no value of a
// package outside the repository, so it cannot see that a receiver is a *sql.DB or a pgx.Conn: a
// method counts only when the file it is called in imports one of sqlClients, and its receiver is
// not a value of one of the repository's own types.
var sinks = map[string]sinkRule{
	"Query": {ctx: true}, "QueryRow": {ctx: true}, "Exec": {ctx: true}, "Prepare": {ctx: true},
	"QueryContext": {ctx: true}, "QueryRowContext": {ctx: true}, "ExecContext": {ctx: true}, "PrepareContext": {ctx: true},
	"Queryx": {ctx: true}, "QueryRowx": {ctx: true}, "QueryxContext": {ctx: true}, "QueryRowxContext": {ctx: true},
	"MustExec": {ctx: true}, "MustExecContext": {ctx: true}, "NamedExec": {ctx: true}, "NamedQuery": {ctx: true},
	"NamedExecContext": {ctx: true}, "NamedQueryContext": {ctx: true}, "Preparex": {ctx: true}, "PreparexContext": {ctx: true},
	"Select": {ctx: true, dest: true, sqlx: true}, "Get": {ctx: true, dest: true, sqlx: true},
	"SelectContext": {ctx: true, dest: true, sqlx: true}, "GetContext": {ctx: true, dest: true, sqlx: true},
}

// sqlClients are the import paths (and their subpackages) of the database clients sinks belongs to.
var sqlClients = []string{"database/sql", "github.com/jackc/pgx", "github.com/jackc/pgconn", "github.com/jmoiron/sqlx"}

func importsClient(imports map[string]string, sqlxOnly bool) bool {
	for _, p := range imports {
		for _, c := range sqlClients {
			if sqlxOnly && c != "github.com/jmoiron/sqlx" {
				continue
			}
			if p == c || strings.HasPrefix(p, c+"/") {
				return true
			}
		}
	}
	return false
}

// sinkCall marks the SQL argument of a call that takes SQL. A string the walker can name is marked
// as passed to SQL; anything else is recorded as a dynamic string.
func (w *walker) sinkCall(c *ast.CallExpr) {
	pos, ok := w.sinkArg(c)
	if !ok {
		return
	}
	if pos < 0 || pos >= len(c.Args) {
		return
	}
	arg := ast.Unparen(c.Args[pos])
	if f, ok := arg.(*ast.CallExpr); ok && isSprintf(f, w.imports) && len(f.Args) > 0 {
		arg = ast.Unparen(f.Args[0]) // the format string is handed over as it is
	}
	if keys, ok := w.strKeys(arg); ok {
		for _, k := range keys {
			w.sinkKeys[k] = true
		}
		return
	}
	w.recordText(facts.Text{Path: w.curPath, Line: w.line(arg), Sink: facts.TextSinkSQL, Dynamic: true})
}

func isSprintf(c *ast.CallExpr, imports map[string]string) bool {
	sel, ok := c.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && imports[pkg.Name] == "fmt" && sel.Sel.Name == "Sprintf"
}

// sinkArg returns the position of a call's SQL argument when the call takes SQL: a configured
// entry, or a method of sinks called in a file that imports a database client.
func (w *walker) sinkArg(c *ast.CallExpr) (int, bool) {
	name := ""
	switch f := ast.Unparen(c.Fun).(type) {
	case *ast.SelectorExpr:
		name = f.Sel.Name
		written := types.ExprString(f)
		if w.tables.sinks[name] || w.tables.sinks[written] {
			return w.firstSQLArg(c, sinkRule{ctx: true}), true
		}
		if x, ok := f.X.(*ast.Ident); ok && (w.tParams[x.Name] || w.imports[x.Name] != "" && !w.shadowed(x.Name)) {
			return 0, false // a call on the testing tool, or a package's function
		}
		rule, ok := sinks[name]
		if !ok || !importsClient(w.imports, rule.sqlx) {
			return 0, false
		}
		if t := w.valueType(f.X); t != nil && t.id != "" {
			return 0, false // a method of the repository's own type
		}
		return w.firstSQLArg(c, rule), true
	case *ast.Ident:
		if w.tables.sinks[f.Name] {
			return w.firstSQLArg(c, sinkRule{ctx: true}), true
		}
	}
	return 0, false
}

// firstSQLArg skips a leading context and, for sqlx's Select and Get, the destination.
func (w *walker) firstSQLArg(c *ast.CallExpr, r sinkRule) int {
	pos := 0
	if r.ctx && len(c.Args) > 1 && w.isContext(c.Args[0]) {
		pos++
	}
	if r.dest {
		pos++
	}
	return pos
}

// isContext reports whether an argument is a context: a name such as ctx, or a call that makes one
// (context.Background(), t.Context()). A string never is.
func (w *walker) isContext(e ast.Expr) bool {
	switch e := ast.Unparen(e).(type) {
	case *ast.Ident:
		n := strings.ToLower(e.Name)
		return n == "ctx" || strings.HasSuffix(n, "ctx") || strings.HasPrefix(n, "context")
	case *ast.CallExpr:
		if sel, ok := e.Fun.(*ast.SelectorExpr); ok {
			if x, ok := sel.X.(*ast.Ident); ok {
				return w.imports[x.Name] == "context" || sel.Sel.Name == "Context" && w.tParams[x.Name]
			}
		}
	}
	return false
}
