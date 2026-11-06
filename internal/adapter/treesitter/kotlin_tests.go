//go:build cgo

package treesitter

import (
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/rerorets/hadome/internal/facts"
)

var ktLoops = set("for_statement", "while_statement", "do_while_statement")

func (k *kotlin) tests(f *file, t *tables) []facts.Test {
	if !f.test {
		return nil
	}
	// kotest's matchers: x.shouldBe(3), and x shouldBe 3 as an infix call.
	jf := &jvmFile{f: f, j: &k.jvm, t: t, names: map[string]string{}, helpers: map[string]*sitter.Node{}, assertPrefixes: []string{"should"}}
	for _, fn := range k.functions[f.unit] {
		jf.names[fn] = f.unit + "." + fn
	}
	for _, v := range k.values[f.unit] {
		jf.names[v] = f.unit + "." + v
	}
	jf.bindClasses(func(v func(string, bool, *sitter.Node)) { ktImports(f, v) })
	var out []facts.Test
	for _, n := range named(f.root) {
		if ktTypes[n.Kind()] {
			out = append(out, jf.ktClass(n, "", nil)...)
		}
	}
	return out
}

// ktAnnotations returns a declaration's annotations by name.
func ktAnnotations(f *file, n *sitter.Node) map[string]*sitter.Node {
	out := map[string]*sitter.Node{}
	for _, c := range named(n) {
		if c.Kind() != "modifiers" {
			continue
		}
		for _, a := range named(c) {
			if a.Kind() != "annotation" {
				continue
			}
			walk(a, func(x *sitter.Node) bool {
				if x.Kind() == "user_type" {
					out[lastElem(f.text(x))] = a
					return false
				}
				return true
			})
		}
	}
	return out
}

func (jf *jvmFile) ktClass(n *sitter.Node, prefix string, classSkip *sitter.Node) []facts.Test {
	f := jf.f
	name := prefix + f.field(n, "name")
	for k, a := range ktAnnotations(f, n) {
		if jvmSkipAnnotations[k] {
			classSkip = a
		}
	}
	var tests []*sitter.Node
	fields := jf.ktConstructorFields(n)
	for _, m := range ktClassMembers(n) {
		switch m.Kind() {
		case "property_declaration":
			if hasAny(ktAnnotations(f, m), jvmMockAnnotations) {
				jf.mocks = append(jf.mocks, m)
			} else {
				if name, class := jf.ktFieldType(m); class != "" {
					fields[name] = class
				}
				jf.ktNoteValueField(m)
			}
		case "function_declaration":
			if hasAny(ktAnnotations(f, m), jvmTestAnnotations) {
				tests = append(tests, m)
			} else {
				jf.helpers[f.field(m, "name")] = ktBody(m)
			}
		}
	}
	var out []facts.Test
	for _, m := range tests {
		out = append(out, jf.ktTest(name+"."+f.field(m, "name"), m, classSkip, fields))
	}
	for _, m := range ktMembers(n) {
		if ktTypes[m.Kind()] {
			out = append(out, jf.ktClass(m, name+".", classSkip)...)
		}
	}
	return out
}

func ktBody(fn *sitter.Node) *sitter.Node {
	for _, c := range named(fn) {
		if c.Kind() == "function_body" {
			return c
		}
	}
	return nil
}

func (jf *jvmFile) ktTest(name string, m, classSkip *sitter.Node, fields map[string]string) facts.Test {
	t := newTest(jf.f, name, m)
	t.fingerprint([]*sitter.Node{m}, m.ChildByFieldName("name"))
	skip := classSkip
	for k, a := range ktAnnotations(jf.f, m) {
		if jvmSkipAnnotations[k] {
			skip = a
		}
	}
	if skip != nil {
		t.skip(annotationReason(jf.f, skip), skip)
	}
	for _, mk := range jf.mocks {
		t.hazard(facts.HazardMock, mk)
	}
	w := &jvmWalk{jf: jf, t: t, vars: copyMap(fields), locals: map[string]bool{}, walking: map[string]bool{}}
	w.walk(ktBody(m), ktLoops, w.ktVisit)
	return t.done()
}

func (w *jvmWalk) ktVisit(n *sitter.Node) bool {
	switch n.Kind() {
	case "property_declaration":
		w.ktProperty(n)
	case "call_expression":
		w.ktCall(n)
	case "infix_expression":
		if ops := named(n); len(ops) == 3 {
			w.t.assertion(w.jf.f.text(ops[1]), w.jf.t.asserts, w.jf.assertPrefixes...)
		}
	case "identifier":
		if _, ok := w.jf.names[w.jf.f.text(n)]; ok {
			w.t.readName(n, ktUses)
		} else if w.readsValueField(n) {
			w.t.test.Reads++
		}
	}
	return true
}

var ktUses = useGrammar{
	members: map[string]string{"navigation_expression": ""},
	names:   map[string]string{"navigation_expression": "-"},
	callees: map[string][]string{"call_expression": {""}},
	types:   set("user_type", "type_arguments"),
}

