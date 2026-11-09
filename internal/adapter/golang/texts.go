package golang

import (
	"go/ast"
	"go/token"
	"go/types"
	"sort"
	"strconv"
	"strings"

	"github.com/rerorets/hadome/internal/facts"
)

// A test's strings (facts.Test.Texts) are handed over for the languages written inside strings,
// such as SQL, to read. This adapter only says where each string is, what it holds and whether it
// was passed to a call that takes SQL; it never reads what the string says.

// textKey identifies one string: where it starts and what it holds.
type textKey struct {
	path  string
	line  int
	value string
}

// textCount is one string recorded in the test, with how often it is used.
type textCount struct {
	text  facts.Text
	times int
}

// strDecl is a package-level constant or variable whose value is a string expression.
type strDecl struct {
	value ast.Expr
	path  string
	in    scope
}

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

// recordText records one use of a string where it is written.
func (w *walker) recordText(t facts.Text) {
	k := textKey{t.Path, t.Line, t.Value}
	tc := w.texts[k]
	if tc == nil {
		tc = &textCount{text: t}
		w.texts[k] = tc
	}
	tc.times++
	if w.loops > 0 {
		tc.times++
	}
}

// literal records a string literal outside a joined string.
func (w *walker) literal(n *ast.BasicLit) {
	if t, ok := w.stringExpr(n, w.scope, w.curPath, 0); ok {
		w.recordText(t)
	}
}

// concat records a string joined with +, and walks its parts that are not strings. It returns false
// when n is not a joined string, so that the walker goes on into it as usual.
func (w *walker) concat(n *ast.BinaryExpr) bool {
	t, ok := w.stringExpr(n, w.scope, w.curPath, 0)
	if !ok {
		return false
	}
	w.recordText(t)
	for _, part := range addends(n) {
		if _, ok := w.stringExpr(part, w.scope, w.curPath, 0); !ok {
			w.inspect(part)
		}
	}
	return true
}

// readConst records a read of a package-level string constant or variable.
func (w *walker) readConst(n *ast.Ident) {
	if w.fields[n] || w.callees[n] {
		return
	}
	if d, ok := w.strDecl(n.Name); ok {
		if t, ok := w.stringExpr(d.value, d.in, d.path, 1); ok {
			w.recordText(t)
		}
	}
}

// strDecl finds the package-level string a name refers to where the walker is: the test package's
// own, then the product package's when the test is inside it. A local name hides both.
func (w *walker) strDecl(name string) (strDecl, bool) {
	return w.scope.strDecl(name)
}

func (s scope) strDecl(name string) (strDecl, bool) {
	if s.shadowed(name) {
		return strDecl{}, false
	}
	if s.testKey != "" {
		if d, ok := s.idx.strs[s.testKey+"."+name]; ok {
			return d, true
		}
	}
	if s.samePkg {
		d, ok := s.idx.strs[s.pkgPath+"."+name]
		return d, ok
	}
	return strDecl{}, false
}

// addends flattens a + b + c, through parentheses.
func addends(e ast.Expr) []ast.Expr {
	e = ast.Unparen(e)
	if b, ok := e.(*ast.BinaryExpr); ok && b.Op == token.ADD {
		return append(addends(b.X), addends(b.Y)...)
	}
	return []ast.Expr{e}
}

// stringExpr reads e as a string the code holds: a literal, a package-level string constant, or
// literals and such constants joined with +. A part that is neither leaves a hole. It is not a
// string when no part is a literal or a constant. depth bounds constants defined by constants.
func (w *walker) stringExpr(e ast.Expr, in scope, at string, depth int) (facts.Text, bool) {
	parts := addends(e)
	var b strings.Builder
	var holes []int
	line, found := 0, false
	for _, p := range parts {
		switch p := p.(type) {
		case *ast.BasicLit:
			if p.Kind != token.STRING {
				return facts.Text{}, false
			}
			s, err := strconv.Unquote(p.Value)
			if err != nil {
				return facts.Text{}, false
			}
			if !found {
				line, found = w.line(p), true
			}
			b.WriteString(s)
			continue
		case *ast.Ident:
			if d, ok := in.strDecl(p.Name); ok && depth < maxDepth {
				if t, ok := w.stringExpr(d.value, d.in, d.path, depth+1); ok {
					if !found && len(parts) == 1 {
						return t, true // the constant itself, where it is declared
					}
					for _, h := range t.Holes {
						holes = append(holes, b.Len()+h)
					}
					b.WriteString(t.Value)
					found = true
					continue
				}
			}
		}
		if len(parts) == 1 {
			return facts.Text{}, false
		}
		if len(holes) == 0 || holes[len(holes)-1] != b.Len() {
			holes = append(holes, b.Len())
		}
	}
	if !found {
		return facts.Text{}, false
	}
	if line == 0 {
		line = w.line(e)
	}
	return facts.Text{Path: at, Line: line, Value: b.String(), Holes: holes}, true
}

