// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

package golang

import (
	"go/ast"
	"go/types"
	"sort"
	"strings"

	"github.com/rerorets/hadome/internal/facts"
)

// How the Go adapter decides Symbol.Supertypes (docs/design/rules-deps.md, "Reading each
// language"): Go writes no implements, so a type implements one of the repository's interfaces when
// its method set holds every method of the interface, compared by name and by the types of its
// parameters and results as written, with type names resolved to their packages.

// goType is what matching needs of one of the repository's named types.
type goType struct {
	iface   bool
	methods map[string]string // name -> signature, its own: declared in the interface or with a receiver
	embeds  []string          // the repository's types it embeds
	// unknown is true for an interface embedding one from another module, whose methods are not
	// read, so its full method set is unknown.
	unknown bool
}

// matching reads the declared types of product code and their methods.
type matching struct{ types map[string]*goType }

// supertypes fills Symbol.Supertypes of the repository's types: the interfaces each one implements,
// and for an interface, the interfaces it embeds. An interface with no methods is left out, since
// every type implements it.
func supertypes(files []parsedFile, a *facts.Analysis) {
	got := readTypes(files).match()
	for i := range a.Symbols {
		s := &a.Symbols[i]
		if s.Kind == facts.SymbolType || s.Kind == facts.SymbolInterface {
			s.Supertypes = got[s.ID]
			sort.Strings(s.Supertypes)
		}
	}
}

// readTypes reads the named types of product code, then their methods.
func readTypes(files []parsedFile) matching {
	m := matching{types: map[string]*goType{}}
	var product []parsedFile
	for _, pf := range files {
		if !pf.test && !pf.support {
			product = append(product, pf)
		}
	}
	for _, pf := range product {
		for _, ts := range typeSpecs(pf.file) {
			m.types[pf.pkgPath+"."+ts.Name.Name] = &goType{methods: map[string]string{}}
		}
	}
	for _, pf := range product {
		imports := fileImports(pf.file)
		for _, ts := range typeSpecs(pf.file) {
			m.read(m.types[pf.pkgPath+"."+ts.Name.Name], ts.Type, pf.pkgPath, imports)
		}
		for _, decl := range pf.file.Decls {
			d, ok := decl.(*ast.FuncDecl)
			if !ok || d.Recv == nil {
				continue
			}
			if t := m.types[pf.pkgPath+"."+receiverType(d.Recv)]; t != nil {
				t.methods[d.Name.Name] = methodSignature(d.Type, m.writer(pf.pkgPath, imports))
			}
		}
	}
	return m
}

// typeSpecs returns the type declarations at the top of a file.
func typeSpecs(f *ast.File) []*ast.TypeSpec {
	var out []*ast.TypeSpec
	for _, decl := range f.Decls {
		if d, ok := decl.(*ast.GenDecl); ok {
			for _, s := range d.Specs {
				if ts, ok := s.(*ast.TypeSpec); ok {
					out = append(out, ts)
				}
			}
		}
	}
	return out
}

// match returns, by type, the interfaces a concrete type implements and those an interface embeds.
func (m matching) match() map[string][]string {
	sets, ifaces := map[string]map[string]string{}, []string{}
	for id, t := range m.types {
		sets[id] = m.methodSet(id, map[string]bool{})
		if t.iface && !t.unknown && len(sets[id]) > 0 {
			ifaces = append(ifaces, id)
		}
	}
	got := map[string][]string{}
	for id, t := range m.types {
		if t.iface {
			got[id] = t.embeds
			continue
		}
		for _, iid := range ifaces {
			if implements(sets[id], sets[iid]) {
				got[id] = append(got[id], iid)
			}
		}
	}
	return got
}

// read records the methods an interface declares and the types a struct or an interface embeds.
func (m matching) read(t *goType, e ast.Expr, pkg string, imports map[string]string) {
	switch x := e.(type) {
	case *ast.InterfaceType:
		t.iface = true
		for _, f := range x.Methods.List {
			ft, isFunc := f.Type.(*ast.FuncType)
			switch {
			case isFunc:
				for _, n := range f.Names {
					t.methods[n.Name] = methodSignature(ft, m.writer(pkg, imports))
				}
			case m.types[m.canon(f.Type, pkg, imports)] != nil:
				t.embeds = append(t.embeds, m.canon(f.Type, pkg, imports))
			default: // another module's interface, or a constraint such as ~int
				t.unknown = true
			}
		}
	case *ast.StructType:
		for _, f := range x.Fields.List {
			if id := m.canon(derefType(f.Type), pkg, imports); len(f.Names) == 0 && m.types[id] != nil {
				t.embeds = append(t.embeds, id)
			}
		}
	}
}

