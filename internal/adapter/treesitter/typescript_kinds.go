//go:build cgo

package treesitter

import "strings"

// What a TypeScript type or value is (vkind), as a file of the version writes it: the types and
// values it declares at its top or imports from another file of the version.

var (
	// tsBuiltinTypes are the global types of the language and its runtimes.
	tsBuiltinTypes = set("Array", "ReadonlyArray", "Map", "ReadonlyMap", "Set", "ReadonlySet", "WeakMap", "WeakSet",
		"Record", "Partial", "Required", "Readonly", "Pick", "Omit", "Promise", "PromiseLike", "Date", "RegExp", "Error",
		"URL", "URLSearchParams", "Iterable", "Iterator", "AsyncIterable", "Uint8Array", "Int8Array", "Uint16Array",
		"Int16Array", "Uint32Array", "Int32Array", "Float32Array", "Float64Array", "BigInt64Array", "BigUint64Array",
		"ArrayBuffer", "Buffer", "String", "Number", "Boolean", "Function", "Symbol", "JSON", "Headers", "Request",
		"Response", "FormData", "Blob", "AbortController", "AbortSignal", "TextEncoder", "TextDecoder")
	// tsElementTypes are the built-in generic types whose one type argument is what an index, a loop
	// or destructuring takes out.
	tsElementTypes = set("Array", "ReadonlyArray", "Set", "ReadonlySet", "Iterable", "AsyncIterable")
	// tsNoneTypes are the members of a union that hold no value to call a method on.
	tsNoneTypes = set("null", "undefined", "void", "never")
)

// tsTypeKind is the kind of a value of the written type t, a type annotation or a type in it.
// name gives the kind a type name stands for.
func tsTypeKind(f *file, t *node, name func(string) vkind) vkind {
	if t == nil {
		return vkind{}
	}
	switch t.Kind() {
	case "type_annotation", "parenthesized_type", "readonly_type", "opting_type_annotation":
		return tsTypeKind(f, t.NamedChild(0), name)
	case "predefined_type":
		if s := f.text(t); s == "any" || s == "unknown" || s == "object" {
			return vkind{}
		}
		return outsideKind
	case "literal_type", "template_literal_type", "function_type", "constructor_type", "type_predicate":
		return outsideKind
	case "type_identifier", "nested_type_identifier":
		return name(f.text(t))
	case "array_type":
		return containerOf(tsTypeKind(f, t.NamedChild(0), name))
	case "generic_type":
		return tsGenericKind(f, t, name)
	case "tuple_type":
		return tsOutsideHolding(f, named(t), name)
	case "union_type":
		return tsUnionKind(f, t, name)
	}
	return vkind{}
}

// tsGenericKind is the kind of a generic type: an element type's elements are its argument's kind,
// another built-in or outside type holds what its arguments hold, and the repository's generic type
// is that type.
func tsGenericKind(f *file, t *node, name func(string) vkind) vkind {
	base := f.field(t, "name")
	args := named(t.ChildByFieldName("type_arguments"))
	if tsElementTypes[base] && len(args) == 1 {
		return containerOf(tsTypeKind(f, args[0], name))
	}
	if k := name(base); !k.outside {
		return k
	}
	return tsOutsideHolding(f, args, name)
}

// tsOutsideHolding is an outside value made of values of the types ts: its elements are outside
// only when every one of them is pure.
func tsOutsideHolding(f *file, ts []*node, name func(string) vkind) vkind {
	k := outsideKind
	for _, t := range ts {
		if t.Kind() == "rest_type" || t.Kind() == "optional_type" {
			t = t.NamedChild(0)
		}
		if !tsTypeKind(f, t, name).pure() {
			k.holds = true
		}
	}
	return k
}

// tsUnionKind is the kind of a union: that of its one member that holds a value (Store | null), else
// outside when every member is pure, else unknown.
func tsUnionKind(f *file, t *node, name func(string) vkind) vkind {
	var members []*node
	var flat func(*node)
	flat = func(n *node) {
		if n.Kind() == "union_type" {
			for _, c := range named(n) {
				flat(c)
			}
		} else if !tsNoneTypes[strings.TrimSpace(f.text(n))] {
			members = append(members, n)
		}
	}
	flat(t)
	if len(members) == 1 {
		return tsTypeKind(f, members[0], name)
	}
	for _, m := range members {
		if !tsTypeKind(f, m, name).pure() {
			return vkind{}
		}
	}
	return outsideKind
}