// strKeys returns the strings an expression stands for: a string expression, or a local name bound
// only to strings. ok is false when that is not known.
func (w *walker) strKeys(e ast.Expr) ([]textKey, bool) {
	e = ast.Unparen(e)
	if id, ok := e.(*ast.Ident); ok {
		for b := w.local; b != nil; b = b.parent {
			if keys, bound := b.strs[id.Name]; bound {
				return keys, keys != nil
			}
			if _, declared := b.vars[id.Name]; declared {
				return nil, false
			}
		}
	}
	t, ok := w.stringExpr(e, w.scope, w.curPath, 0)
	if !ok {
		return nil, false
	}
	return []textKey{{t.Path, t.Line, t.Value}}, true
}

// bindStrs remembers which strings a local name holds after it is set: nil when it is set to
// something else, so that passing it to SQL is not taken for passing a known string.
func (w *walker) bindStrs(names []ast.Expr, values []ast.Expr, define bool) {
	for i, l := range names {
		id, ok := l.(*ast.Ident)
		if !ok || id.Name == "_" {
			continue
		}
		var keys []textKey
		if len(values) == len(names) {
			keys, _ = w.strKeys(values[i])
		}
		b := w.local
		if !define {
			for o := w.local; o != nil; o = o.parent {
				if _, ok := o.vars[id.Name]; ok {
					b = o
					break
				}
			}
		}
		b.strs[id.Name] = keys
	}
}

// pendStrs binds a helper's parameters to the strings the caller passed, for bindParams to put
// into the helper's block.
func (w *walker) pendStrs(ft *ast.FuncType, c *ast.CallExpr) {
	w.pendingStrs = map[string][]textKey{}
	i := 0
	for _, f := range ft.Params.List {
		for _, n := range f.Names {
			if i < len(c.Args) {
				if keys, ok := w.strKeys(c.Args[i]); ok {
					w.pendingStrs[n.Name] = keys
				}
			}
			i++
		}
	}
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

// textFacts returns the test's strings, a string passed to SQL anywhere marked so, sorted.
func (w *walker) textFacts() []facts.Text {
	out := make([]facts.Text, 0, len(w.texts))
	for k, tc := range w.texts {
		t := tc.text
		t.Times = tc.times
		if !t.Dynamic && w.sinkKeys[k] {
			t.Sink = facts.TextSinkSQL
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Value < b.Value
	})
	if len(out) == 0 {
		return nil
	}
	return out
}

// addStrs indexes the package-level constants and variables of a file whose value is a string
// expression, under the package (product and support files) or the test package (test files).
func (idx *index) addStrs(pf parsedFile, in scope) {
	key := pf.pkgPath
	if pf.test {
		key = in.testKey
	}
	for _, decl := range pf.file.Decls {
		d, ok := decl.(*ast.GenDecl)
		if !ok || d.Tok != token.CONST && d.Tok != token.VAR {
			continue
		}
		for _, s := range d.Specs {
			vs, ok := s.(*ast.ValueSpec)
			if !ok || len(vs.Values) != len(vs.Names) {
				continue
			}
			for i, n := range vs.Names {
				if isStringish(vs.Values[i]) {
					idx.strs[key+"."+n.Name] = strDecl{value: vs.Values[i], path: pf.path, in: in}
				}
			}
		}
	}
}

// isStringish reports whether an expression may be a string: a string literal among its + parts,
// or only names, which may be string constants (stringExpr decides when it is read).
func isStringish(e ast.Expr) bool {
	names := true
	for _, p := range addends(e) {
		if l, ok := p.(*ast.BasicLit); ok && l.Kind == token.STRING {
			return true
		}
		if _, ok := p.(*ast.Ident); !ok {
			names = false
		}
	}
	return names
}

// bindDecl remembers the strings of var and const declarations inside a function.
func (w *walker) bindDecl(d *ast.GenDecl) {
	if d.Tok != token.VAR && d.Tok != token.CONST {
		return
	}
	for _, spec := range d.Specs {
		if vs, ok := spec.(*ast.ValueSpec); ok {
			names := make([]ast.Expr, len(vs.Names))
			for i, n := range vs.Names {
				names[i] = n
			}
			w.bindStrs(names, vs.Values, true)
		}
	}
}
