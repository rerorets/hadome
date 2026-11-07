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
	modules map[string]string          // directory -> module path; read when first needed
	byDir   map[string]map[string]bool // directory -> constant name -> its value is the zero value
}

// zero reports whether e, returned in file f at path p, is a constant whose value is the zero value.
func (z *zeroConsts) zero(f *ast.File, p string, e ast.Expr) bool {
	switch e := e.(type) {
	case *ast.ParenExpr:
		return z.zero(f, p, e.X)
	case *ast.Ident:
		if e.Obj != nil && (e.Obj.Kind != ast.Con || f.Scope.Lookup(e.Name) != e.Obj) {
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
	if z.modules == nil {
		mods, err := findModules(z.fsys)
		if err != nil {
			return "", false
		}
		z.modules = mods
	}
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
		if err != nil || (pkg != "" && f.Name.Name != pkg) {
			continue
		}
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
	if z.byDir == nil {
		z.byDir = map[string]map[string]bool{}
	}
	z.byDir[key] = out
	return out
}

// zeroValue reports whether a constant expression is the zero value, with iota at the given index.
func zeroValue(e ast.Expr, iota int) bool {
	switch e := e.(type) {
	case *ast.ParenExpr:
		return zeroValue(e.X, iota)
	case *ast.BasicLit:
		v := constant.MakeFromLiteral(e.Value, e.Kind, 0)
		switch v.Kind() {
		case constant.String:
			return constant.StringVal(v) == ""
		case constant.Int, constant.Float, constant.Complex:
			return constant.Sign(v) == 0
		}
	case *ast.Ident:
		return e.Name == "false" || (e.Name == "iota" && iota == 0)
	case *ast.CallExpr:
		if len(e.Args) != 1 || e.Ellipsis != token.NoPos {
			return false
		}
		switch fn := e.Fun.(type) {
		case *ast.Ident: // a conversion such as Code(0); len, cap and the like are not
			if fn.Name == "len" || fn.Name == "cap" || fn.Name == "real" || fn.Name == "imag" {
				return false
			}
		case *ast.SelectorExpr: // pkg.Code(0); unsafe.Sizeof and the like are not
			if x, ok := fn.X.(*ast.Ident); !ok || x.Name == "unsafe" {
				return false
			}
		default:
			return false
		}
		return zeroValue(e.Args[0], iota)
	}
	return false
}
