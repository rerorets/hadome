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

// recordVars types the variables an assignment sets, so that later method calls on them resolve to
// a target. := declares a variable, which hides one of the same name even when its type is not
// known; = keeps the type the variable was declared with.
func (w *walker) recordVars(as *ast.AssignStmt) {
	for i, l := range as.Lhs {
		name, ok := l.(*ast.Ident)
		if !ok || name.Name == "_" {
			continue
		}
		var t *vtype
		switch {
		case len(as.Rhs) == len(as.Lhs):
			t = w.valueType(as.Rhs[i])
		case len(as.Rhs) == 1: // s, q := store.Pair(), v, ok := m[k]
			t = w.nthValueType(as.Rhs[0], i, 0)
		}
		switch {
		case as.Tok == token.DEFINE:
			w.local.vars[name.Name] = t
		case t != nil:
			w.assign(name.Name, t)
		}
	}
}

// assign types a variable set with =, in the function that declares it.
func (w *walker) assign(name string, t *vtype) {
	for b := w.local; b != nil; b = b.parent {
		if _, ok := b.vars[name]; ok {
			b.vars[name] = t
			return
		}
	}
	w.local.vars[name] = t
}

// recordRange types the variables of a range over a typed slice, map or channel.
func (w *walker) recordRange(r *ast.RangeStmt) {
	if r.Tok != token.DEFINE {
		return
	}
	x := w.valueType(r.X)
	key, value := (*vtype)(nil), x.elem()
	if x.isChan() {
		key, value = value, nil
	}
	for _, v := range []struct {
		e ast.Expr
		t *vtype
	}{{r.Key, key}, {r.Value, value}} {
		if id, ok := v.e.(*ast.Ident); ok && id.Name != "_" {
			w.local.vars[id.Name] = v.t
		}
	}
}

// recordDecl types the variables and records the types a declaration inside a test declares.
func (w *walker) recordDecl(d *ast.GenDecl) {
	if d.Tok != token.VAR && d.Tok != token.TYPE {
		return
	}
	for _, spec := range d.Specs {
		switch s := spec.(type) {
		case *ast.ValueSpec:
			w.recordSpec(s)
		case *ast.TypeSpec:
			w.local.types[s.Name.Name] = s.Type
		}
	}
}

// recordSpec types the variables of var x T or var x = v.
func (w *walker) recordSpec(vs *ast.ValueSpec) {
	for i, name := range vs.Names {
		var t *vtype
		switch {
		case vs.Type != nil:
			t = w.resolve(vs.Type)
		case len(vs.Values) == len(vs.Names):
			t = w.valueType(vs.Values[i])
		case len(vs.Values) == 1:
			t = w.nthValueType(vs.Values[0], i, 0)
		}
		w.local.vars[name.Name] = t
	}
}

// bindParams types the parameters of the function being entered. A parameter declared with a type
// of the repository that is not an interface keeps that type. One declared with an interface, or
// with a type the adapter cannot read (http.Handler, a type parameter), takes the type of the value
// the caller passed, from passed; when that is not known either, it stays untyped, so a method
// called on it is unresolved rather than the interface's method, which runs no code of its own.
func (w *walker) bindParams(ft *ast.FuncType, passed map[string]*vtype) {
	for _, f := range ft.Params.List {
		typ := f.Type
		if e, ok := typ.(*ast.Ellipsis); ok { // ...T is a []T inside the function
			typ = &ast.ArrayType{Elt: e.Elt}
		}
		declared := w.resolve(typ)
		for _, n := range f.Names {
			if n.Name == "_" {
				continue
			}
			t := declared
			if declared == nil || declared.isInterface() {
				t = passed[n.Name]
			}
			w.local.vars[n.Name] = t
		}
	}
}

// funcLit walks a function literal in its own block, which sees the names around it.
func (w *walker) funcLit(lit *ast.FuncLit) {
	outer := w.local
	w.local = newBlock(outer)
	w.bindParams(lit.Type, nil)
	w.inspect(lit.Body)
	w.local = outer
}

