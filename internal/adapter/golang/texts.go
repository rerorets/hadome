package golang

import (
	"go/ast"
	"go/token"
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
	if len(parts) == 1 {
		return w.stringPart(parts[0], in, at, depth)
	}
	var j joined
	for _, p := range parts {
		t, ok := w.stringPart(p, in, at, depth)
		if !ok {
			j.hole()
			continue
		}
		j.add(t)
	}
	if !j.found {
		return facts.Text{}, false
	}
	if j.line == 0 {
		j.line = w.line(e)
	}
	return facts.Text{Path: at, Line: j.line, Value: j.b.String(), Holes: j.holes}, true
}

// stringPart reads one addend: a string literal, or a package-level string constant read where it
// is declared.
func (w *walker) stringPart(p ast.Expr, in scope, at string, depth int) (facts.Text, bool) {
	switch p := p.(type) {
	case *ast.BasicLit:
		if p.Kind != token.STRING {
			return facts.Text{}, false
		}
		// Only files that parse are walked, and the parser rejects every literal Unquote cannot read.
		s, _ := strconv.Unquote(p.Value)
		return facts.Text{Path: at, Line: w.line(p), Value: s}, true
	case *ast.Ident:
		if d, ok := in.strDecl(p.Name); ok && depth < maxDepth {
			return w.stringExpr(d.value, d.in, d.path, depth+1)
		}
	}
	return facts.Text{}, false
}

// joined builds the string of addends joined with +. A part that is not a string leaves one hole
// where it is joined. The line is that of the first literal.
type joined struct {
	b     strings.Builder
	holes []int
	line  int
	found bool
}

func (j *joined) add(t facts.Text) {
	for _, h := range t.Holes {
		j.holes = append(j.holes, j.b.Len()+h)
	}
	if !j.found {
		j.line = t.Line
	}
	j.b.WriteString(t.Value)
	j.found = true
}

func (j *joined) hole() {
	if len(j.holes) == 0 || j.holes[len(j.holes)-1] != j.b.Len() {
		j.holes = append(j.holes, j.b.Len())
	}
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

// visitText records the strings a node of the test holds and the names they are bound to. It
// returns false when it read the whole node, a string joined with +, so that its parts are not read
// again.
func (w *walker) visitText(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.AssignStmt:
		if n.Tok == token.DEFINE || n.Tok == token.ASSIGN {
			w.bindStrs(n.Lhs, n.Rhs, n.Tok == token.DEFINE)
		}
	case *ast.GenDecl:
		w.bindDecl(n)
	case *ast.Ident:
		w.readConst(n)
	case *ast.BasicLit:
		w.literal(n)
	case *ast.BinaryExpr:
		return !w.concat(n)
	}
	return true
}
