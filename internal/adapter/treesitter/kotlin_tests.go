//go:build cgo

package treesitter

import (
	"regexp"
	"strings"

	"github.com/rerorets/hadome/internal/facts"
)

var ktLoops = set("for_statement", "while_statement", "do_while_statement")

func (k *kotlin) tests(f *file, t *tables) []facts.Test {
	if !f.test {
		return nil
	}
	jf := k.testFile(f, t)
	var out []facts.Test
	for _, n := range named(f.root) {
		if ktTypes[n.Kind()] {
			out = append(out, jf.ktClass(n, "", nil, jvmScope{fields: map[string]string{}})...)
		}
	}
	return out
}

// testFile binds the names a Kotlin test file can use.
func (k *kotlin) testFile(f *file, t *tables) *jvmFile {
	// kotest's matchers: x.shouldBe(3), and x shouldBe 3 as an infix call. MockK's checks of
	// suspending calls (coVerify, coVerifyOrder) and confirmVerified start with neither verify nor
	// assert.
	jf := &jvmFile{f: f, j: &k.jvm, kt: k, t: t, names: map[string]string{}, helpers: map[string]*node{},
		assertPrefixes: []string{"should", "coverify", "confirmverified"}}
	for _, fn := range k.functions[f.unit] {
		jf.names[fn] = f.unit + "." + fn
	}
	for _, v := range k.values[f.unit] {
		jf.names[v] = f.unit + "." + v
	}
	jf.bindClasses(func(v func(string, bool, *node)) { ktImports(f, v) })
	jf.bindTopHelpers()
	return jf
}