func (w *walker) call(c *ast.CallExpr) {
	if w.passesTool(c) && !w.readable(c) {
		w.test.AssertsUnknown = true
	}
	switch fun := c.Fun.(type) {
	case *ast.SelectorExpr:
		w.selectorCall(c, fun)
	case *ast.Ident: // builtins and helpers declared in test files are neither targets nor unresolved
		if t, ok := w.lookup(fun.Name); ok && t != nil && t.method != "" {
			w.methodValueCall(t.method, c)
		} else if id := w.qualify(fun, func(id string) bool { _, ok := w.idx.funcs[id]; return ok }); id != "" {
			w.record(id, c)
		} else if w.qualify(fun, func(id string) bool { return w.idx.values[id] }) != "" {
			w.test.Unresolved++
		} else {
			w.followHelper(fun.Name, c)
		}
	case *ast.FuncLit:
	default:
		w.test.Unresolved++
	}
}

func (w *walker) selectorCall(c *ast.CallExpr, sel *ast.SelectorExpr) {
	if w.suiteCall(c, sel) {
		return
	}
	name := sel.Sel.Name
	if x, ok := sel.X.(*ast.Ident); ok {
		if p, ok := w.imports[x.Name]; ok && !w.shadowed(x.Name) {
			w.packageCall(c, p, name)
			return
		}
		if w.tParams[x.Name] {
			w.toolCall(c, name)
			return
		}
	}
	// A method of a value whose type is declared: a variable, a field (tc.store.Append()), the
	// result of a call (store.New().Len()) or a literal ((Config{}).Validate()).
	t := w.valueType(sel.X)
	if t != nil && t.id != "" && (ast.IsExported(name) || w.ownType(t.id)) {
		w.record(t.id+"."+name, c)
		return
	}
	if t != nil && t.ext != "" { // a call into another module, like one of its functions
		return
	}
	w.test.Unresolved++
}

// methodValueCall counts a call of a variable holding a method value as a call of the method,
// under the same visibility a call written s.Append() has.
func (w *walker) methodValueCall(method string, c *ast.CallExpr) {
	dot := strings.LastIndex(method, ".")
	if ast.IsExported(method[dot+1:]) || w.ownType(method[:dot]) {
		w.record(method, c)
		return
	}
	w.test.Unresolved++
}

// ownType reports whether a type belongs to the package the test is in, so that the test may call
// its unexported methods.
func (w *walker) ownType(id string) bool {
	rest, ok := strings.CutPrefix(id, w.pkgPath+".")
	return ok && w.samePkg && !strings.Contains(rest, ".")
}

// toolCall reads a call on the testing tool: t.Skip, t.Fatal and the like, or t.Run.
func (w *walker) toolCall(c *ast.CallExpr, name string) {
	switch {
	case name == "Skip" || name == "SkipNow" || name == "Skipf":
		w.skip(c)
	case failing[name]:
		w.test.Asserts++
	case name == "Run" && len(c.Args) == 2:
		w.subtest(c.Args[1])
	}
}

// record counts one call site of a target, and the argument passed as its scope.
func (w *walker) record(id string, c *ast.CallExpr) {
	cc := w.calls[id]
	if cc == nil {
		cc = &callCount{scopes: map[string]bool{}}
		w.calls[id] = cc
	}
	cc.times++
	if w.loops > 0 {
		cc.times++
	}
	pos, known := w.idx.scopes[id]
	if !known {
		pos = 0 // a method hadome did not index: its first argument is the best guess
	}
	if pos >= 0 && pos < len(c.Args) {
		cc.scopes[w.argText(c.Args[pos])] = true
	}
}