// tsKinds is what the names of the version's files stand for, read when first needed.
type tsKinds struct {
	r       *typescript
	imports map[string]map[string][2]string // path -> local name -> the file and the name it imports; "" for outside
	types   map[string]vkind                // "<path>#<name>" -> what the type name declared there stands for
	values  map[string]vkind                // the same for values
	busy    map[string]bool                 // the names being read, so that a cycle reads as unknown
}

func newTSKinds(r *typescript) *tsKinds {
	return &tsKinds{r: r, imports: map[string]map[string][2]string{}, types: map[string]vkind{},
		values: map[string]vkind{}, busy: map[string]bool{}}
}

// importsOf returns what the imports of the file p bind: a local name -> the file of the version
// and the name there, or an empty file for a name from outside the repository.
func (ks *tsKinds) importsOf(p string) map[string][2]string {
	if m, ok := ks.imports[p]; ok {
		return m
	}
	m := map[string][2]string{}
	ks.imports[p] = m
	f := ks.r.files[p]
	if f == nil {
		return m
	}
	for _, top := range named(f.root) {
		if top.Kind() != "import_statement" {
			continue
		}
		to, kind := ks.r.project.resolve(p, unquote(f.field(top, "source")))
		if kind != tsFound && kind != tsExternal {
			continue
		}
		if kind == tsExternal {
			to = ""
		}
		walk(top, func(x *node) bool {
			switch x.Kind() {
			case "import_specifier":
				local := f.field(x, "alias")
				if local == "" {
					local = f.field(x, "name")
				}
				m[strings.TrimSpace(local)] = [2]string{to, f.field(x, "name")}
			case "namespace_import":
				for _, id := range named(x) {
					m[f.text(id)] = [2]string{to, "*"}
				}
			case "import_clause":
				if first := x.NamedChild(0); first != nil && first.Kind() == "identifier" {
					m[f.text(first)] = [2]string{to, "default"}
				}
			}
			return true
		})
	}
	return m
}

// typeName is what a type name written in the file p stands for: a type it declares or imports,
// one of a module it imports whole (ns.Store), or a built-in type. Anything else, such as a type
// parameter, is unknown.
func (ks *tsKinds) typeName(p, name string) vkind {
	imports := ks.importsOf(p)
	if ns, member, dotted := strings.Cut(name, "."); dotted {
		if from, ok := imports[ns]; ok && from[1] == "*" {
			if from[0] == "" {
				return outsideKind
			}
			return ks.declaredType(from[0], member)
		}
		return vkind{}
	}
	if from, ok := imports[name]; ok {
		if from[0] == "" {
			return outsideKind
		}
		return ks.declaredType(from[0], from[1])
	}
	if ks.top(p, name) != nil {
		return ks.declaredType(p, name)
	}
	if tsBuiltinTypes[name] {
		return outsideKind
	}
	return vkind{}
}

// declaredType is what the type name exported by the file p stands for where it is declared: the
// repository's class or interface, the type an alias names, or outside for an enum.
func (ks *tsKinds) declaredType(p, name string) vkind {
	p, name = ks.r.declaring(p, name)
	key := p + "#" + name
	if k, ok := ks.types[key]; ok {
		return k
	}
	if ks.busy["type "+key] {
		return vkind{}
	}
	ks.busy["type "+key] = true
	defer delete(ks.busy, "type "+key)
	var k vkind
	if d := ks.top(p, name); d != nil {
		switch d.Kind() {
		case "class_declaration", "abstract_class_declaration", "interface_declaration":
			k = repoKind(ks.r.symbolID(p, name))
		case "type_alias_declaration":
			k = tsTypeKind(ks.r.files[p], d.ChildByFieldName("value"), func(n string) vkind { return ks.typeName(p, n) })
		case "enum_declaration":
			k = outsideKind
		}
	}
	ks.types[key] = k
	return k
}

