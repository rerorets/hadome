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
	// ifaces are the parameters declared with one of the repository's interfaces, by name, and
	// through the methods called on them, <interface>.<method>: where such a call lands depends on
	// the value passed, so it is not among the calls.
	ifaces  map[string]string
	through map[string]int // -> the line of the first call
}

// throughIDs returns the calls through an interface that are not among the walker's calls, sorted,
// and adds their lines to at.
func (t *trace) throughIDs(w *walker, at map[string]int) []string {
	var out []string
	for c, line := range t.through {
		if w.calls[c] == nil {
			out = append(out, c)
			at[c] = line
		}
	}
	sort.Strings(out)
	return out
}

// interfaceParam records a parameter declared with one of the repository's interfaces.
func (t *trace) interfaceParam(name string, declared *vtype) {
	if t != nil && declared != nil && declared.id != "" && declared.isInterface() {
		t.ifaces[name] = declared.id
	}
}

// callThrough records a method called on a parameter declared with one of the repository's
// interfaces.
func (t *trace) callThrough(param, method string, line int) {
	if t == nil || t.ifaces[param] == "" {
		return
	}
	if id := t.ifaces[param] + "." + method; t.through[id] == 0 {
		t.through[id] = line
	}
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

// routeMethods are the names of the calls that register a handler with a string: a path for
// net/http's ServeMux and the routers built like it, a task type for asynq's ServeMux, a subject
// for NATS's Subscribe and QueueSubscribe, a schedule for robfig/cron's AddFunc.
var routeMethods = map[string]bool{"Handle": true, "HandleFunc": true, "Get": true, "Post": true, "Put": true,
	"Patch": true, "Delete": true, "Head": true, "Options": true, "Method": true, "MethodFunc": true, "Any": true,
	"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true,
	"Subscribe": true, "QueueSubscribe": true, "AddFunc": true}

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
			t.entries = append(t.entries, facts.Entry{Path: t.path, Line: line, Calls: callIDs(inner), CallAt: callLines(inner)})
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

// callLines returns the line of the first call of each ID the walker called.
func callLines(w *walker) map[string]int {
	out := make(map[string]int, len(w.calls))
	for id, cc := range w.calls {
		out[id] = cc.line
	}
	return out
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
func traceFile(rd reading, pf parsedFile, syms []facts.Symbol) ([]facts.Symbol, []facts.Entry) {
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
		t := &trace{units: map[string]bool{}, path: pf.path, fset: rd.fset, ifaces: map[string]string{}, through: map[string]int{}}
		w := newWalker(rd, pf, nil)
		w.testKey, w.trace = "", t
		if d.Recv != nil && len(d.Recv.List) == 1 && len(d.Recv.List[0].Names) == 1 {
			w.local.vars[d.Recv.List[0].Names[0].Name] = w.resolve(d.Recv.List[0].Type)
		}
		w.bindParams(d.Type, nil)
		ast.Inspect(d.Body, w.visit)
		entries = append(entries, t.entries...)
		if id != "" && handlerShape(d.Type, imports) {
			entries = append(entries, facts.Entry{Path: pf.path, Line: rd.fset.Position(d.Pos()).Line, Symbol: id})
		}
		i, ok := byID[id]
		if id == "" || !ok {
			continue
		}
		t.fill(&syms[i], w, pf.pkgPath)
	}
	return syms, entries
}

// fill sets what a function's walk found on its symbol: its calls with their lines, the units it
// uses other than its own package, and whether it does input or output.
func (t *trace) fill(s *facts.Symbol, w *walker, pkgPath string) {
	s.IO, s.Calls, s.CallAt = t.io, callIDs(w), callLines(w)
	s.Through = t.throughIDs(w, s.CallAt)
	if len(s.CallAt) == 0 {
		s.CallAt = nil
	}
	for u, used := range t.units {
		if used && u != pkgPath {
			s.Units = append(s.Units, u)
		}
	}
	sort.Strings(s.Units)
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
