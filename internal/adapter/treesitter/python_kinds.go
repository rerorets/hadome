//go:build cgo

package treesitter

import "strings"

// What a Python type or value is (vkind), as a module of the version writes it: the classes,
// aliases and variables it declares at its top or imports from another module of the version.

var (
	// pyBuiltinTypes are the built-in types, which need no import.
	pyBuiltinTypes = set("str", "int", "float", "complex", "bool", "bytes", "bytearray", "list", "dict", "set",
		"frozenset", "tuple", "range", "slice", "memoryview")
	// pyElementTypes are the containers whose one type argument is what an index or a loop takes out.
	pyElementTypes = set("list", "set", "frozenset", "List", "Set", "FrozenSet", "AbstractSet", "Sequence",
		"MutableSequence", "Iterable", "Iterator", "Collection", "Generator", "Deque", "deque")
	// pyBuiltinFunctions are the built-in functions whose results are outside values; the containers
	// among them (pyCopies) hold what their argument holds.
	pyBuiltinFunctions = set("str", "int", "float", "bool", "bytes", "repr", "len", "abs", "round", "sum", "min",
		"max", "any", "all", "range", "enumerate", "zip", "map", "filter", "dict", "format", "hash", "ord", "chr",
		"isinstance", "hex", "bin", "oct", "divmod", "pow")
	pyCopies = set("list", "tuple", "set", "frozenset", "sorted", "reversed", "iter")
	// pyLiteralKinds are the expressions that build an outside value whatever they hold.
	pyLiteralKinds = set("list", "dictionary", "set", "tuple", "string", "concatenated_string", "integer", "float",
		"true", "false", "none", "list_comprehension", "dictionary_comprehension", "set_comprehension",
		"generator_expression", "comparison_operator", "boolean_operator",
		"not_operator", "lambda")
)

// pyTypeKind is the kind of a value of the written type t. name gives the kind a type name, dotted
// or not, stands for.
func pyTypeKind(f *file, t *node, name func(string) vkind) vkind {
	if t == nil {
		return vkind{}
	}
	switch t.Kind() {
	case "type", "parenthesized_expression":
		return pyTypeKind(f, t.NamedChild(0), name)
	case "none":
		return outsideKind
	case "identifier", "attribute":
		if f.text(t) == "Any" || f.text(t) == "typing.Any" || f.text(t) == "object" {
			return vkind{}
		}
		return name(f.text(t))
	case "string":
		return name(strings.Trim(f.text(t), `"'`)) // a forward reference: "Store"
	case "binary_operator":
		return pyUnionKind(f, []*node{t.ChildByFieldName("left"), t.ChildByFieldName("right")}, name)
	case "generic_type", "subscript":
		base, args := pyTypeArguments(f, t)
		return pyGenericKind(f, base, args, name)
	}
	return vkind{}
}

// pyTypeArguments are the base and the arguments of a generic type, written list[str] or, in an
// expression, typing.List[str].
func pyTypeArguments(f *file, t *node) (string, []*node) {
	var args []*node
	if t.Kind() == "generic_type" {
		for _, p := range named(t) {
			if p.Kind() == "type_parameter" {
				args = append(args, named(p)...)
			}
		}
		return f.text(t.NamedChild(0)), args
	}
	for i := uint(0); i < t.ChildCount(); i++ {
		if t.FieldNameForChild(uint32(i)) == "subscript" {
			args = append(args, t.Child(i))
		}
	}
	return f.field(t, "value"), args
}

// pyGenericKind is the kind of a generic type base[args]: an optional or union one is read as a
// union, an element type's elements are its argument's kind, another outside type holds what its
// arguments hold, and the repository's generic class is that class.
func pyGenericKind(f *file, base string, args []*node, name func(string) vkind) vkind {
	short := base[strings.LastIndex(base, ".")+1:]
	switch {
	case short == "Optional" && len(args) == 1:
		return pyTypeKind(f, args[0], name)
	case short == "Union":
		return pyUnionKind(f, args, name)
	case pyElementTypes[short] && len(args) == 1:
		return containerOf(pyTypeKind(f, args[0], name))
	case (short == "tuple" || short == "Tuple") && len(args) == 2 && f.text(args[1]) == "...":
		return containerOf(pyTypeKind(f, args[0], name))
	}
	if k := name(base); !k.outside {
		return k
	}
	k := outsideKind
	for _, a := range args {
		if f.text(a) != "..." && !pyTypeKind(f, a, name).pure() {
			k.holds = true
		}
	}
	return k
}

