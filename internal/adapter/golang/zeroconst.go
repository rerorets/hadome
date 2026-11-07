package golang

import (
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"io/fs"
	"path"
	"strings"
)

// zeroConsts reads, from syntax only, the declarations returning the zero value needs: the
// underlying type of a named result type, which gives its zero value, and constants whose value is
// already the zero value, for which returning it changes nothing, so no test could catch that
// mutant. It reads packages of the version's modules only: constants whose value is a literal,
// false, iota at the start of its block, or a conversion of one of these, and types declared once.
type zeroConsts struct {
	fsys    fs.FS
	modules map[string]string                   // directory -> module path
	files   map[string][]*ast.File              // directory|package -> its non-test files
	byDir   map[string]map[string]bool          // directory|package -> constant name -> its value is the zero value
	types   map[string]map[string]*ast.TypeSpec // directory|package -> type name -> its declaration, nil when declared twice
	declIn  map[*ast.TypeSpec]*ast.File         // a type's declaration -> the file that declares it
}

// zero reports whether e, returned in file f at path p, is a constant whose value is the zero value.
func (z *zeroConsts) zero(f *ast.File, p string, e ast.Expr) bool {
	switch e := e.(type) {
	case *ast.ParenExpr:
		return z.zero(f, p, e.X)
	case *ast.Ident:
		if e.Obj != nil && f.Scope.Lookup(e.Name) != e.Obj {
			return false // a variable, a parameter, or a constant declared in the function
		}
		return z.in(path.Dir(p), f.Name.Name)[e.Name]
	case *ast.SelectorExpr:
		dir := z.imported(f, e)
		return dir != "" && z.in(dir, "")[e.Sel.Name]
	}
	return false
}

// imported is the directory of the package a selector such as pkg.Name names, or "" when that is
// not a package of one of the version's modules.
func (z *zeroConsts) imported(f *ast.File, e *ast.SelectorExpr) string {
	pkg, ok := e.X.(*ast.Ident)
	if !ok || pkg.Obj != nil {
		return "" // a field or a method, not a package's declaration
	}
	dir, _ := z.dirOf(fileImports(f)[pkg.Name])
	return dir
}

// dirOf is the directory of a package of one of the version's modules.
func (z *zeroConsts) dirOf(importPath string) (string, bool) {
	dir, best := "", ""
	for d, m := range z.modules {
		if (importPath == m || strings.HasPrefix(importPath, m+"/")) && len(m) > len(best) {
			dir, best = path.Join(d, strings.TrimPrefix(importPath, m)), m
		}
	}
	return dir, best != ""
}

// filesIn parses the non-test files of a directory, of package pkg when it is given.
func (z *zeroConsts) filesIn(dir, pkg string) []*ast.File {
	key := dir + "|" + pkg
	if got, ok := z.files[key]; ok {
		return got
	}
	var out []*ast.File
	entries, _ := fs.ReadDir(z.fsys, dir)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		src, err := fs.ReadFile(z.fsys, path.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), e.Name(), src, parser.SkipObjectResolution)
		if err == nil && (pkg == "" || f.Name.Name == pkg) {
			out = append(out, f)
		}
	}
	if z.files == nil {
		z.files = map[string][]*ast.File{}
	}
	z.files[key] = out
	return out
}

// in reads the constants of a package. A name declared twice with values that disagree (files for
// different build tags) is not zero.
func (z *zeroConsts) in(dir, pkg string) map[string]bool {
	key := dir + "|" + pkg
	if got, ok := z.byDir[key]; ok {
		return got
	}
	out := map[string]bool{}
	seen := map[string]bool{}
	for _, f := range z.filesIn(dir, pkg) {
		addZeroConsts(f, out, seen)
	}
	if z.byDir == nil {
		z.byDir = map[string]map[string]bool{}
	}
	z.byDir[key] = out
	return out
}

// typesIn reads the types declared at the top of a package. A name declared twice (files for
// different build tags) maps to nil: which one builds is not known.
func (z *zeroConsts) typesIn(dir, pkg string) map[string]*ast.TypeSpec {
	key := dir + "|" + pkg
	if got, ok := z.types[key]; ok {
		return got
	}
	out := map[string]*ast.TypeSpec{}
	if z.declIn == nil {
		z.declIn = map[*ast.TypeSpec]*ast.File{}
	}
	for _, f := range z.filesIn(dir, pkg) {
		for _, d := range f.Decls {
			g, ok := d.(*ast.GenDecl)
			if !ok || g.Tok != token.TYPE {
				continue
			}
			for _, s := range g.Specs {
				ts := s.(*ast.TypeSpec)
				if _, twice := out[ts.Name.Name]; twice {
					out[ts.Name.Name] = nil
					continue
				}
				out[ts.Name.Name] = ts
				z.declIn[ts] = f
			}
		}
	}
	if z.types == nil {
		z.types = map[string]map[string]*ast.TypeSpec{}
	}
	z.types[key] = out
	return out
}

