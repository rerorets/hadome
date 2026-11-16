//go:build cgo

package treesitter

import (
	"strings"

	"github.com/rerorets/hadome/internal/facts"
)

// How the TypeScript reader finds the calls through a value's declared type (Symbol.Through) and
// the types a class or interface extends or implements (Symbol.Supertypes), for the call diagram
// (docs/design/rules-deps.md, "Call facts").

// tsTypes is what the type names of one file stand for: the repository's types it imports, type
// only or not, and the ones it declares.
type tsTypes struct {
	tr       *tsTrace
	imported map[string]tsImported
	repo     map[string]bool // the IDs of the version's types and interfaces
}

func newTSTypes(tr *tsTrace, t *tables, repo map[string]bool) *tsTypes {
	ty := &tsTypes{tr: tr, imported: map[string]tsImported{}, repo: repo}
	for _, top := range named(tr.f.root) {
		if top.Kind() == "import_statement" {
			tr.bindImportInto(top, t, ty.imported)
		}
	}
	return ty
}

// resolve returns the ID of the repository's type a written type names (Finder, Finder | null,
// Repo<User>, ns.Store), or "".
func (ty *tsTypes) resolve(written string) string {
	written = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(written), ":"))
	for _, part := range strings.Split(written, "|") {
		part = strings.TrimSpace(part)
		if part == "" || part == "null" || part == "undefined" {
			continue
		}
		name := part
		if i := strings.IndexAny(name, "<(["); i >= 0 {
			name = name[:i]
		}
		var id string
		if dot := strings.Index(name, "."); dot >= 0 { // ns.Store, with ns a namespace import
			if b, ok := ty.imported[name[:dot]]; ok && b.file != "" && b.name == "*" {
				id = ty.tr.r.target(b.file, name[dot+1:])
			}
		} else if b, ok := ty.imported[name]; ok && b.file != "" {
			id = ty.tr.r.target(b.file, b.name)
		} else {
			id = ty.tr.r.symbolID(ty.tr.f.path, name)
		}
		if ty.repo[id] {
			return id
		}
		return ""
	}
	return ""
}

// supertypes records what each class and interface the file declares at the top extends or
// implements.
func (ty *tsTypes) supertypes(sup supers) {
	f := ty.tr.f
	for _, top := range named(f.root) {
		decl := top
		if top.Kind() == "export_statement" {
			if d := top.ChildByFieldName("declaration"); d != nil {
				decl = d
			}
		}
		var written []string
		walk(decl, func(x *node) bool {
			switch x.Kind() {
			case "class_body", "interface_body", "object_type", "type_arguments":
				return false
			case "extends_clause", "implements_clause", "extends_type_clause":
				for _, t := range named(x) {
					if t.Kind() != "type_arguments" {
						written = append(written, f.text(t))
					}
				}
				return false
			}
			return true
		})
		if len(written) > 0 {
			sup.add(ty.tr.r.symbolID(f.path, f.field(decl, "name")), written, func(w string) tbind { return tbind{id: ty.resolve(w)} })
		}
	}
}

// through returns the methods a function's body calls on values whose written type is one of the
// repository's types: the class's fields and constructor parameter properties (this.store.save()),
// and the function's parameters and locals (store.save()).
func (ty *tsTypes) through(decl, class *node) []string {
	f := ty.tr.f
	fields, vars := tsFields(f, class), map[string]string{}
	walk(decl, func(x *node) bool {
		switch x.Kind() {
		case "required_parameter", "optional_parameter", "variable_declarator":
			tsTyped(f, x, vars)
		}
		return true
	})
	out := map[string]bool{}
	walk(decl.ChildByFieldName("body"), func(x *node) bool {
		if x.Kind() != "call_expression" {
			return true
		}
		callee := x.ChildByFieldName("function")
		if callee == nil || callee.Kind() != "member_expression" {
			return true
		}
		obj, method := callee.ChildByFieldName("object"), f.field(callee, "property")
		written := ""
		switch {
		case obj.Kind() == "identifier":
			written = vars[f.text(obj)]
		case obj.Kind() == "member_expression" && f.field(obj, "object") == "this":
			written = fields[f.field(obj, "property")]
		}
		if id := ty.resolve(written); id != "" && method != "" {
			out[id+"."+method] = true
		}
		return true
	})
	for _, id := range madeInPlace(f, decl.ChildByFieldName("body"), tsMade, ty.resolve) {
		out[id] = true
	}
	return keys(out)
}

// throughs maps the IDs of the file's functions and methods to their calls through a value's
// declared type, as bodies lists them.
func (ty *tsTypes) throughs() map[string][]string {
	f, r := ty.tr.f, ty.tr.r
	out := map[string][]string{}
	for name, d := range ty.tr.top {
		switch d.Kind() {
		case "function_declaration", "generator_function_declaration":
			out[r.symbolID(f.path, name)] = ty.through(d, nil)
		case "class_declaration", "abstract_class_declaration":
			for _, m := range named(d.ChildByFieldName("body")) {
				if m.Kind() == "method_definition" {
					out[r.symbolID(f.path, name)+"."+f.field(m, "name")] = ty.through(m, d)
				}
			}
		case "variable_declarator":
			if v := d.ChildByFieldName("value"); v != nil && (v.Kind() == "arrow_function" || v.Kind() == "function_expression") {
				out[r.symbolID(f.path, name)] = ty.through(v, nil)
			}
		}
	}
	return out
}

// tsTyped records the written type of a declaration that has one, by its name.
func tsTyped(f *file, n *node, into map[string]string) {
	t := n.ChildByFieldName("type")
	if t == nil {
		return
	}
	name := n.ChildByFieldName("name")
	if name == nil {
		name = n.ChildByFieldName("pattern")
	}
	if name != nil && name.Kind() != "object_pattern" && name.Kind() != "array_pattern" {
		into[f.text(name)] = f.text(t)
	}
}

// tsFields returns the written types of a class's fields and constructor parameter properties, by
// name. A field without one set to an object made in place (private repo = new Repo()) takes the
// class it makes.
func tsFields(f *file, class *node) map[string]string {
	fields := map[string]string{}
	if class == nil {
		return fields
	}
	for _, m := range named(class.ChildByFieldName("body")) {
		switch {
		case m.Kind() == "public_field_definition" && m.ChildByFieldName("type") == nil:
			fields[f.field(m, "name")] = tsMade.made(f, m.ChildByFieldName("value"))
		case m.Kind() == "public_field_definition":
			tsTyped(f, m, fields)
		case m.Kind() == "method_definition" && f.field(m, "name") == "constructor":
			for _, p := range named(m.ChildByFieldName("parameters")) {
				if tsParameterProperty(p) {
					tsTyped(f, p, fields)
				}
			}
		}
	}
	return fields
}

// tsParameterProperty reports whether a constructor parameter is also a property of the class:
// it has an accessibility modifier or readonly.
func tsParameterProperty(p *node) bool {
	for _, c := range named(p) {
		if c.Kind() == "accessibility_modifier" {
			return true
		}
	}
	return hasToken(p, "readonly")
}

// tsRepoTypes returns the IDs of the version's types and interfaces.
func tsRepoTypes(a *facts.Analysis) map[string]bool {
	out := map[string]bool{}
	for _, s := range a.Symbols {
		if s.Kind == facts.SymbolType || s.Kind == facts.SymbolInterface {
			out[s.ID] = true
		}
	}
	return out
}
