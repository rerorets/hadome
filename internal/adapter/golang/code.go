// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

package golang

import (
	"go/ast"
	"go/scanner"
	"go/token"
	"go/types"
	"strconv"

	"github.com/rerorets/hadome/internal/facts"
)

// funcBody fingerprints a function from its type parameters (or else its parameters) to its closing
// brace, as body does a test's, with each parameter, named result and local variable replaced by its
// number in the order they first appear: copies with renamed variables have the same Body. Names
// from outside the function (packages, package variables, fields, the functions it calls) stay,
// since they decide what the body means. The receiver is left out with the name. Statement ends are
// kept, written or inserted alike, except before a closing bracket, where layout alone decides
// whether the scanner inserts one: return; f() and return f() are different bodies.
func funcBody(fset *token.FileSet, src []byte, d *ast.FuncDecl) (string, int) {
	locals := map[int]*ast.Object{} // the identifiers that name a local, by offset, with what they name
	start := d.Type.Params.Pos()
	if d.Type.TypeParams != nil {
		start = d.Type.TypeParams.Pos()
	}
	from, to := fset.Position(start).Offset, fset.Position(d.End()).Offset
	inside := func(p token.Pos) bool { return p >= d.Pos() && p < d.End() }
	fieldKeys := map[*ast.Ident]bool{} // keys of struct literals, which the parser resolves as if they were names in scope
	ast.Inspect(d, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CompositeLit:
			if _, isMap := x.Type.(*ast.MapType); !isMap {
				for _, e := range x.Elts {
					if kv, ok := e.(*ast.KeyValueExpr); ok {
						if k, ok := kv.Key.(*ast.Ident); ok {
							fieldKeys[k] = true
						}
					}
				}
			}
		case *ast.Ident:
			if x.Obj != nil && x.Obj.Kind == ast.Var && inside(x.Obj.Pos()) && !fieldKeys[x] {
				locals[fset.Position(x.Pos()).Offset-from] = x.Obj
			}
		}
		return true
	})
	var s scanner.Scanner
	file := token.NewFileSet().AddFile("", -1, to-from)
	s.Init(file, src[from:to], nil, 0)
	numbers := map[*ast.Object]string{}
	var toks []string
	for {
		pos, tok, lit := s.Scan()
		if len(toks) > 0 && toks[len(toks)-1] == ";" && (tok == token.RBRACE || tok == token.RPAREN) {
			toks = toks[:len(toks)-1]
		}
		switch obj := locals[file.Offset(pos)]; {
		case tok == token.EOF:
			if len(toks) > 0 && toks[len(toks)-1] == ";" {
				toks = toks[:len(toks)-1]
			}
			return facts.Fingerprint(toks), len(toks)
		case tok == token.SEMICOLON:
			toks = append(toks, ";")
		case tok == token.IDENT && obj != nil:
			if numbers[obj] == "" {
				numbers[obj] = "$" + strconv.Itoa(len(numbers))
			}
			toks = append(toks, numbers[obj])
		case lit != "":
			toks = append(toks, lit)
		default:
			toks = append(toks, tok.String())
		}
	}
}

// durationUnits are the time package's constants a duration is built from.
var durationUnits = map[string]bool{
	"Nanosecond": true, "Microsecond": true, "Millisecond": true, "Second": true, "Minute": true, "Hour": true,
}

// literals finds the values in a function's body that belong in configuration
// (docs/design/rules-code.md): strings shaped like a URL, an IP address or a host and port,
// durations built from literals (30 * time.Second), and literals given to fields whose names the
// configuration lists. Constants declared in the body are named, so their values do not count.
func literals(fset *token.FileSet, src []byte, body *ast.BlockStmt, imports map[string]string, names map[string]bool) []facts.Literal {
	if body == nil {
		return nil
	}
	timePkg := ""
	for name, p := range imports {
		if p == "time" {
			timePkg = name
		}
	}
	var out []facts.Literal
	add := func(kind facts.LiteralKind, n ast.Node) {
		out = append(out, facts.Literal{Kind: kind, Line: fset.Position(n.Pos()).Line,
			Evidence: facts.Evidence(string(src[fset.Position(n.Pos()).Offset:fset.Position(n.End()).Offset]))})
	}
	used := map[ast.Node]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		if d, ok := n.(*ast.GenDecl); ok && d.Tok == token.CONST || used[n] {
			return false
		}
		markUsed(n, used)
		if kind := literalKind(n, timePkg, names); kind != "" {
			add(kind, n)
			return false
		}
		return true
	})
	return out
}