// result returns the zero value of the single result of fn, declared in file f at path p, or ""
// when it has several results or one whose zero value cannot be told from syntax.
func (z *zeroConsts) result(f *ast.File, p string, fn *ast.FuncDecl, written func(ast.Expr) string) string {
	t := fn.Type
	if t.Results == nil || len(t.Results.List) != 1 || len(t.Results.List[0].Names) > 1 {
		return ""
	}
	r := t.Results.List[0].Type
	if _, literal := r.(*ast.StructType); literal {
		return "" // a struct type written out has no short zero value
	}
	zero, structure := z.zeroType(f, path.Dir(p), r, typeParams(fn), 0)
	if structure {
		return written(r) + "{}"
	}
	return zero
}

// zeroType returns the zero value of a type written in file f of directory dir, or reports that its
// underlying type is a struct. hidden are the type parameters in scope, which hide package types.
func (z *zeroConsts) zeroType(f *ast.File, dir string, e ast.Expr, hidden map[string]bool, depth int) (zero string, structure bool) {
	if depth > 16 {
		return "", false // a cycle, which does not compile
	}
	switch e := e.(type) {
	case *ast.ParenExpr:
		return z.zeroType(f, dir, e.X, hidden, depth)
	case *ast.Ident:
		if hidden[e.Name] {
			return "", false
		}
		if ts, declared := z.typesIn(dir, f.Name.Name)[e.Name]; declared {
			return z.underlying(dir, ts, depth)
		}
		return builtinZero(e.Name), false
	case *ast.SelectorExpr:
		if to := z.imported(f, e); to != "" {
			if ts, declared := z.typesIn(to, "")[e.Sel.Name]; declared {
				return z.underlying(to, ts, depth)
			}
		}
	case *ast.StructType:
		return "", true
	case *ast.ArrayType:
		if e.Len == nil {
			return "nil", false
		}
	case *ast.StarExpr, *ast.MapType, *ast.ChanType, *ast.FuncType, *ast.InterfaceType:
		return "nil", false
	}
	return "", false
}

// underlying follows a named type to the zero value of its underlying type, read in the file that
// declares it. A type declared twice, or with type parameters, is not followed.
func (z *zeroConsts) underlying(dir string, ts *ast.TypeSpec, depth int) (string, bool) {
	if ts == nil || ts.TypeParams != nil {
		return "", false
	}
	return z.zeroType(z.declIn[ts], dir, ts.Type, nil, depth+1)
}

// typeParams are the names of a function's type parameters, and those its receiver declares.
func typeParams(fn *ast.FuncDecl) map[string]bool {
	out := map[string]bool{}
	if fn.Type.TypeParams != nil {
		for _, field := range fn.Type.TypeParams.List {
			for _, n := range field.Names {
				out[n.Name] = true
			}
		}
	}
	if fn.Recv != nil && len(fn.Recv.List) == 1 {
		r := fn.Recv.List[0].Type
		if s, ok := r.(*ast.StarExpr); ok {
			r = s.X
		}
		var indices []ast.Expr
		switch r := r.(type) {
		case *ast.IndexExpr:
			indices = []ast.Expr{r.Index}
		case *ast.IndexListExpr:
			indices = r.Indices
		}
		for _, i := range indices {
			if id, ok := i.(*ast.Ident); ok {
				out[id.Name] = true
			}
		}
	}
	return out
}

// builtinZero is the zero value of a predeclared type, or "" for a name that is not one.
func builtinZero(name string) string {
	switch name {
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
	return ""
}

// addZeroConsts records, for each package-level constant of f, whether its value is the zero value.
func addZeroConsts(f *ast.File, out, seen map[string]bool) {
	for _, d := range f.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.CONST {
			continue
		}
		var values []ast.Expr
		for i, s := range g.Specs {
			vs := s.(*ast.ValueSpec)
			if len(vs.Values) > 0 {
				values = vs.Values // a spec without values repeats the previous ones
			}
			for j, n := range vs.Names {
				isZero := j < len(values) && zeroValue(values[j], i)
				if seen[n.Name] && out[n.Name] != isZero {
					isZero = false
				}
				seen[n.Name], out[n.Name] = true, isZero
			}
		}
	}
}

// zeroValue reports whether a constant expression is the zero value, with iota at the given index.
func zeroValue(e ast.Expr, iota int) bool {
	switch e := e.(type) {
	case *ast.ParenExpr:
		return zeroValue(e.X, iota)
	case *ast.BasicLit:
		return zeroLiteral(e)
	case *ast.Ident:
		return e.Name == "false" || (e.Name == "iota" && iota == 0)
	case *ast.CallExpr:
		return conversion(e) && zeroValue(e.Args[0], iota)
	}
	return false
}

func zeroLiteral(l *ast.BasicLit) bool {
	v := constant.MakeFromLiteral(l.Value, l.Kind, 0)
	if v.Kind() == constant.String {
		return constant.StringVal(v) == ""
	}
	return constant.Sign(v) == 0 // a number or a rune
}

// conversion reports a call that may be a conversion such as Code(0) or pkg.Code(0). A builtin
// such as len or real gives zero for a zero argument too; unsafe.Sizeof and the like do not.
func conversion(c *ast.CallExpr) bool {
	if len(c.Args) != 1 {
		return false
	}
	if s, ok := c.Fun.(*ast.SelectorExpr); ok {
		if x, ok := s.X.(*ast.Ident); ok && x.Name == "unsafe" {
			return false
		}
	}
	return true
}
