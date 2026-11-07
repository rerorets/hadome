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

// zeroConsts tells whether a returned expression names a constant whose value is already the zero
// value. Returning the zero value in its place changes nothing, so no test could catch that mutant.
// It reads syntax only: constants of the same package, or of a package of the same module, whose
// value is a literal, false, iota at the start of its block, or a conversion of one of these.
type zeroConsts struct {
	fsys    fs.FS
	modules map[string]string          // directory -> module path
	byDir   map[string]map[string]bool // directory -> constant name -> its value is the zero value
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
		pkg, ok := e.X.(*ast.Ident)
		if !ok || pkg.Obj != nil {
			return false // a field or a method, not a package's constant
		}
		imported, ok := fileImports(f)[pkg.Name]
		if !ok {
			return false
		}
		dir, ok := z.dirOf(imported)
		return ok && z.in(dir, "")[e.Sel.Name]
	}
	return false
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

// in reads the constants of the non-test files of a directory, of package pkg when it is given. A
// name declared twice with values that disagree (files for different build tags) is not zero.
func (z *zeroConsts) in(dir, pkg string) map[string]bool {
	key := dir + "|" + pkg
	if got, ok := z.byDir[key]; ok {
		return got
	}
	out := map[string]bool{}
	seen := map[string]bool{}
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
			addZeroConsts(f, out, seen)
		}
	}
	if z.byDir == nil {
		z.byDir = map[string]map[string]bool{}
	}
	z.byDir[key] = out
	return out
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