// pyUnionKind is the kind of a union: that of its one member other than None, else outside when
// every member is pure, else unknown.
func pyUnionKind(f *file, ts []*node, name func(string) vkind) vkind {
	var members []*node
	var flat func(*node)
	flat = func(n *node) {
		switch {
		case n == nil:
		case n.Kind() == "type":
			flat(n.NamedChild(0))
		case n.Kind() == "binary_operator":
			flat(n.ChildByFieldName("left"))
			flat(n.ChildByFieldName("right"))
		case n.Kind() != "none":
			members = append(members, n)
		}
	}
	for _, t := range ts {
		flat(t)
	}
	if len(members) == 1 {
		return pyTypeKind(f, members[0], name)
	}
	for _, m := range members {
		if !pyTypeKind(f, m, name).pure() {
			return vkind{}
		}
	}
	return outsideKind
}

// pyKinds is what the names of the version's modules stand for, read when first needed.
type pyKinds struct {
	r       *python
	imports map[string]map[string][2]string // path -> local name -> the module and the name there; "" for outside
	types   map[string]vkind                // "<module>#<name>" -> what the type name declared there stands for
	values  map[string]vkind                // the same for values
	busy    map[string]bool                 // the names being read, so that a cycle reads as unknown
}

func newPyKinds(r *python) *pyKinds {
	return &pyKinds{r: r, imports: map[string]map[string][2]string{}, types: map[string]vkind{},
		values: map[string]vkind{}, busy: map[string]bool{}}
}

// importsOf returns what the imports of the file p bind: a local name -> the module of the version
// and the name there ("" for the module itself), or an empty module for one from outside.
func (ks *pyKinds) importsOf(p string) map[string][2]string {
	if m, ok := ks.imports[p]; ok {
		return m
	}
	m := map[string][2]string{}
	ks.imports[p] = m
	if f := ks.r.files[p]; f != nil {
		for _, im := range ks.r.parseImports(f) {
			for local, name := range im.names {
				switch {
				case im.internal:
					m[local] = [2]string{im.mod, name}
				case !im.missing:
					m[local] = [2]string{"", name}
				}
			}
		}
	}
	return m
}

// file is the path of a module's file, or "".
func (ks *pyKinds) file(mod string) string {
	for _, p := range []string{mod + ".py", mod + "/__init__.py"} {
		if ks.r.files[p] != nil {
			return p
		}
	}
	return ""
}

