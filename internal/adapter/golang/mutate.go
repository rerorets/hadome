package golang

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"io/fs"
	"sort"
	"strings"

	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
)

// The kinds of break, as the design names them.
const (
	flipComparison = "flip a comparison"
	negateBoolean  = "negate a boolean"
	swapArithmetic = "swap arithmetic"
	returnZero     = "return the zero value"
	removeCall     = "remove a call statement"
)

var flipped = map[token.Token]token.Token{
	token.LSS: token.GEQ, token.LEQ: token.GTR, token.GTR: token.LEQ, token.GEQ: token.LSS,
	token.EQL: token.NEQ, token.NEQ: token.EQL,
}

var swapped = map[token.Token]token.Token{
	token.ADD: token.SUB, token.SUB: token.ADD, token.MUL: token.QUO, token.QUO: token.MUL,
}

// Mutants makes the mutants of the given functions of a version. It reads syntax only and runs
// nothing. The order is that of the files' paths, then of positions in each file.
func (Adapter) Mutants(v engine.Version, funcs []facts.Symbol) ([]facts.Mutant, error) {
	byPath := map[string]map[int]string{} // path -> declaration line -> symbol ID
	for _, s := range funcs {
		if strings.HasSuffix(s.Path, ".go") && (s.Kind == facts.SymbolFunction || s.Kind == facts.SymbolMethod) {
			if byPath[s.Path] == nil {
				byPath[s.Path] = map[int]string{}
			}
			byPath[s.Path][s.Line] = s.ID
		}
	}
	paths := make([]string, 0, len(byPath))
	for p := range byPath {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var out []facts.Mutant
	consts := &zeroConsts{fsys: v.FS}
	for _, p := range paths {
		src, err := fs.ReadFile(v.FS, p)
		if err != nil {
			return nil, err
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, p, src, 0)
		if err != nil {
			continue // a file that does not parse is unknown/unread's business
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			if id, wanted := byPath[p][fset.Position(fn.Pos()).Line]; wanted {
				m := mutator{fset: fset, src: src, path: p, file: f, consts: consts, fn: id, seen: map[string]int{}}
				m.function(fn)
				out = append(out, m.out...)
			}
		}
	}
	return out, nil
}

// mutator collects the mutants of one function.
type mutator struct {
	fset *token.FileSet
	src  []byte
	path string
	file *ast.File
	// consts finds constants whose value is the zero value, which returning the zero value leaves
	// unchanged.
	consts *zeroConsts
	fn     string
	seen   map[string]int // anchor without its index -> how many so far
	out    []facts.Mutant
}

func (m *mutator) function(fn *ast.FuncDecl) {
	zero := zeroOf(fn.Type)
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false // a closure is broken through the function that declares it, not here
		case *ast.BinaryExpr:
			m.binary(n)
		case *ast.UnaryExpr:
			if n.Op == token.NOT {
				m.add(negateBoolean, n.Pos(), n.X.Pos(), "")
			}
		case *ast.Ident:
			if n.Name == "true" || n.Name == "false" {
				m.add(negateBoolean, n.Pos(), n.End(), map[string]string{"true": "false", "false": "true"}[n.Name])
			}
		case *ast.ReturnStmt:
			m.returnZero(n, zero)
		case *ast.ExprStmt:
			if _, ok := n.X.(*ast.CallExpr); ok {
				m.add(removeCall, n.Pos(), n.End(), "_ = 0")
			}
		}
		return true
	})
}

// returnZero breaks a return of a single result by returning the zero value instead, unless the
// result already is that value: a literal, or a constant whose value it is.
func (m *mutator) returnZero(n *ast.ReturnStmt, zero string) {
	if zero == "" || len(n.Results) != 1 || boolLiteral(n.Results[0]) {
		return
	}
	r := n.Results[0]
	if m.text(r.Pos(), r.End()) == zero || (zero != "nil" && m.consts.zero(m.file, m.path, r)) {
		return
	}
	m.add(returnZero, r.Pos(), r.End(), zero)
}

func (m *mutator) binary(n *ast.BinaryExpr) {
	if to, ok := flipped[n.Op]; ok {
		m.add(flipComparison, n.OpPos, n.OpPos+token.Pos(len(n.Op.String())), to.String())
		return
	}
	if to, ok := swapped[n.Op]; ok && !stringy(n.X) && !stringy(n.Y) {
		m.add(swapArithmetic, n.OpPos, n.OpPos+token.Pos(len(n.Op.String())), to.String())
	}
}

// boolLiteral reports true or false, which negating a boolean already breaks.
func boolLiteral(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	return ok && (id.Name == "true" || id.Name == "false")
}

// stringy reports a string literal: + on strings joins them, and swapping it would not compile.
func stringy(e ast.Expr) bool {
	l, ok := e.(*ast.BasicLit)
	return ok && l.Kind == token.STRING
}

func (m *mutator) add(kind string, from, to token.Pos, replacement string) {
	start, end := m.fset.Position(from).Offset, m.fset.Position(to).Offset
	line := m.fset.Position(from).Line
	key := m.fn + "|" + kind + "|" + normalisedLine(m.src, start)
	m.seen[key]++
	m.out = append(m.out, facts.Mutant{
		Path: m.path, Line: line, Func: m.fn, Operator: kind,
		Original: string(m.src[start:end]), Replacement: replacement, Start: start, End: end,
		Anchor: fmt.Sprintf("%s|%d", key, m.seen[key]),
	})
}

func (m *mutator) text(from, to token.Pos) string {
	return string(m.src[m.fset.Position(from).Offset:m.fset.Position(to).Offset])
}

// normalisedLine is the tokens of the line holding offset, without comments or whitespace, so that
// reformatting the line or commenting it does not make its mutants new.
func normalisedLine(src []byte, offset int) string {
	start := strings.LastIndexByte(string(src[:offset]), '\n') + 1
	end := strings.IndexByte(string(src[offset:]), '\n')
	if end < 0 {
		end = len(src) - offset
	}
	line := src[start : offset+end]
	var s scanner.Scanner
	fset := token.NewFileSet()
	s.Init(fset.AddFile("", -1, len(line)), line, nil, 0)
	var toks []string
	for {
		_, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		if tok == token.SEMICOLON && lit == "\n" {
			continue // inserted at the end of the line
		}
		if lit == "" {
			lit = tok.String()
		}
		toks = append(toks, lit)
	}
	return strings.Join(toks, " ")
}

// zeroOf returns the zero value of a function's single result when it is a basic type or one whose
// zero value is nil; empty otherwise (several results, or a struct).
func zeroOf(t *ast.FuncType) string {
	if t.Results == nil || len(t.Results.List) != 1 || len(t.Results.List[0].Names) > 1 {
		return ""
	}
	switch r := t.Results.List[0].Type.(type) {
	case *ast.Ident:
		switch r.Name {
		case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
			"float32", "float64", "byte", "rune":
			return "0"
		case "string":
			return `""`
		case "bool":
			return "false"
		case "error", "any":
			return "nil"
		}
	case *ast.StarExpr, *ast.ArrayType, *ast.MapType, *ast.ChanType, *ast.FuncType, *ast.InterfaceType:
		if a, ok := r.(*ast.ArrayType); ok && a.Len != nil {
			return "" // a fixed-size array is not nil
		}
		return "nil"
	}
	return ""
}
