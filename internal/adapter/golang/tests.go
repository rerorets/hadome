package golang

import (
	"go/ast"
	"go/scanner"
	"go/token"
	"go/types"
	"path"
	"strconv"
	"strings"

	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/facts"
)

// tables are the hazard tables: the built-in entries plus those the configuration adds.
// Configuration can add entries, never remove them.
type tables struct {
	calls map[string]facts.HazardCategory // "<import path>.<Name>" -> category
	mocks map[string]bool                 // import paths of mocking libraries
	io    ioTable
	// asserts are the functions configuration adds to the assertions, "<import path>.<Name>".
	asserts map[string]bool
}

func newTables(c config.Config) *tables {
	t := &tables{
		calls: map[string]facts.HazardCategory{
			"time.Now": facts.HazardClock, "time.Since": facts.HazardClock, "time.Until": facts.HazardClock,
			"time.Sleep": facts.HazardSleep,
		},
		mocks: map[string]bool{
			"github.com/golang/mock/gomock":      true,
			"go.uber.org/mock/gomock":            true,
			"github.com/stretchr/testify/mock":   true,
			"github.com/vektra/mockery/v2/mocks": true,
		},
		io:      newIOTable(c.IO),
		asserts: map[string]bool{},
	}
	for _, e := range c.Assertions {
		t.asserts[e] = true
	}
	for _, e := range c.Hazards["clock"] {
		t.calls[e] = facts.HazardClock
	}
	for _, e := range c.Hazards["sleep"] {
		t.calls[e] = facts.HazardSleep
	}
	for _, e := range c.Hazards["mock"] {
		t.mocks[e] = true
	}
	return t
}

// failing are the methods of the testing tool that fail the test.
var failing = map[string]bool{"Error": true, "Errorf": true, "Fatal": true, "Fatalf": true, "Fail": true, "FailNow": true}

var assertImports = map[string]bool{
	"github.com/stretchr/testify/assert":  true,
	"github.com/stretchr/testify/require": true,
}

// helper is a function declared in a test file that is not itself a test. Tests often reach the
// code under test only through helpers, so the walker follows calls into them.
type helper struct {
	decl *ast.FuncDecl
	path string
}

func helperKey(pf parsedFile) string { return path.Dir(pf.path) + "\x00" + pf.file.Name.Name }

// collectHelpers groups the helper functions of test files by directory and package name.
func collectHelpers(files []parsedFile) map[string]map[string]helper {
	out := map[string]map[string]helper{}
	for _, pf := range files {
		if !pf.test {
			continue
		}
		k := helperKey(pf)
		if out[k] == nil {
			out[k] = map[string]helper{}
		}
		for _, decl := range pf.file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Body != nil && !strings.HasPrefix(fn.Name.Name, "Test") {
				out[k][fn.Name.Name] = helper{decl: fn, path: pf.path}
			}
		}
	}
	return out
}

// walker collects the facts of one test function.
type walker struct {
	fset    *token.FileSet
	idx     *index
	tables  *tables
	pkgPath string
	samePkg bool
	imports map[string]string // local name -> import path
	tParams map[string]bool   // names bound to *testing.T anywhere in the test
	vars    map[string]string // local variable -> "<import path>.<Type>"
	test    facts.Test
	calls   map[string]*callCount
	helpers map[string]helper
	// walking holds the helpers being walked, so recursion stops; a helper called twice is walked
	// twice, so the calls inside it count twice.
	walking map[string]bool
	// args maps a helper's parameter names to what the caller passed, so that a scope passed
	// through a helper is the caller's value.
	args     map[string]string
	loops    int    // how many loops enclose the node being walked
	branches int    // how many ifs, switches and selects enclose it
	curPath  string // the file being walked: the test's, or a helper's
	// callees holds the expressions in call position, and fields the identifiers after a dot, so
	// that neither is taken for a read of a product value.
	callees map[ast.Expr]bool
	fields  map[*ast.Ident]bool
}