// ktAnnotations returns a declaration's annotations by name.
func ktAnnotations(f *file, n *node) map[string]*node {
	out := map[string]*node{}
	for _, c := range named(n) {
		if c.Kind() != "modifiers" {
			continue
		}
		for _, a := range named(c) {
			if a.Kind() != "annotation" {
				continue
			}
			walk(a, func(x *node) bool {
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

func (jf *jvmFile) ktClass(n *node, prefix string, classSkip *node, outer jvmScope) []facts.Test {
	f := jf.f
	name := prefix + f.field(n, "name")
	anns := ktAnnotations(f, n)
	leading, _ := ktLeading(f, n)
	for k, a := range leading {
		anns[k] = a
	}
	for k, a := range anns {
		if jvmSkipAnnotations[k] {
			classSkip = a
		}
	}
	scope := outer.nested().withInputs(anns)
	jf.ktInherit(n, &scope, 0)
	tests, mocks := jf.ktCollect(n, &scope)
	jf.mocks = append(jf.mocks, mocks...)
	var out []facts.Test
	for _, m := range tests {
		out = append(out, jf.ktTest(name+"."+f.field(m, "name"), m, classSkip, scope))
	}
	for _, m := range ktMembers(n) {
		if ktTypes[m.Kind()] {
			out = append(out, jf.ktClass(m, name+".", classSkip, scope)...)
		}
	}
	return out
}

func ktBody(fn *node) *node {
	for _, c := range named(fn) {
		if c.Kind() == "function_body" {
			return c
		}
	}
	return nil
}

func (jf *jvmFile) ktTest(name string, m, classSkip *node, scope jvmScope) facts.Test {
	t := newTest(jf.f, name, m)
	nodes := append(append([]*node{}, scope.inputs...), m)
	t.fingerprint(nodes, append(jvmNameOnly(jf.f, nodes), m.ChildByFieldName("name")))
	skip := classSkip
	for k, a := range ktAnnotations(jf.f, m) {
		if jvmSkipAnnotations[k] {
			skip = a
		}
	}
	if skip != nil {
		t.skip(annotationReason(jf.f, skip), skip)
	}
	w := jf.startTest(t, scope, func(w *jvmWalk, n *node) { w.walk(n, ktLoops, w.ktVisit) })
	w.walk(ktBody(m), ktLoops, w.ktVisit)
	return t.done()
}

func (w *jvmWalk) ktVisit(n *node, kind string) bool {
	switch kind {
	case "property_declaration":
		w.ktProperty(n)
	case "call_expression":
		w.ktCall(n)
	case "binary_expression":
		if callee, arg := ktTypedCall(n); callee != nil {
			w.ktInvoke(n, callee, []*node{arg})
			w.walk(arg, ktLoops, w.ktVisit) // not the type argument, which is no value the test reads
			return false
		}
	case "infix_expression":
		if ops := named(n); len(ops) == 3 {
			w.t.assertion(w.jf.f.text(ops[1]), w.jf.t.asserts, w.jf.assertPrefixes...)
		}
	case "string_literal":
		w.ktTemplateReads(n)
	case "identifier":
		if _, ok := w.jf.names[w.jf.f.text(n)]; ok {
			w.t.readName(n, ktUses)
		} else if w.readsValueField(n) || w.ktReadsProperty(n) {
			w.t.test.Reads++
		}
	}
	return true
}

// ktTemplateReads counts the product values and value fields a string substitutes by name
// ("IN ($COUNTED)").
func (w *jvmWalk) ktTemplateReads(n *node) {
	for _, name := range ktTemplateNames(w.jf.f, n) {
		if !w.locals[name] && (w.jf.names[name] != "" || w.valueField(name)) {
			w.t.test.Reads++
		}
	}
}

// ktTemplateNames lists the names a one-line string substitutes as $name. This grammar keeps them
// in the string's text, though it reads ${…}, and the $name of a multi-line string, as code of their
// own. An escaped \$ is a dollar sign.
func ktTemplateNames(f *file, n *node) []string {
	text := []byte(f.text(n))
	for _, c := range named(n) {
		if c.Kind() == "interpolation" {
			for i := c.StartByte(); i < c.EndByte(); i++ {
				text[i-n.StartByte()] = ' '
			}
		}
	}
	var out []string
	for _, m := range ktTemplateName.FindAllSubmatch(text, -1) {
		if len(m[1])%2 == 0 {
			out = append(out, string(m[2]))
		}
	}
	return out
}

// ktTemplateName finds $name in a string's text, with the backslashes before it.
var ktTemplateName = regexp.MustCompile(`(\\*)\$([A-Za-z_][A-Za-z0-9_]*)`)

var ktUses = useGrammar{
	members: map[string]string{"navigation_expression": ""},
	names:   map[string]string{"navigation_expression": "-"},
	callees: map[string][]string{"call_expression": {""}},
	types:   set("user_type", "type_arguments"),
}

// ktProperty records a local variable, typed when it holds an object of a known class (ktBuilt).
func (w *jvmWalk) ktProperty(n *node) {
	f := w.jf.f
	name := ""
	for _, c := range named(n) {
		switch {
		case c.Kind() == "variable_declaration":
			name = f.text(c.NamedChild(0))
			w.locals[name] = true
			kind := ""
			if c.NamedChildCount() > 1 {
				kind = w.builtinType(f.text(c.NamedChild(1)))
			}
			w.noteBuiltin(name, kind)
		case name != "" && !strings.Contains(c.Kind(), "comment"):
			if class := w.jf.ktBuilt(c); class != "" {
				w.vars[name] = class
			}
			if w.builtins[name] == "" {
				w.noteBuiltin(name, w.ktBuiltin(c))
			}
		}
	}
}

var (
	ktReturnsReceiver = set("apply", "also")
	// ktScopeFunctions are the standard library's functions every object has, not the product's.
	ktScopeFunctions = set("apply", "also", "let", "run", "takeIf", "takeUnless")
)

func (w *jvmWalk) ktCall(n *node) {
	callee := n.NamedChild(0)
	if callee.Kind() == "call_expression" {
		return // f(x) { … }: the call is the inner f(x), visited on its own
	}
	var args []*node
	for _, c := range named(n) {
		if c.Kind() == "value_arguments" {
			for _, a := range arguments(c) {
				if v := ktArgumentValue(a); v != nil {
					args = append(args, v)
				}
			}
		}
	}
	w.ktInvoke(n, callee, args)
}

// ktInvoke handles the call n of callee: a constructor of the product, or what invoke handles.
func (w *jvmWalk) ktInvoke(n, callee *node, args []*node) {
	f := w.jf.f
	obj, name, built := "", f.text(callee), ""
	if callee.Kind() == "navigation_expression" && callee.NamedChildCount() == 2 {
		obj, name = f.text(callee.NamedChild(0)), f.text(callee.NamedChild(1))
		if callee.NamedChild(0).Kind() == "call_expression" && !ktScopeFunctions[name] {
			built = w.jf.ktBuilt(callee.NamedChild(0)) // Store().append(1)
		}
	}
	if obj == "" && w.jf.names[name] != "" && w.jf.j.classes[packageOf(w.jf.names[name])][name] {
		w.t.call(w.jf.names[name], nil) // a constructor call: Store()
		return
	}
	w.invoke(n, obj, name, args, func(body *node) { w.walk(body, ktLoops, w.ktVisit) })
	if built != "" {
		w.t.call(built+"."+name, scopeArg(w.jf.j.scopes, built+"."+name, args))
	}
}

// packageOf returns the package of a class target ("a.b.Store" -> "a.b").
func packageOf(class string) string {
	return class[:len(class)-len(lastElem(class))-1]
}

// ktFieldType types a property of a test class, so that calls through it reach the product: by the
// object its initializer builds (ktBuilt), or, with no initializer (lateinit, injected), by its
// declared type. A property built some other way (mockk(), a factory) is left untyped: what it holds
// is unknown.
func (jf *jvmFile) ktFieldType(n *node) (string, string) {
	f := jf.f
	name, declared, init := "", "", (*node)(nil)
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
	}
	return name, jf.ktBuilt(init)
}

// ktConstructorFields types the val and var parameters of a test class's primary constructor
// (constructor injection) by their declared types.
func (jf *jvmFile) ktConstructorFields(class *node) map[string]string {
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

func ktClassParameters(ctor *node) []*node {
	var out []*node
	walk(ctor, func(n *node) bool {
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
func ktArgumentValue(a *node) *node {
	if a.NamedChildCount() == 0 {
		return nil
	}
	return a.NamedChild(a.NamedChildCount() - 1)
}

// ktNoteValueField records a property of a test class whose initializer names something of the
// product: its expression after the declaration (val query = QUERY.trim()). It returns the
// property's name, and whether it is such a field.
func (jf *jvmFile) ktNoteValueField(n *node) (string, bool) {
	name, uses := "", false // what comes before the name (modifiers) is not the initializer
	for _, c := range named(n) {
		switch {
		case c.Kind() == "variable_declaration":
			name = jf.f.text(c.NamedChild(0))
		case name != "" && !strings.Contains(c.Kind(), "comment"):
			uses = jf.noteValueField(name, c) || uses
			walk(c, func(x *node) bool {
				if x.Kind() != "string_literal" {
					return true
				}
				for _, used := range ktTemplateNames(jf.f, x) {
					if jf.names[used] != "" {
						jf.markValueField(name)
						uses = true
					}
				}
				return true
			})
		}
	}
	return name, uses
}
