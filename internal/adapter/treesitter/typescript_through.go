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
// Repo<User>, ns.Store), or "": an array of the type, or one of its members' types, is not the type.
func (ty *tsTypes) resolve(written string) string {
	written = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(written), ":"))
	if ty.repo[written] { // the class of a value made in another file (made)
		return written
	}
	for _, part := range strings.Split(written, "|") {
		part = strings.TrimSpace(part)
		if part == "" || part == "null" || part == "undefined" {
			continue
		}
		if strings.HasSuffix(part, "]") { // Repo[], Repo<T>[], Repo["key"]: not a Repo
			return ""
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

// made records, by the value's ID, the class of each value at the top of the file whose written
// type is one of the repository's classes or that is made from one (export const api = new Api()),
// for the files importing it.
func (ty *tsTypes) made(into map[string]string) {
	tr, f := ty.tr, ty.tr.f
	for name, d := range tr.top {
		if d.Kind() != "variable_declarator" {
			continue
		}
		written := f.field(d, "type")
		if written == "" {
			written = tsMade.made(f, d.ChildByFieldName("value"))
		}
		if class := ty.resolve(written); class != "" {
			into[tr.r.symbolID(f.path, name)] = class
		}
	}
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
// the function's parameters and locals (store.save()), and the values at the top of the file
// (tsModuleValues). It also counts the calls on such values whose type is not written, or any.
func (ty *tsTypes) through(decl, class *node, module map[string]string) (callLines, int) {
	f := ty.tr.f
	fields, vars, locals := tsFields(f, class), map[string]string{}, map[string]bool{}
	walk(decl, func(x *node) bool {
		switch x.Kind() {
		case "required_parameter", "optional_parameter":
			if x.ChildByFieldName("type") == nil {
				vars[f.field(x, "pattern")] = unknownType
			}
			tsTyped(f, x, vars)
		case "variable_declarator":
			locals[f.field(x, "name")] = true
			tsTyped(f, x, vars)
		}
		return true
	})
	out, unfollowed := callLines{}, 0
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
		case obj.Kind() == "identifier" && vars[f.text(obj)] != "":
			written = vars[f.text(obj)]
		case obj.Kind() == "identifier" && !locals[f.text(obj)]:
			written = module[f.text(obj)]
		case obj.Kind() == "member_expression" && f.field(obj, "object") == "this":
			written = fields[f.field(obj, "property")]
		}
		if written == unknownType {
			unfollowed++
		} else if id := ty.resolve(written); id != "" && method != "" {
			out.add(id+"."+method, line(x))
		}
		return true
	})
	for id, at := range madeInPlace(f, decl.ChildByFieldName("body"), tsMade, ty.resolve) {
		out.add(id, at)
	}
	return out, unfollowed
}

// throughs maps the IDs of the file's functions and methods to their calls through a value's
// declared type, and to the number of calls on values whose type is unknown, as bodies lists them.
func (ty *tsTypes) throughs(made map[string]string) (map[string]callLines, map[string]int) {
	f, r := ty.tr.f, ty.tr.r
	module := ty.moduleValues(made)
	out, unfollowed := map[string]callLines{}, map[string]int{}
	add := func(id string, decl, class *node) { out[id], unfollowed[id] = ty.through(decl, class, module) }
	for name, d := range ty.tr.top {
		switch d.Kind() {
		case "function_declaration", "generator_function_declaration":
			add(r.symbolID(f.path, name), d, nil)
		case "class_declaration", "abstract_class_declaration":
			for _, m := range named(d.ChildByFieldName("body")) {
				if m.Kind() == "method_definition" {
					add(r.symbolID(f.path, name)+"."+f.field(m, "name"), m, d)
				}
			}
		case "variable_declarator":
			if v := d.ChildByFieldName("value"); v != nil && (v.Kind() == "arrow_function" || v.Kind() == "function_expression") {
				add(r.symbolID(f.path, name), v, nil)
			}
		}
	}
	return out, unfollowed
}