type callCount struct {
	times  int
	scopes map[string]bool
}

func extractTests(fset *token.FileSet, pf parsedFile, idx *index, tb *tables, helpers map[string]helper) []facts.Test {
	imports := fileImports(pf.file)
	var out []facts.Test
	for _, decl := range pf.file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Body == nil || !strings.HasPrefix(fn.Name.Name, "Test") || !takesT(fn.Type, imports) {
			continue
		}
		w := &walker{
			fset: fset, idx: idx, tables: tb, pkgPath: pf.pkgPath, imports: imports,
			samePkg: !strings.HasSuffix(pf.file.Name.Name, "_test"),
			tParams: map[string]bool{}, vars: map[string]string{}, calls: map[string]*callCount{},
			helpers: helpers, walking: map[string]bool{}, curPath: pf.path,
			callees: map[ast.Expr]bool{}, fields: map[*ast.Ident]bool{},
			test: facts.Test{Name: fn.Name.Name, Path: pf.path, Line: fset.Position(fn.Pos()).Line, Declared: declared(fn.Doc),
				Body: body(fset, pf.src, fn)},
		}
		w.collectTParams(fn.Type, fn.Body)
		ast.Inspect(fn.Body, w.visit)
		w.test.Calls = w.callFacts()
		out = append(out, w.test)
	}
	return out
}

// body fingerprints the test from its parameters to its closing brace, so the name is left out.
// The scanner drops comments and whitespace.
func body(fset *token.FileSet, src []byte, fn *ast.FuncDecl) string {
	from, to := fset.Position(fn.Type.Params.Pos()).Offset, fset.Position(fn.End()).Offset
	var s scanner.Scanner
	s.Init(token.NewFileSet().AddFile("", -1, to-from), src[from:to], nil, 0)
	var toks []string
	for {
		_, tok, lit := s.Scan()
		switch {
		case tok == token.EOF:
			return facts.Fingerprint(toks)
		case tok == token.SEMICOLON: // the scanner inserts them at some line ends, so they depend on layout
		case lit != "":
			toks = append(toks, lit)
		default:
			toks = append(toks, tok.String())
		}
	}
}

func fileImports(f *ast.File) map[string]string {
	m := map[string]string{}
	for _, s := range f.Imports {
		p, _ := strconv.Unquote(s.Path.Value)
		name := path.Base(p)
		if strings.HasPrefix(name, "v") && len(name) > 1 && strings.Trim(name[1:], "0123456789") == "" {
			name = path.Base(path.Dir(p)) // ".../foo/v2" is imported as foo
		}
		if s.Name != nil {
			name = s.Name.Name
		}
		m[name] = p
	}
	return m
}

func takesT(ft *ast.FuncType, imports map[string]string) bool {
	if ft.Params == nil || len(ft.Params.List) != 1 {
		return false
	}
	return isTestingT(ft.Params.List[0].Type, imports)
}

func isTestingT(e ast.Expr, imports map[string]string) bool {
	star, ok := e.(*ast.StarExpr)
	return ok && isTesting(star.X, "T", imports)
}

// isTool reports whether e is the testing tool's type as a helper takes it: *testing.T or testing.TB.
func isTool(e ast.Expr, imports map[string]string) bool {
	return isTestingT(e, imports) || isTesting(e, "TB", imports)
}

func isTesting(e ast.Expr, name string, imports map[string]string) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && imports[pkg.Name] == "testing" && sel.Sel.Name == name
}

// collectTParams records every name bound to *testing.T, including subtest closures.
func (w *walker) collectTParams(ft *ast.FuncType, body *ast.BlockStmt) {
	add := func(ft *ast.FuncType) {
		for _, f := range ft.Params.List {
			if isTool(f.Type, w.imports) {
				for _, n := range f.Names {
					w.tParams[n.Name] = true
				}
			}
		}
	}
	add(ft)
	ast.Inspect(body, func(n ast.Node) bool {
		if lit, ok := n.(*ast.FuncLit); ok {
			add(lit.Type)
		}
		return true
	})
}

