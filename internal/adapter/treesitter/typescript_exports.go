//go:build cgo

package treesitter

import (
	"path"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

// How the TypeScript reader names the symbol an imported name stands for: the file that declares
// it, found through re-exports, and an ID that tells apart two files of one directory declaring the
// same name.

// tsModule is what one file exports.
type tsModule struct {
	declared map[string]bool      // names declared at the top of the file
	from     map[string][2]string // exported name -> the specifier and the name it passes on
	stars    []string             // specifiers of export * from
	def      string               // the name of the default export's declaration, "" when unnamed
}

// module reads what a file of the version exports; nil for a path that is not one.
func (r *typescript) module(p string) *tsModule {
	if m, ok := r.modules[p]; ok {
		return m
	}
	f := r.files[p]
	if f == nil {
		return nil
	}
	m := &tsModule{declared: map[string]bool{}, from: map[string][2]string{}}
	for _, top := range named(f.root) {
		for _, name := range tsDeclared(f, top) {
			m.declared[name] = true
		}
		if top.Kind() != "export_statement" {
			continue
		}
		src := unquote(f.field(top, "source"))
		if d := top.ChildByFieldName("declaration"); d != nil && strings.HasPrefix(f.text(top), "export default") {
			m.def = f.field(d, "name")
		} else if v := top.ChildByFieldName("value"); v != nil && v.Kind() == "identifier" {
			m.def = f.text(v)
		}
		if src == "" {
			continue
		}
		clause := false
		walk(top, func(x *sitter.Node) bool {
			if x.Kind() == "export_specifier" {
				clause = true
				name, alias := f.field(x, "name"), f.field(x, "alias")
				if alias == "" {
					alias = name
				}
				m.from[alias] = [2]string{src, name}
			}
			return true
		})
		if !clause {
			m.stars = append(m.stars, src)
		}
	}
	r.modules[p] = m
	return m
}

// tsDeclared lists the names a top-level statement declares as symbols.
func tsDeclared(f *file, top *sitter.Node) []string {
	n := top
	if top.Kind() == "export_statement" {
		if d := top.ChildByFieldName("declaration"); d != nil {
			n = d
		}
	}
	switch n.Kind() {
	case "function_declaration", "generator_function_declaration", "class_declaration", "abstract_class_declaration",
		"interface_declaration", "type_alias_declaration", "enum_declaration":
		return []string{f.field(n, "name")}
	case "lexical_declaration", "variable_declaration":
		var out []string
		for _, d := range named(n) {
			if v := d.ChildByFieldName("value"); d.Kind() == "variable_declarator" && v != nil &&
				(v.Kind() == "arrow_function" || v.Kind() == "function_expression") {
				out = append(out, f.field(d, "name"))
			}
		}
		return out
	}
	return nil
}

// declaring follows re-exports from a file to the file that declares an exported name, and the
// name it has there. It returns its arguments when it finds nothing better.
func (r *typescript) declaring(p, name string) (string, string) {
	return r.declaringIn(p, name, map[string]bool{})
}

func (r *typescript) declaringIn(p, name string, seen map[string]bool) (string, string) {
	m := r.module(p)
	if m == nil || seen[p+"#"+name] {
		return p, name
	}
	seen[p+"#"+name] = true
	if name == "default" && m.def != "" {
		name = m.def
	}
	if m.declared[name] {
		return p, name
	}
	if fw, ok := m.from[name]; ok {
		if to, kind := r.project.resolve(p, fw[0]); kind == tsFound && r.files[to] != nil {
			return r.declaringIn(to, fw[1], seen)
		}
	}
	for _, spec := range m.stars {
		if to, kind := r.project.resolve(p, spec); kind == tsFound && r.files[to] != nil {
			if q, n := r.declaringIn(to, name, seen); r.module(q) != nil && r.module(q).declared[n] {
				return q, n
			}
		}
	}
	return p, name
}

// target is the ID of the symbol a name exported by a file stands for.
func (r *typescript) target(p, name string) string {
	return r.symbolID(r.declaring(p, name))
}

// symbolID is the ID of a name declared at the top of a file: <directory>.<name>, or, when another
// file of the directory declares the same name, <file without extension>.<name>.
func (r *typescript) symbolID(p, name string) string {
	dir := path.Dir(p)
	if r.collide[dir+"."+name] {
		return strings.TrimSuffix(p, path.Ext(p)) + "." + name
	}
	return dir + "." + name
}

// collisions finds the names that two or more product files of one directory declare.
func (r *typescript) collisions(files []*file) {
	r.collide = map[string]bool{}
	seen := map[string]string{} // <dir>.<name> -> the first file declaring it
	for _, f := range files {
		if f.test {
			continue
		}
		for _, top := range named(f.root) {
			for _, name := range tsDeclared(f, top) {
				key := path.Dir(f.path) + "." + name
				if first, ok := seen[key]; ok && first != f.path {
					r.collide[key] = true
				} else if !ok {
					seen[key] = f.path
				}
			}
		}
	}
}