// markUsed notes the operands of n that are recognised or calculated with rather than settings: a
// URL compared with a value or given to strings.HasPrefix, a duration added to a time or compared
// with one: now.Add(-24 * time.Hour), t.Sub(start) > 5 * time.Minute, deadline + 2*time.Hour. A
// string joined with + is still a setting: "https://api.shop.internal" + path. Of a sum, only a
// product next to something else counts as calculated with; marking a string or a number does
// nothing, since neither is read as a duration.
func markUsed(n ast.Node, used map[ast.Node]bool) {
	switch x := n.(type) {
	case *ast.BinaryExpr:
		if facts.Comparison(x.Op.String()) {
			used[ast.Unparen(x.X)], used[ast.Unparen(x.Y)] = true, true
		}
		if x.Op == token.ADD || x.Op == token.SUB {
			l, r := ast.Unparen(x.X), ast.Unparen(x.Y)
			if product(l) != product(r) { // a time and a duration; 30*time.Second + 5*time.Second is a setting
				used[l], used[r] = true, true
			}
		}
	case *ast.CallExpr:
		if name := calleeName(x.Fun); facts.Compares(name) || facts.MovesTime(name) {
			for _, a := range x.Args {
				used[ast.Unparen(a)] = true
			}
		}
	}
}

// product reports a product, such as 30 * time.Second.
func product(e ast.Expr) bool {
	b, ok := e.(*ast.BinaryExpr)
	return ok && b.Op == token.MUL
}

// calleeName is the last name of a call's function: HasPrefix of strings.HasPrefix.
func calleeName(fun ast.Expr) string {
	switch f := ast.Unparen(fun).(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	}
	return ""
}

// literalKind is the shape of n when n is a value that belongs in configuration, or "".
func literalKind(n ast.Node, timePkg string, names map[string]bool) facts.LiteralKind {
	switch x := n.(type) {
	case *ast.BasicLit:
		if s, err := strconv.Unquote(x.Value); err == nil && x.Kind == token.STRING {
			return facts.ConfigString(s)
		}
	case *ast.BinaryExpr:
		if timePkg != "" && x.Op == token.MUL && isDuration(x, timePkg) {
			return facts.LiteralDuration
		}
	case *ast.KeyValueExpr:
		if key, ok := x.Key.(*ast.Ident); ok && literal(x.Value) {
			return facts.NamedLiteral(key.Name, number(x.Value), zero(x.Value), names)
		}
	case *ast.AssignStmt:
		return fieldLiteral(x, names)
	}
	return ""
}

// fieldLiteral is the shape of a literal assigned to a field, s.Timeout = 30, by the field's name.
func fieldLiteral(x *ast.AssignStmt, names map[string]bool) facts.LiteralKind {
	if len(x.Lhs) != 1 || len(x.Rhs) != 1 || !literal(x.Rhs[0]) {
		return ""
	}
	sel, ok := x.Lhs[0].(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	return facts.NamedLiteral(sel.Sel.Name, number(x.Rhs[0]), zero(x.Rhs[0]), names)
}

// isDuration reports a product of a time unit and a number written out: 30 * time.Second,
// time.Duration(30) * time.Millisecond, time.Minute * 5.
func isDuration(x *ast.BinaryExpr, timePkg string) bool {
	unit := func(e ast.Expr) bool {
		sel, ok := ast.Unparen(e).(*ast.SelectorExpr)
		if !ok {
			return false
		}
		pkg, ok := sel.X.(*ast.Ident)
		return ok && pkg.Name == timePkg && durationUnits[sel.Sel.Name]
	}
	num := func(e ast.Expr) bool {
		e = ast.Unparen(e)
		if c, ok := e.(*ast.CallExpr); ok && len(c.Args) == 1 { // time.Duration(30)
			if sel, ok := c.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Duration" {
				if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == timePkg {
					e = ast.Unparen(c.Args[0])
				}
			}
		}
		return number(e) && !zero(e)
	}
	return unit(x.X) && num(x.Y) || unit(x.Y) && num(x.X)
}