// moduleValues returns, by name, the written types of the values at the top of the file that come
// from the repository's code: one of its classes made there (const repo = new Repo()) or in the
// file it is imported from (made), or
// unknownType for the result of one of its functions (const repo = makeRepo()), for one typed any,
// and for a value imported from one of its files that is neither a function nor a class.
func (ty *tsTypes) moduleValues(made map[string]string) map[string]string {
	tr, f := ty.tr, ty.tr.f
	out := map[string]string{}
	for name, b := range tr.bindings {
		if id := tr.r.target(b.file, b.name); b.file != "" && b.name != "*" && !tr.funcs[id] && !ty.repo[id] {
			out[name] = unknownType
			if class := made[id]; class != "" {
				out[name] = class
			}
		}
	}
	for name, d := range tr.top {
		if d.Kind() != "variable_declarator" {
			continue
		}
		v := d.ChildByFieldName("value")
		switch {
		case d.ChildByFieldName("type") != nil:
			tsTyped(f, d, out)
		case v == nil:
		case ty.resolve(tsMade.made(f, v)) != "":
			out[name] = tsMade.made(f, v)
		case v.Kind() == "call_expression" && ty.repoCallee(v.ChildByFieldName("function")):
			out[name] = unknownType
		}
	}
	return out
}

// repoCallee reports whether a callee names one of the repository's functions: one the file
// declares or imports, or a function of a module it imports whole (repo.make()).
func (ty *tsTypes) repoCallee(callee *node) bool {
	tr, f := ty.tr, ty.tr.f
	switch {
	case callee == nil:
		return false
	case callee.Kind() == "identifier":
		if b, ok := tr.bindings[f.text(callee)]; ok {
			return b.file != "" && tr.funcs[tr.r.target(b.file, b.name)]
		}
		return tr.funcs[tr.r.symbolID(f.path, f.text(callee))]
	case callee.Kind() == "member_expression":
		b, ok := tr.bindings[f.field(callee, "object")]
		return ok && b.file != "" && b.name == "*" && tr.funcs[tr.r.target(b.file, f.field(callee, "property"))]
	}
	return false
}

// tsTyped records the written type of a declaration that has one, by its name: unknownType for any.
func tsTyped(f *file, n *node, into map[string]string) {
	t := n.ChildByFieldName("type")
	if t == nil {
		return
	}
	name := n.ChildByFieldName("name")
	if name == nil {
		name = n.ChildByFieldName("pattern")
	}
	if name == nil || name.Kind() == "object_pattern" || name.Kind() == "array_pattern" {
		return
	}
	into[f.text(name)] = f.text(t)
	if strings.TrimSpace(strings.TrimPrefix(f.text(t), ":")) == "any" {
		into[f.text(name)] = unknownType
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
				if tsParameterProperty(p) && p.ChildByFieldName("type") == nil {
					fields[f.field(p, "pattern")] = unknownType // constructor(private repo)
				} else if tsParameterProperty(p) {
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

// bodies maps the IDs of the file's functions and methods to their bodies, as symbols lists them.
func (tr *tsTrace) bodies() map[string]*node {
	f, r := tr.f, tr.r
	out := map[string]*node{}
	for name, d := range tr.top {
		switch d.Kind() {
		case "function_declaration", "generator_function_declaration":
			out[r.symbolID(f.path, name)] = d.ChildByFieldName("body")
		case "class_declaration", "abstract_class_declaration":
			for _, m := range named(d.ChildByFieldName("body")) {
				if m.Kind() == "method_definition" {
					out[r.symbolID(f.path, name)+"."+f.field(m, "name")] = m.ChildByFieldName("body")
				}
			}
		case "variable_declarator":
			if v := d.ChildByFieldName("value"); v != nil && (v.Kind() == "arrow_function" || v.Kind() == "function_expression") {
				out[r.symbolID(f.path, name)] = v.ChildByFieldName("body")
			}
		}
	}
	return out
}