// typeName is what a type name written in the file p stands for: a class or alias it declares or
// imports, one of a module it imports whole, or a built-in type.
func (ks *pyKinds) typeName(p, name string) vkind {
	imports := ks.importsOf(p)
	if mod, member, dotted := strings.Cut(name, "."); dotted {
		if from, ok := imports[mod]; ok && from[1] == "" {
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
		return ks.declaredType(module(p), name)
	}
	if pyBuiltinTypes[name] {
		return outsideKind
	}
	return vkind{}
}

// declaredType is what a type name of the module mod stands for where it is declared: one of its
// classes, or the type an alias assigns.
func (ks *pyKinds) declaredType(mod, name string) vkind {
	key := mod + "#" + name
	if k, ok := ks.types[key]; ok {
		return k
	}
	if ks.busy["type "+key] {
		return vkind{}
	}
	ks.busy["type "+key] = true
	defer delete(ks.busy, "type "+key)
	var k vkind
	p := ks.file(mod)
	if d := ks.top(p, name); d != nil {
		switch d.Kind() {
		case "class_definition":
			k = repoKind(mod + "." + name)
		case "assignment":
			k = pyTypeKind(ks.r.files[p], d.ChildByFieldName("right"), func(n string) vkind { return ks.typeName(p, n) })
		}
	}
	ks.types[key] = k
	return k
}

// importedValue is what a name imported into the file p as local stands for where it is declared:
// a variable of a module of the version. Anything else, such as a function, a class or a module, is
// unknown.
func (ks *pyKinds) importedValue(p, local string) vkind {
	from, ok := ks.importsOf(p)[local]
	if !ok || from[0] == "" || from[1] == "" {
		return vkind{}
	}
	if d := ks.top(ks.file(from[0]), from[1]); d == nil || d.Kind() != "assignment" {
		return vkind{}
	}
	return ks.declaredValue(from[0], from[1])
}

// declaredValue is what a variable at the top of the module mod stands for: its written type, else
// its value.
func (ks *pyKinds) declaredValue(mod, name string) vkind {
	key := mod + "#" + name
	if k, ok := ks.values[key]; ok {
		return k
	}
	if ks.busy["value "+key] {
		return vkind{}
	}
	ks.busy["value "+key] = true
	defer delete(ks.busy, "value "+key)
	var k vkind
	p := ks.file(mod)
	if d := ks.top(p, name); d != nil && d.Kind() == "assignment" {
		k = pyTypeKind(ks.r.files[p], d.ChildByFieldName("type"), func(n string) vkind { return ks.typeName(p, n) }).
			or(func() vkind { return ks.staticValue(p, d.ChildByFieldName("right")) })
	}
	ks.values[key] = k
	return k
}

// staticValue is what a value at the top of the file p is, read without running anything: a
// literal, a class of the repository made, or another such variable. A literal that names anything
// of the repository holds it.
func (ks *pyKinds) staticValue(p string, v *node) vkind {
	f := ks.r.files[p]
	if v == nil || f == nil {
		return vkind{}
	}
	switch k := v.Kind(); {
	case k == "binary_operator" || k == "unary_operator":
		for _, o := range named(v) {
			if !ks.staticValue(p, o).pure() {
				return vkind{}
			}
		}
		return outsideKind
	case pyLiteralKinds[k]:
		out := outsideKind
		walk(v, func(x *node) bool {
			switch x.Kind() {
			case "lambda":
				return false
			case "identifier", "call":
				out.holds = out.holds || !ks.staticValue(p, x).pure()
				return false
			}
			return !out.holds
		})
		return out
	case k == "identifier":
		return ks.staticName(p, f.text(v))
	case k == "call":
		fn := f.field(v, "function")
		if (pyBuiltinFunctions[fn] || pyCopies[fn]) && ks.top(p, fn) == nil {
			return outsideKind
		}
		if c := ks.typeName(p, fn); c.repo != "" {
			return c
		}
	}
	return vkind{}
}

// staticName is what a name used as a value at the top of the file p holds: a name from outside, a
// variable imported from the version, or one the module assigns.
func (ks *pyKinds) staticName(p, name string) vkind {
	if from, ok := ks.importsOf(p)[name]; ok {
		if from[0] == "" {
			return outsideKind
		}
		return ks.importedValue(p, name)
	}
	if d := ks.top(p, name); d != nil && d.Kind() == "assignment" {
		return ks.declaredValue(module(p), name)
	}
	return vkind{}
}

// top is the declaration of a name at the top of the file p: a class or function definition, or
// an assignment to the name alone. nil for none.
func (ks *pyKinds) top(p, name string) *node {
	f := ks.r.files[p]
	if f == nil {
		return nil
	}
	var out *node
	for _, n := range named(f.root) {
		d := definition(n)
		switch {
		case (d.Kind() == "class_definition" || d.Kind() == "function_definition") && f.field(d, "name") == name:
			out = d
		case n.Kind() == "expression_statement" && n.NamedChild(0) != nil && n.NamedChild(0).Kind() == "assignment":
			if a := n.NamedChild(0); f.field(a, "left") == name {
				out = a
			}
		}
	}
	return out
}