// number reports a number written out, possibly negative.
func number(e ast.Expr) bool {
	e = ast.Unparen(e)
	if u, ok := e.(*ast.UnaryExpr); ok && u.Op == token.SUB {
		e = u.X
	}
	b, ok := e.(*ast.BasicLit)
	return ok && (b.Kind == token.INT || b.Kind == token.FLOAT)
}

// zero reports a number written out whose value is 0.
func zero(e ast.Expr) bool {
	b, ok := ast.Unparen(e).(*ast.BasicLit)
	return ok && (b.Kind == token.INT || b.Kind == token.FLOAT) && facts.Zero(b.Value)
}

// literal reports a number or a string written out.
func literal(e ast.Expr) bool {
	if number(e) {
		return true
	}
	b, ok := ast.Unparen(e).(*ast.BasicLit)
	return ok && b.Kind == token.STRING
}

// errorFuncs are functions and methods outside the repository whose last result is an error,
// "<import path>.<Name>" and "<import path>.<Type>.<Method>". fmt's Fprint functions are left out:
// _, _ = fmt.Fprintf(w, …) is how Go code says a write to a response or a terminal may fail
// unseen, and reporting each would bury everything else.
var errorFuncs = func() map[string]bool {
	m := map[string]bool{}
	add := func(owner string, names ...string) {
		for _, n := range names {
			m[owner+"."+n] = true
		}
	}
	add("os", "Remove", "RemoveAll", "Mkdir", "MkdirAll", "MkdirTemp", "Rename", "Chmod", "Chdir", "Chown", "Chtimes",
		"Setenv", "Unsetenv", "WriteFile", "ReadFile", "Symlink", "Link", "Truncate", "Open", "Create", "OpenFile",
		"CreateTemp", "Stat", "Lstat", "ReadDir", "Getwd", "Hostname", "Executable", "UserHomeDir", "UserConfigDir", "UserCacheDir")
	add("os.File", "Close", "Sync", "Write", "WriteString", "WriteAt", "Read", "ReadAt", "Seek", "Truncate", "Chmod",
		"Chown", "Chdir", "SetDeadline", "SetReadDeadline", "SetWriteDeadline", "Readdir", "Readdirnames", "ReadDir", "Stat")
	add("io", "Copy", "CopyN", "CopyBuffer", "ReadFull", "ReadAtLeast", "WriteString", "ReadAll")
	add("io/ioutil", "ReadFile", "WriteFile", "ReadAll", "ReadDir", "TempDir", "TempFile")
	add("strconv", "Atoi", "ParseInt", "ParseUint", "ParseFloat", "ParseBool", "ParseComplex", "Unquote", "UnquoteChar")
	add("encoding/json", "Marshal", "MarshalIndent", "Unmarshal", "Compact", "Indent")
	add("encoding/json.Encoder", "Encode")
	add("encoding/json.Decoder", "Decode")
	add("encoding/xml", "Marshal", "MarshalIndent", "Unmarshal")
	add("encoding/hex", "DecodeString", "Decode")
	add("net/url", "Parse", "ParseRequestURI", "ParseQuery", "QueryUnescape", "PathUnescape")
	add("time", "Parse", "ParseInLocation", "ParseDuration", "LoadLocation")
	add("net/http", "NewRequest", "NewRequestWithContext", "Get", "Post", "Head", "PostForm", "ListenAndServe", "ListenAndServeTLS")
	add("net/http.Server", "Shutdown", "Close", "ListenAndServe", "ListenAndServeTLS", "Serve")
	add("net/http.Client", "Do", "Get", "Post", "Head", "PostForm")
	add("net", "Dial", "DialTimeout", "Listen", "ListenPacket", "ResolveTCPAddr", "ResolveUDPAddr", "LookupHost", "SplitHostPort")
	add("os/exec.Cmd", "Run", "Start", "Wait", "Output", "CombinedOutput")
	add("path/filepath", "Abs", "Rel", "Walk", "WalkDir", "EvalSymlinks", "Glob")
	add("regexp", "Compile", "CompilePOSIX")
	add("database/sql", "Open")
	add("database/sql.DB", "Close", "Ping", "PingContext", "Exec", "ExecContext", "Begin", "BeginTx", "Query", "QueryContext", "Prepare", "PrepareContext")
	add("database/sql.Tx", "Commit", "Rollback", "Exec", "ExecContext", "Query", "QueryContext", "Prepare", "PrepareContext")
	add("database/sql.Conn", "Close", "Exec", "ExecContext", "PingContext", "BeginTx")
	add("database/sql.Stmt", "Close", "Exec", "ExecContext", "Query", "QueryContext")
	add("database/sql.Rows", "Close", "Err", "Scan")
	add("database/sql.Row", "Scan", "Err")
	return m
}()

