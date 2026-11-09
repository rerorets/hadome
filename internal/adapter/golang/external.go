package golang

import (
	"go/ast"
	"strings"
)

// externalResults gives the concrete type of the first result of functions and methods of modules
// the adapter does not read, so that a method called on the result is known to run that module's
// code. Keys and values are written without a major version suffix ("github.com/jackc/pgx", not
// ".../pgx/v5"). Results that are interfaces (pgx.Conn.Begin returns pgx.Tx) are left out: a method
// of one may run code of the product.
var externalResults = func() map[string]string {
	m := map[string]string{}
	add := func(owner, result string, names ...string) {
		for _, n := range names {
			m[owner+"."+n] = result
		}
	}
	const sql = "database/sql"
	add(sql, sql+".DB", "Open", "OpenDB")
	for _, owner := range []string{sql + ".DB", sql + ".Conn"} {
		add(owner, sql+".Tx", "Begin", "BeginTx")
	}
	add(sql+".DB", sql+".Conn", "Conn")
	for _, owner := range []string{sql + ".DB", sql + ".Conn", sql + ".Tx"} {
		add(owner, sql+".Stmt", "Prepare", "PrepareContext")
	}
	add(sql+".Tx", sql+".Stmt", "Stmt", "StmtContext")
	for _, owner := range []string{sql + ".DB", sql + ".Conn", sql + ".Tx", sql + ".Stmt"} {
		add(owner, sql+".Rows", "Query", "QueryContext")
		add(owner, sql+".Row", "QueryRow", "QueryRowContext")
	}

	const sqlx = "github.com/jmoiron/sqlx"
	add(sqlx, sqlx+".DB", "Open", "Connect", "ConnectContext", "MustOpen", "MustConnect", "NewDb")
	add(sqlx+".DB", sqlx+".Tx", "Beginx", "BeginTxx", "MustBegin", "MustBeginTx")
	add(sqlx+".DB", sqlx+".Conn", "Connx")
	add(sqlx+".Conn", sqlx+".Tx", "BeginTxx")
	for _, owner := range []string{sqlx + ".DB", sqlx + ".Conn", sqlx + ".Tx"} {
		add(owner, sqlx+".Stmt", "Preparex", "PreparexContext")
		add(owner, sqlx+".NamedStmt", "PrepareNamed", "PrepareNamedContext")
	}
	for _, owner := range []string{sqlx + ".DB", sqlx + ".Conn", sqlx + ".Tx", sqlx + ".Stmt"} {
		add(owner, sqlx+".Rows", "Queryx", "QueryxContext")
		add(owner, sqlx+".Row", "QueryRowx", "QueryRowxContext")
	}

	const pgx, pool = "github.com/jackc/pgx", "github.com/jackc/pgx/pgxpool"
	add(pgx, pgx+".Conn", "Connect", "ConnectConfig")
	add(pool, pool+".Pool", "New", "NewWithConfig", "Connect", "ConnectConfig")
	add(pool+".Pool", pool+".Conn", "Acquire")
	add(pool+".Conn", pgx+".Conn", "Conn")
	return m
}()

// unversioned drops the major version elements of an import path: ".../pgx/v5/pgxpool" becomes
// ".../pgx/pgxpool", so that one entry of externalResults covers every major version.
func unversioned(p string) string {
	parts := strings.Split(p, "/")
	out := parts[:0]
	for i, e := range parts {
		if i > 0 && isMajorVersion(e) {
			continue
		}
		out = append(out, e)
	}
	return strings.Join(out, "/")
}

func isMajorVersion(e string) bool {
	return len(e) > 1 && e[0] == 'v' && strings.Trim(e[1:], "0123456789") == ""
}

// external returns the type pkg.T names when pkg is an imported package outside the repository,
// for a declaration the caller knows to be of a concrete type. It returns nil for anything else.
func (s scope) external(e ast.Expr) *vtype {
	sel, ok := ast.Unparen(e).(*ast.SelectorExpr)
	if !ok {
		return nil
	}
	x, ok := sel.X.(*ast.Ident)
	if !ok || s.shadowed(x.Name) {
		return nil
	}
	p, ok := s.imports[x.Name]
	if !ok || s.idx.internal(p) {
		return nil
	}
	return &vtype{ext: p + "." + sel.Sel.Name}
}

// externalResult returns the concrete type of the first result of name called on owner, when
// externalResults knows it. owner is an import path for a function of a package, or the ext of a
// value's type for a method.
func externalResult(owner, name string, n int, method bool) *vtype {
	if n != 0 {
		return nil
	}
	key := unversioned(owner)
	if method {
		dot := strings.LastIndex(owner, ".")
		key = unversioned(owner[:dot]) + owner[dot:]
	}
	if r, ok := externalResults[key+"."+name]; ok {
		return &vtype{ext: r}
	}
	return nil
}