// derefType strips the * of an embedded *T.
func derefType(e ast.Expr) ast.Expr {
	if s, ok := e.(*ast.StarExpr); ok {
		return s.X
	}
	return e
}

// methodSet returns a type's methods with those of the repository's types it embeds, at any depth.
// A method with a pointer receiver counts too: when only *T satisfies an interface, T is still
// taken as its implementation.
func (m matching) methodSet(id string, seen map[string]bool) map[string]string {
	t := m.types[id]
	if t == nil || seen[id] {
		return nil
	}
	seen[id] = true
	out := map[string]string{}
	for _, e := range t.embeds {
		for name, sig := range m.methodSet(e, seen) {
			out[name] = sig
		}
	}
	for name, sig := range t.methods { // a type's own method hides a promoted one
		out[name] = sig
	}
	return out
}

func implements(set, want map[string]string) bool {
	for name, sig := range want {
		if set[name] != sig {
			return false
		}
	}
	return true
}

// methodSignature writes the types of a function's parameters and results, without their names, each
// written by c.
func methodSignature(ft *ast.FuncType, c func(ast.Expr) string) string {
	list := func(fl *ast.FieldList) string {
		if fl == nil {
			return "()"
		}
		var parts []string
		for _, f := range fl.List {
			for range max(len(f.Names), 1) {
				parts = append(parts, c(f.Type))
			}
		}
		return "(" + strings.Join(parts, ",") + ")"
	}
	return list(ft.Params) + list(ft.Results)
}

// writer returns how types are written in a file of package pkg (see canon).
func (m matching) writer(pkg string, imports map[string]string) func(ast.Expr) string {
	return func(e ast.Expr) string { return m.canon(e, pkg, imports) }
}

// canon writes a type as written, each of the repository's named types with its package's path and
// each imported one with the import's path, so that the same type gives the same text in every
// file.
func (m matching) canon(e ast.Expr, pkg string, imports map[string]string) string {
	c := m.writer(pkg, imports)
	switch x := e.(type) {
	case *ast.Ident:
		if m.types[pkg+"."+x.Name] != nil {
			return pkg + "." + x.Name
		}
		return x.Name
	case *ast.SelectorExpr:
		if p, ok := x.X.(*ast.Ident); ok && imports[p.Name] != "" {
			return imports[p.Name] + "." + x.Sel.Name
		}
	case *ast.ParenExpr:
		return c(x.X)
	case *ast.StarExpr:
		return "*" + c(x.X)
	case *ast.Ellipsis:
		return "..." + c(x.Elt)
	case *ast.ArrayType, *ast.MapType, *ast.ChanType, *ast.FuncType, *ast.IndexExpr, *ast.IndexListExpr:
		return m.composite(x, c)
	}
	return types.ExprString(e)
}

// composite writes a type made of other types, each written by c: one of the kinds canon hands it.
func (m matching) composite(e ast.Expr, c func(ast.Expr) string) string {
	switch x := e.(type) {
	case *ast.ArrayType:
		if x.Len == nil {
			return "[]" + c(x.Elt)
		}
		return "[" + types.ExprString(x.Len) + "]" + c(x.Elt)
	case *ast.MapType:
		return "map[" + c(x.Key) + "]" + c(x.Value)
	case *ast.ChanType:
		return types.ExprString(&ast.ChanType{Dir: x.Dir, Value: ast.NewIdent("")}) + c(x.Value)
	case *ast.FuncType:
		return "func" + methodSignature(x, c)
	case *ast.IndexExpr:
		return c(x.X) + "[" + c(x.Index) + "]"
	}
	x := e.(*ast.IndexListExpr)
	parts := make([]string, len(x.Indices))
	for i, ix := range x.Indices {
		parts[i] = c(ix)
	}
	return c(x.X) + "[" + strings.Join(parts, ",") + "]"
}