// swallow records an error assigned to _ (docs/design/rules-code.md): _ = f() or n, _ := f(), where
// f's last result is declared as error. A call whose callee's results the adapter cannot read is not
// reported.
func (w *walker) swallow(as *ast.AssignStmt) {
	if w.trace == nil || len(as.Rhs) != 1 {
		return
	}
	last, ok := as.Lhs[len(as.Lhs)-1].(*ast.Ident)
	if !ok || last.Name != "_" {
		return
	}
	c, ok := ast.Unparen(as.Rhs[0]).(*ast.CallExpr)
	if !ok || !w.returnsError(c) {
		return
	}
	w.trace.swallows = append(w.trace.swallows, facts.Swallow{Line: w.fset.Position(as.Pos()).Line,
		Evidence: facts.Evidence(types.ExprString(as.Rhs[0]))})
}

// returnsError reports whether the call's last result is declared as error: a function or method of
// the repository, or one in errorFuncs.
func (w *walker) returnsError(c *ast.CallExpr) bool {
	fun := ast.Unparen(c.Fun)
	switch f := fun.(type) {
	case *ast.IndexExpr:
		fun = f.X
	case *ast.IndexListExpr:
		fun = f.X
	}
	if id, ok := fun.(*ast.Ident); ok {
		if _, local := w.lookup(id.Name); local {
			return false // a function held in a variable of the body, which may shadow one of the package
		}
	}
	if id := w.qualify(fun, func(id string) bool { return w.idx.funcs[id] }); id != "" {
		return w.lastIsError(id)
	}
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if w.isPackage(sel.X) {
		return errorFuncs[w.imports[sel.X.(*ast.Ident).Name]+"."+sel.Sel.Name]
	}
	t := w.valueType(sel.X)
	switch {
	case t == nil:
		return false
	case t.id != "":
		return w.lastIsError(t.id + "." + sel.Sel.Name)
	case t.ext != "":
		return errorFuncs[unversioned(t.ext)+"."+sel.Sel.Name]
	}
	return false
}

// lastIsError reports whether a function or method of the repository declares error as its last
// result.
func (w *walker) lastIsError(id string) bool {
	refs := pick(w.idx.results[id], func(refs []declRef) string { return refs[0].in.build }, w.build)
	if len(refs) == 0 {
		return false
	}
	e, ok := refs[len(refs)-1].typ.(*ast.Ident)
	return ok && e.Name == "error"
}