// importedValue is what a value imported into the file p as local stands for where it is declared:
// a variable of a file of the version. Anything else, such as a function, a class or a module, is
// unknown.
func (ks *tsKinds) importedValue(p, local string) vkind {
	from, ok := ks.importsOf(p)[local]
	if !ok || from[0] == "" || from[1] == "*" {
		return vkind{}
	}
	q, name := ks.r.declaring(from[0], from[1])
	if d := ks.top(q, name); d == nil || d.Kind() != "variable_declarator" || tsFunctionValue(d) {
		return vkind{}
	}
	return ks.declaredValue(q, name)
}

// declaredValue is what a variable declared at the top of the file p stands for: its written type,
// else its value.
func (ks *tsKinds) declaredValue(p, name string) vkind {
	key := p + "#" + name
	if k, ok := ks.values[key]; ok {
		return k
	}
	if ks.busy["value "+key] {
		return vkind{}
	}
	ks.busy["value "+key] = true
	defer delete(ks.busy, "value "+key)
	var k vkind
	if d := ks.top(p, name); d != nil && d.Kind() == "variable_declarator" {
		f := ks.r.files[p]
		k = tsTypeKind(f, d.ChildByFieldName("type"), func(n string) vkind { return ks.typeName(p, n) }).
			or(func() vkind { return ks.staticValue(p, d.ChildByFieldName("value")) })
	}
	ks.values[key] = k
	return k
}

// staticValue is what a value at the top of the file p is, read without running anything: a
// literal, a built-in or external object made, or another such variable. A literal that names
// anything of the repository holds it.
func (ks *tsKinds) staticValue(p string, v *node) vkind {
	f := ks.r.files[p]
	if v != nil && v.Kind() == "as_expression" {
		if t := v.NamedChild(1); t != nil && f.text(t) != "const" {
			if k := tsTypeKind(f, t, func(n string) vkind { return ks.typeName(p, n) }); k.known() {
				return k
			}
		}
	}
	v = tsUnwrap(v)
	switch {
	case v == nil:
	case tsPlainKinds[v.Kind()]:
		out := outsideKind
		walk(v, func(x *node) bool {
			if x.Kind() == "identifier" && !ks.staticValue(p, x).pure() {
				out.holds = true
			}
			return !out.holds
		})
		return out
	case v.Kind() == "identifier":
		return ks.staticName(p, f.text(v))
	case v.Kind() == "new_expression":
		ctor := f.field(v, "constructor")
		if tsBuiltinClasses[ctor] && ks.top(p, ctor) == nil {
			return outsideKind
		}
		if c := ks.typeName(p, ctor); c.known() {
			return c
		}
	}
	return vkind{}
}

// staticName is what a name used as a value at the top of the file p holds: an external import, a
// variable imported from the version, or one the file declares.
func (ks *tsKinds) staticName(p, name string) vkind {
	if from, ok := ks.importsOf(p)[name]; ok {
		if from[0] == "" {
			return outsideKind
		}
		return ks.importedValue(p, name)
	}
	if d := ks.top(p, name); d != nil && d.Kind() == "variable_declarator" {
		return ks.declaredValue(p, name)
	}
	return vkind{}
}

// top is the declaration of a name at the top of the file p: a function, class, interface, type
// alias or enum declaration, or a variable's declarator. nil for none.
func (ks *tsKinds) top(p, name string) *node {
	f := ks.r.files[p]
	if f == nil {
		return nil
	}
	for _, top := range named(f.root) {
		n := top
		if top.Kind() == "export_statement" {
			if d := top.ChildByFieldName("declaration"); d != nil {
				n = d
			}
		}
		switch n.Kind() {
		case "lexical_declaration", "variable_declaration":
			for _, d := range named(n) {
				if d.Kind() == "variable_declarator" && f.field(d, "name") == name {
					return d
				}
			}
		default:
			if f.field(n, "name") == name && strings.HasSuffix(n.Kind(), "_declaration") {
				return n
			}
		}
	}
	return nil
}

// tsFunctionValue reports whether a declarator holds a function.
func tsFunctionValue(d *node) bool {
	v := d.ChildByFieldName("value")
	return v != nil && (v.Kind() == "arrow_function" || v.Kind() == "function_expression")
}