// callFacts returns the counted calls, sorted by target.
func (w *walker) callFacts() []facts.Call {
	out := make([]facts.Call, 0, len(w.calls))
	for id, cc := range w.calls {
		out = append(out, facts.Call{Target: id, Times: cc.times, Scopes: len(cc.scopes)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Target < out[j].Target })
	return out
}

// skip records a call to t.Skip. One inside an if, a switch or a select (or in a helper called from
// one) makes the test skip itself only in some cases; an unconditional one wins over it.
func (w *walker) skip(c *ast.CallExpr) {
	if w.test.Skip.Skipped {
		return
	}
	w.test.Skip = facts.Skip{Skipped: w.branches == 0, Conditional: w.branches > 0, Reason: firstString(c), Line: w.line(c)}
}

// readSelector counts pkg.Name as a read when it names a product value that is not being called.
func (w *walker) readSelector(sel *ast.SelectorExpr) {
	if x, ok := sel.X.(*ast.Ident); ok && !w.shadowed(x.Name) {
		if p, ok := w.imports[x.Name]; ok && w.idx.internal(p) && w.idx.value(p+"."+sel.Sel.Name) {
			w.test.Reads++
		}
	}
}

// readIdent counts Name as a read when it names a value of the same package that is not being
// called, and is not the name after a dot.
func (w *walker) readIdent(n *ast.Ident) {
	if !w.fields[n] && !w.callees[n] && w.qualify(n, w.idx.value) != "" {
		w.test.Reads++
	}
}

// passesTool reports whether the call hands the testing tool to the function it calls.
func (w *walker) passesTool(c *ast.CallExpr) bool {
	for _, a := range c.Args {
		if id, ok := a.(*ast.Ident); ok && w.tParams[id.Name] {
			return true
		}
	}
	return false
}

// readable reports whether the adapter sees what a call that is given the testing tool does with
// it: a helper of the test files or of a test support package, the tool's own methods, an assertion
// library or a configured one.
func (w *walker) readable(c *ast.CallExpr) bool {
	switch fun := c.Fun.(type) {
	case *ast.Ident:
		_, ok := w.helpers[fun.Name]
		return ok
	case *ast.SelectorExpr:
		if x, ok := fun.X.(*ast.Ident); ok {
			p, imported := w.imports[x.Name]
			_, support := w.idx.support[p][fun.Sel.Name]
			return w.tParams[x.Name] || imported && (assertImports[p] || w.tables.asserts[p+"."+fun.Sel.Name] || support)
		}
	}
	return false
}

// subtest handles the function given to t.Run. A literal is walked with the test; a helper is
// followed; anything else may check something the adapter cannot see.
func (w *walker) subtest(fn ast.Expr) {
	switch fn := fn.(type) {
	case *ast.FuncLit:
	case *ast.Ident:
		if h, ok := w.helpers[fn.Name]; ok && !w.walking[fn.Name] {
			w.walking[fn.Name] = true
			w.walkHelper(h, w.args, nil)
			delete(w.walking, fn.Name)
			return
		}
		w.test.AssertsUnknown = true
	default:
		w.test.AssertsUnknown = true
	}
}

func (w *walker) packageCall(c *ast.CallExpr, p, name string) {
	if assertImports[p] || w.tables.asserts[p+"."+name] {
		w.test.Asserts++
	}
	if cat, ok := w.tables.calls[p+"."+name]; ok {
		w.hazard(cat, c)
		return
	}
	switch {
	case w.tables.mocks[p]:
		w.hazard(facts.HazardMock, c)
	case p == suitePkg && name == "Run" && len(c.Args) == 2:
		w.suite(c.Args[1])
	case assertImports[p]:
		if alwaysTrue(name, c) {
			w.hazard(facts.HazardAlwaysTrue, c)
		}
	case w.idx.internal(p):
		h, support := w.idx.support[p][name]
		switch _, fn := w.idx.funcs[p+"."+name]; {
		case fn:
			w.record(p+"."+name, c)
		case support:
			w.followSupport(p+"."+name, h, c)
		case w.idx.types[p+"."+name]: // a conversion runs no code of the package
		default: // a function variable, or a package hadome did not read
			w.test.Unresolved++
		}
	}
}

// alwaysTrue recognises testify assertions that cannot fail: True(t, true) and Equal(t, x, x) where
// x calls nothing.
func alwaysTrue(name string, c *ast.CallExpr) bool {
	switch name {
	case "True":
		if len(c.Args) >= 2 {
			id, ok := c.Args[1].(*ast.Ident)
			return ok && id.Name == "true"
		}
	case "False":
		if len(c.Args) >= 2 {
			id, ok := c.Args[1].(*ast.Ident)
			return ok && id.Name == "false"
		}
	case "Equal", "Exactly", "EqualValues":
		return len(c.Args) >= 3 && types.ExprString(c.Args[1]) == types.ExprString(c.Args[2]) && !holdsCall(c.Args[1])
	}
	return false
}

// holdsCall reports whether an expression calls something. Two calls spelled the same may return
// different values, so comparing them is a check that can fail, not a value compared with itself.
func holdsCall(e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if _, ok := n.(*ast.CallExpr); ok {
			found = true
		}
		return !found
	})
	return found
}

func firstString(c *ast.CallExpr) string {
	if len(c.Args) == 0 {
		return ""
	}
	if lit, ok := c.Args[0].(*ast.BasicLit); ok {
		s, err := strconv.Unquote(lit.Value)
		if err == nil {
			return s
		}
	}
	return "(computed)"
}