// ktProperty records a local variable, typed when a constructor of a known class builds it.
func (w *jvmWalk) ktProperty(n *sitter.Node) {
	f := w.jf.f
	name := ""
	for _, c := range named(n) {
		switch c.Kind() {
		case "variable_declaration":
			name = f.text(c.NamedChild(0))
			w.locals[name] = true
		case "call_expression":
			if class, ok := w.jf.names[f.text(c.NamedChild(0))]; ok && name != "" {
				w.vars[name] = class
			}
		}
	}
}

func (w *jvmWalk) ktCall(n *sitter.Node) {
	f := w.jf.f
	callee := n.NamedChild(0)
	var args []*sitter.Node
	for _, c := range named(n) {
		if c.Kind() == "value_arguments" {
			for _, a := range arguments(c) {
				if v := ktArgumentValue(a); v != nil {
					args = append(args, v)
				}
			}
		}
	}
	obj, name := "", f.text(callee)
	if callee.Kind() == "navigation_expression" && callee.NamedChildCount() == 2 {
		obj, name = f.text(callee.NamedChild(0)), f.text(callee.NamedChild(1))
	}
	if obj == "" && w.jf.names[name] != "" && w.jf.j.classes[packageOf(w.jf.names[name])][name] {
		w.t.call(w.jf.names[name], nil) // a constructor call: Store()
		return
	}
	w.invoke(n, obj, name, args, func(body *sitter.Node) { w.walk(body, ktLoops, w.ktVisit) })
}

// packageOf returns the package of a class target ("a.b.Store" -> "a.b").
func packageOf(class string) string {
	return class[:len(class)-len(lastElem(class))-1]
}

// ktFieldType types a property of a test class, so that calls through it reach the product: by the
// constructor that builds it, or, with no initializer (lateinit, injected), by its declared type. A
// property built some other way (mockk(), a factory) is left untyped: what it holds is unknown.
func (jf *jvmFile) ktFieldType(n *sitter.Node) (string, string) {
	f := jf.f
	name, declared, init := "", "", (*sitter.Node)(nil)
	for _, c := range named(n) {
		switch c.Kind() {
		case "variable_declaration":
			name = f.text(c.NamedChild(0))
			if c.NamedChildCount() > 1 {
				declared = typeName(f.text(c.NamedChild(1)))
			}
		case "call_expression":
			init = c
		default:
			if name != "" && !strings.Contains(c.Kind(), "comment") && c.Kind() != "property_delegate" {
				init = c
			}
		}
	}
	switch {
	case name == "":
		return "", ""
	case init == nil:
		return name, jf.names[declared]
	case init.Kind() == "call_expression":
		return name, jf.names[f.text(init.NamedChild(0))]
	}
	return name, ""
}

// ktConstructorFields types the val and var parameters of a test class's primary constructor
// (constructor injection) by their declared types.
func (jf *jvmFile) ktConstructorFields(class *sitter.Node) map[string]string {
	f := jf.f
	out := map[string]string{}
	for _, c := range named(class) {
		if c.Kind() != "primary_constructor" {
			continue
		}
		for _, p := range ktClassParameters(c) {
			property, name, typ := false, "", ""
			for i := uint(0); i < p.ChildCount(); i++ {
				x := p.Child(i)
				switch x.Kind() {
				case "val", "var":
					property = true
				case "identifier", "simple_identifier":
					name = f.text(x)
				case "user_type", "nullable_type":
					typ = typeName(f.text(x))
				}
			}
			if property && name != "" && jf.names[typ] != "" {
				out[name] = jf.names[typ]
			}
		}
	}
	return out
}

func ktClassParameters(ctor *sitter.Node) []*sitter.Node {
	var out []*sitter.Node
	walk(ctor, func(n *sitter.Node) bool {
		if n.Kind() == "class_parameter" {
			out = append(out, n)
			return false
		}
		return true
	})
	return out
}

// typeName is a type's simple name, without its type arguments and nullability: Repo<T>? -> Repo.
func typeName(t string) string {
	if i := strings.IndexAny(t, "<?"); i >= 0 {
		t = t[:i]
	}
	return strings.TrimSpace(t)
}

func copyMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// ktArgumentValue is the expression of a value_argument: its last named child, since a named
// argument (expected = 1) starts with the name. Nil when it holds none.
func ktArgumentValue(a *sitter.Node) *sitter.Node {
	if a.NamedChildCount() == 0 {
		return nil
	}
	return a.NamedChild(a.NamedChildCount() - 1)
}

// ktNoteValueField records a property of a test class whose initializer names something of the
// product: its expression after the declaration (val query = QUERY.trim()).
func (jf *jvmFile) ktNoteValueField(n *sitter.Node) {
	name := "" // what comes before the name (modifiers) is not the initializer
	for _, c := range named(n) {
		switch {
		case c.Kind() == "variable_declaration":
			name = jf.f.text(c.NamedChild(0))
		case name != "" && !strings.Contains(c.Kind(), "comment"):
			jf.noteValueField(name, c)
		}
	}
}

// ktClassMembers lists a class's members with those of its companion object, whose properties and
// functions the class's tests use by their bare names.
func ktClassMembers(n *sitter.Node) []*sitter.Node {
	var out []*sitter.Node
	for _, m := range ktMembers(n) {
		if m.Kind() == "companion_object" {
			out = append(out, ktMembers(m)...)
			continue
		}
		out = append(out, m)
	}
	return out
}
