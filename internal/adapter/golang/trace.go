package golang

import (
	"go/ast"
	"go/token"
	"sort"

	"github.com/rerorets/hadome/internal/facts"
)

// trace collects what one function of the product code reaches: whether it does input or output
// itself, the repository's units whose values it uses, and the routes it registers. Its methods do
// nothing on a nil trace, which is a test's.
type trace struct {
	io      bool
	units   map[string]bool
	entries []facts.Entry
	path    string
	fset    *token.FileSet
}

func (t *trace) doesIO() {
	if t != nil {
		t.io = true
	}
}

func (t *trace) usesUnit(p string) {
	if t != nil {
		t.units[p] = true
	}
}

// routeMethods are the names of the calls that register a handler for a path: net/http's ServeMux
// and the routers built like it.
var routeMethods = map[string]bool{"Handle": true, "HandleFunc": true, "Get": true, "Post": true, "Put": true,
	"Patch": true, "Delete": true, "Head": true, "Options": true, "Method": true, "MethodFunc": true, "Any": true,
	"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true}

// route records the handlers a call registers with a string path: mux.HandleFunc("/orders",
// h.CreateOrder). A function or method named there is an entry point; a function literal written
// there is entry code, whose calls are listed. It reports whether the call registers a route.
func (t *trace) route(w *walker, c *ast.CallExpr) bool {
	if t == nil || len(c.Args) < 2 {
		return false
	}
	sel, ok := c.Fun.(*ast.SelectorExpr)
	if lit, isLit := c.Args[0].(*ast.BasicLit); !ok || !routeMethods[sel.Sel.Name] || !isLit || lit.Kind != token.STRING {
		return false
	}
	for _, a := range c.Args[1:] {
		a = handlerArg(a)
		line := t.fset.Position(a.Pos()).Line
		if lit, ok := a.(*ast.FuncLit); ok {
			inner := &walker{scope: w.scope, fset: w.fset, tables: w.tables, tParams: map[string]bool{},
				calls: map[string]*callCount{}, walking: map[string]bool{}, curPath: w.curPath,
				callees: map[ast.Expr]bool{}, fields: map[*ast.Ident]bool{}, file: w.file}
			inner.local = newBlock(w.local)
			inner.bindParams(lit.Type, nil)
			ast.Inspect(lit.Body, inner.visit)
			t.entries = append(t.entries, facts.Entry{Path: t.path, Line: line, Calls: callIDs(inner)})
			continue
		}
		if id := w.funcRef(a); id != "" {
			t.entries = append(t.entries, facts.Entry{Path: t.path, Line: line, Symbol: id})
		}
	}
	return true
}

// handlerArg unwraps a conversion to a handler type: http.HandlerFunc(h.CreateOrder).
func handlerArg(a ast.Expr) ast.Expr {
	if c, ok := a.(*ast.CallExpr); ok && len(c.Args) == 1 {
		if sel, ok := c.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "HandlerFunc" {
			return c.Args[0]
		}
	}
	return a
}

// funcRef returns the ID of the function or method a value names without calling it: CreateOrder,
// orders.Create or h.CreateOrder.
func (w *walker) funcRef(e ast.Expr) string {
	if id := w.qualify(e, func(id string) bool { return w.idx.funcs[id] }); id != "" {
		return id
	}
	if sel, ok := e.(*ast.SelectorExpr); ok && !w.isPackage(sel.X) {
		if t := w.valueType(sel.X); t != nil && t.id != "" && w.idx.methods[t.id+"."+sel.Sel.Name] {
			return t.id + "." + sel.Sel.Name
		}
	}
	return ""
}

func callIDs(w *walker) []string {
	out := make([]string, 0, len(w.calls))
	for id := range w.calls {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// traceFile fills in the calls, units and input or output of the file's functions and methods,
// and returns the entry points the file declares or registers.
func traceFile(fset *token.FileSet, pf parsedFile, idx *index, tb *tables, syms []facts.Symbol) ([]facts.Symbol, []facts.Entry) {
	byID := map[string]int{}
	for i, s := range syms {
		byID[s.ID] = i
	}
	imports := fileImports(pf.file)
	var entries []facts.Entry
	for _, decl := range pf.file.Decls {
		d, ok := decl.(*ast.FuncDecl)
		if !ok || d.Body == nil {
			continue
		}
		id := funcID(pf.pkgPath, d)
		t := &trace{units: map[string]bool{}, path: pf.path, fset: fset}
		w := newWalker(fset, pf, idx, tb, nil)
		w.testKey, w.trace = "", t
		if d.Recv != nil && len(d.Recv.List) == 1 && len(d.Recv.List[0].Names) == 1 {
			w.local.vars[d.Recv.List[0].Names[0].Name] = w.resolve(d.Recv.List[0].Type)
		}
		w.bindParams(d.Type, nil)
		ast.Inspect(d.Body, w.visit)
		entries = append(entries, t.entries...)
		if id != "" && handlerShape(d.Type, imports) {
			entries = append(entries, facts.Entry{Path: pf.path, Line: fset.Position(d.Pos()).Line, Symbol: id})
		}
		i, ok := byID[id]
		if id == "" || !ok {
			continue
		}
		s := &syms[i]
		s.Traced, s.IO, s.Calls = true, t.io, callIDs(w)
		for u := range t.units {
			if u != pf.pkgPath {
				s.Units = append(s.Units, u)
			}
		}
		sort.Strings(s.Units)
	}
	return syms, entries
}

// handlerShape reports a function whose parameters are (http.ResponseWriter, *http.Request): an
// http.HandlerFunc, or the ServeHTTP of an http.Handler.
func handlerShape(ft *ast.FuncType, imports map[string]string) bool {
	var types []ast.Expr
	for _, f := range ft.Params.List {
		for range max(len(f.Names), 1) {
			types = append(types, f.Type)
		}
	}
	if len(types) != 2 {
		return false
	}
	is := func(e ast.Expr, name string) bool {
		sel, ok := e.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		pkg, ok := sel.X.(*ast.Ident)
		return ok && imports[pkg.Name] == "net/http" && sel.Sel.Name == name
	}
	star, ok := types[1].(*ast.StarExpr)
	return is(types[0], "ResponseWriter") && ok && is(star.X, "Request")
}