func (w *walker) visit(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.AssignStmt:
		w.recordVars(n)
	case *ast.CallExpr:
		w.callees[ast.Unparen(n.Fun)] = true
		w.call(n)
	case *ast.SelectorExpr:
		w.fields[n.Sel] = true
		if !w.callees[n] {
			w.readSelector(n)
		}
	case *ast.Ident:
		if !w.fields[n] && !w.callees[n] && w.qualify(n, w.idx.value) != "" {
			w.test.Reads++
		}
	case *ast.ForStmt:
		w.inspect(n.Init)
		w.inLoop(n.Cond, n.Post, n.Body)
		return false
	case *ast.RangeStmt:
		w.inspect(n.X)
		w.inLoop(n.Body)
		return false
	case *ast.IfStmt:
		w.inspect(n.Init)
		w.inspect(n.Cond)
		w.inBranch(n.Body, n.Else)
		return false
	case *ast.SwitchStmt:
		w.inspect(n.Init)
		w.inspect(n.Tag)
		w.inBranch(n.Body)
		return false
	case *ast.TypeSwitchStmt:
		w.inspect(n.Init)
		w.inspect(n.Assign)
		w.inBranch(n.Body)
		return false
	case *ast.SelectStmt:
		w.inBranch(n.Body)
		return false
	}
	return true
}

// inBranch walks nodes that run only in some cases.
func (w *walker) inBranch(nodes ...ast.Node) {
	w.branches++
	for _, n := range nodes {
		w.inspect(n)
	}
	w.branches--
}

// inspect walks n, which is nil for a missing loop clause.
func (w *walker) inspect(n ast.Node) {
	if n != nil {
		ast.Inspect(n, w.visit)
	}
}

func (w *walker) inLoop(nodes ...ast.Node) {
	w.loops++
	for _, n := range nodes {
		w.inspect(n)
	}
	w.loops--
}

// declared collects the "key: value" lines of a doc comment.
func declared(doc *ast.CommentGroup) map[string][]string {
	if doc == nil {
		return nil
	}
	return facts.Declarations(strings.Split(doc.Text(), "\n"))
}

func (w *walker) line(n ast.Node) int { return w.fset.Position(n.Pos()).Line }

// followHelper walks a helper's body at each call, as if it were part of the test.
func (w *walker) followHelper(name string, c *ast.CallExpr) {
	h, ok := w.helpers[name]
	if !ok || w.walking[name] {
		return
	}
	w.walking[name] = true
	w.collectTParams(h.decl.Type, h.decl.Body)
	prevPath, prevArgs := w.curPath, w.args
	w.curPath, w.args = h.path, w.bindArgs(h.decl.Type, c)
	ast.Inspect(h.decl.Body, w.visit)
	w.curPath, w.args = prevPath, prevArgs
	delete(w.walking, name)
}

// bindArgs maps the helper's parameters to the caller's arguments, as the caller wrote them.
func (w *walker) bindArgs(ft *ast.FuncType, c *ast.CallExpr) map[string]string {
	out := map[string]string{}
	i := 0
	for _, f := range ft.Params.List {
		for _, n := range f.Names {
			if i < len(c.Args) {
				out[n.Name] = w.argText(c.Args[i])
			}
			i++
		}
	}
	return out
}

// argText is an argument's source text, or the caller's when it is a helper's parameter.
func (w *walker) argText(e ast.Expr) string {
	text := types.ExprString(e)
	if outer, ok := w.args[text]; ok {
		return outer
	}
	return text
}

// hazard records a hazard once, however many times the helper holding it is walked.
func (w *walker) hazard(c facts.HazardCategory, n ast.Expr) {
	hz := facts.Hazard{Category: c, Path: w.curPath, Line: w.line(n), Evidence: types.ExprString(n)}
	for _, h := range w.test.Hazards {
		if h == hz {
			return
		}
	}
	w.test.Hazards = append(w.test.Hazards, hz)
}
