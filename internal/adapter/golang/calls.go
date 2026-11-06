package golang

import (
	"go/ast"
	"go/types"
	"sort"
	"strconv"

	"github.com/rerorets/hadome/internal/facts"
)

// recordVars remembers the type of variables created from the repository's own constructors or
// composite literals, so that later method calls on them resolve to a target.
func (w *walker) recordVars(as *ast.AssignStmt) {
	if len(as.Lhs) == 0 || len(as.Rhs) == 0 {
		return
	}
	name, ok := as.Lhs[0].(*ast.Ident)
	if !ok {
		return
	}
	if t := w.typeOf(as.Rhs[0]); t != "" {
		w.vars[name.Name] = t
	} else {
		delete(w.vars, name.Name) // a local of the same name hides a package-level variable
	}
}

func (w *walker) typeOf(e ast.Expr) string {
	if u, ok := e.(*ast.UnaryExpr); ok {
		e = u.X
	}
	switch e := e.(type) {
	case *ast.CompositeLit:
		return w.qualify(e.Type, func(id string) bool { return w.idx.types[id] })
	case *ast.CallExpr:
		if fn := w.qualify(e.Fun, func(id string) bool { _, ok := w.idx.funcs[id]; return ok }); fn != "" {
			return w.idx.funcs[fn]
		}
	}
	return ""
}

// qualify turns pkg.Name (or Name in the same package) into "<import path>.<Name>" when known.
func (w *walker) qualify(e ast.Expr, known func(string) bool) string {
	switch e := e.(type) {
	case *ast.SelectorExpr:
		if pkg, ok := e.X.(*ast.Ident); ok {
			if p, ok := w.imports[pkg.Name]; ok && known(p+"."+e.Sel.Name) {
				return p + "." + e.Sel.Name
			}
		}
	case *ast.Ident:
		if w.samePkg && known(w.pkgPath+"."+e.Name) {
			return w.pkgPath + "." + e.Name
		}
	}
	return ""
}

func (w *walker) call(c *ast.CallExpr) {
	if w.passesTool(c) && !w.readable(c) {
		w.test.AssertsUnknown = true
	}
	switch fun := c.Fun.(type) {
	case *ast.SelectorExpr:
		w.selectorCall(c, fun)
	case *ast.Ident: // builtins and helpers declared in test files are neither targets nor unresolved
		if id := w.qualify(fun, func(id string) bool { _, ok := w.idx.funcs[id]; return ok }); id != "" {
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
	x, ok := sel.X.(*ast.Ident)
	if !ok {
		// A method on the result of a call or literal, for example store.New().Len().
		if t := w.typeOf(sel.X); t != "" && ast.IsExported(sel.Sel.Name) {
			w.record(t+"."+sel.Sel.Name, c)
		} else {
			w.test.Unresolved++
		}
		return
	}
	name := sel.Sel.Name
	if p, ok := w.imports[x.Name]; ok && !w.shadowed(x.Name) {
		w.packageCall(c, p, name)
		return
	}
	if w.tParams[x.Name] {
		w.toolCall(c, name)
		return
	}
	if t, ok := w.vars[x.Name]; ok && ast.IsExported(name) {
		w.record(t+"."+name, c)
		return
	}
	w.test.Unresolved++
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
// it: a helper of the test files, the tool's own methods, an assertion library or a configured one.
func (w *walker) readable(c *ast.CallExpr) bool {
	switch fun := c.Fun.(type) {
	case *ast.Ident:
		_, ok := w.helpers[fun.Name]
		return ok
	case *ast.SelectorExpr:
		if x, ok := fun.X.(*ast.Ident); ok {
			p, imported := w.imports[x.Name]
			return w.tParams[x.Name] || imported && (assertImports[p] || w.tables.asserts[p+"."+fun.Sel.Name])
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
			w.collectTParams(h.decl.Type, h.decl.Body)
			prev := w.curPath
			w.curPath = h.path
			ast.Inspect(h.decl.Body, w.visit)
			w.curPath = prev
			delete(w.walking, fn.Name)
			return
		}
		w.test.AssertsUnknown = true
	default:
		w.test.AssertsUnknown = true
	}
}

func (w *walker) shadowed(name string) bool { _, ok := w.vars[name]; return ok }

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
		switch _, fn := w.idx.funcs[p+"."+name]; {
		case fn:
			w.record(p+"."+name, c)
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
