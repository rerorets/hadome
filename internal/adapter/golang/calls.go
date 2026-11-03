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
	switch fun := c.Fun.(type) {
	case *ast.SelectorExpr:
		w.selectorCall(c, fun)
	case *ast.Ident: // builtins and helpers declared in test files are neither targets nor unresolved
		if id := w.qualify(fun, func(id string) bool { _, ok := w.idx.funcs[id]; return ok }); id != "" {
			w.record(id, c)
		} else {
			w.followHelper(fun.Name, c)
		}
	case *ast.FuncLit:
	default:
		w.test.Unresolved++
	}
}

func (w *walker) selectorCall(c *ast.CallExpr, sel *ast.SelectorExpr) {
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
		if name == "Skip" || name == "SkipNow" || name == "Skipf" {
			w.skip(c)
		}
		return
	}
	if t, ok := w.vars[x.Name]; ok && ast.IsExported(name) {
		w.record(t+"."+name, c)
		return
	}
	w.test.Unresolved++
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

func (w *walker) shadowed(name string) bool { _, ok := w.vars[name]; return ok }

func (w *walker) packageCall(c *ast.CallExpr, p, name string) {
	if cat, ok := w.tables.calls[p+"."+name]; ok {
		w.hazard(cat, c)
		return
	}
	switch {
	case w.tables.mocks[p]:
		w.hazard(facts.HazardMock, c)
	case assertImports[p]:
		if alwaysTrue(name, c) {
			w.hazard(facts.HazardAlwaysTrue, c)
		}
	case w.idx.internal(p):
		if _, ok := w.idx.funcs[p+"."+name]; ok {
			w.record(p+"."+name, c)
		}
	}
}

// alwaysTrue recognises testify assertions that cannot fail: True(t, true) and Equal(t, x, x).
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
		return len(c.Args) >= 3 && types.ExprString(c.Args[1]) == types.ExprString(c.Args[2])
	}
	return false
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
