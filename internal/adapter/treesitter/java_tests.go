//go:build cgo

package treesitter

import (
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/rerorets/hadome/internal/facts"
)

var (
	jvmTestAnnotations = set("Test", "ParameterizedTest", "RepeatedTest", "TestFactory", "TestTemplate")
	jvmSkipAnnotations = set("Disabled", "Ignore")
	// Mockito's @InjectMocks is not among them: its field holds a real instance of the product with
	// the mocks injected, and only those mocks replace anything.
	jvmMockAnnotations = set("Mock", "MockBean", "Spy", "MockK", "RelaxedMockK", "SpyK",
		"InjectMock", "InjectSpy")
	// jvmRealImports are imports from a mocking library that replace nothing.
	jvmRealImports = set("org.mockito.InjectMocks")
	// jvmSpyAnnotations mark the mock fields that run the real methods of the object they wrap: a
	// call through one calls the product.
	jvmSpyAnnotations = set("Spy", "SpyK", "InjectSpy")
	javaLoops         = set("for_statement", "enhanced_for_statement", "while_statement", "do_statement")
)

// jvmFile is what a Java or Kotlin test file binds: class names it can use, helpers, and mocks.
type jvmFile struct {
	f       *file
	j       *jvm
	t       *tables
	names   map[string]string // simple name -> target: a class ("a.b.Store") or a static import ("a.b.Store.create")
	helpers map[string]*sitter.Node
	mocks   []*sitter.Node
	// drivers are the names of the HTTP test clients the file imports, whose calls drive the
	// application from outside.
	drivers map[string]bool
	// valueFields are the fields of the file's test classes whose initializer uses the product
	// (private val query = QUERY.trim()): a test reading one reads the product's value.
	valueFields map[string]bool
	// assertPrefixes are what start the names of assertions in this language beyond the common ones.
	assertPrefixes []string
	// kt is the Kotlin reader, which finds the test classes a Kotlin test class extends; nil in Java.
	kt *kotlin
	// topHelpers are the top-level functions of Kotlin test sources the file can call by their bare
	// names: those of its own package and those it imports.
	topHelpers map[string]jvmDecl
	// others are the files of topHelpers, read as their own files bind names.
	others map[*file]*jvmFile
}

// bindClasses binds the classes of the file's own package and of what it imports.
func (jf *jvmFile) bindClasses(imports func(visit func(name string, wildcard bool, n *sitter.Node))) {
	for c := range jf.j.classes[jf.f.unit] {
		jf.names[c] = jf.f.unit + "." + c
	}
	imports(func(name string, wildcard bool, n *sitter.Node) {
		if jf.t.isMock(name) && !jvmRealImports[name] {
			jf.mocks = append(jf.mocks, n)
		}
		for prefix, calls := range jvmDrivers {
			if strings.HasPrefix(name, prefix) {
				if jf.drivers == nil {
					jf.drivers = map[string]bool{}
				}
				for _, c := range calls {
					jf.drivers[c] = true
				}
			}
		}
		pkg, class, internal := jf.j.resolve(name, wildcard)
		switch {
		case !internal:
		case wildcard && class == "":
			for c := range jf.j.classes[pkg] {
				jf.names[c] = pkg + "." + c
			}
		case class != "":
			rest := name[len(pkg)+1:] // "Store", or "Store.create" for a static import
			jf.names[lastElem(rest)] = pkg + "." + rest
		}
	})
}

func lastElem(dotted string) string {
	for i := len(dotted) - 1; i >= 0; i-- {
		if dotted[i] == '.' {
			return dotted[i+1:]
		}
	}
	return dotted
}

func (j *java) tests(f *file, t *tables) []facts.Test {
	if !f.test {
		return nil
	}
	jf := &jvmFile{f: f, j: &j.jvm, t: t, names: map[string]string{}, helpers: map[string]*sitter.Node{}}
	jf.bindClasses(func(v func(string, bool, *sitter.Node)) { javaImports(f, v) })
	var out []facts.Test
	for _, n := range named(f.root) {
		if javaTypes[n.Kind()] {
			out = append(out, jf.javaClass(n, "", nil, jvmScope{fields: map[string]string{}})...)
		}
	}
	return out
}

// annotations returns the annotations of a declaration by name, with their first string argument.
func annotations(f *file, n *sitter.Node) map[string]*sitter.Node {
	out := map[string]*sitter.Node{}
	for _, c := range named(n) {
		if c.Kind() != "modifiers" {
			continue
		}
		walk(c, func(a *sitter.Node) bool {
			if a.Kind() == "marker_annotation" || a.Kind() == "annotation" {
				out[lastElem(f.field(a, "name"))] = a
				return false
			}
			return true
		})
	}
	return out
}

// annotationReason is an annotation's first string argument.
func annotationReason(f *file, a *sitter.Node) string {
	reason := ""
	walk(a, func(x *sitter.Node) bool {
		if reason == "" && (x.Kind() == "string_literal" || x.Kind() == "string_fragment" || x.Kind() == "string_content") {
			reason = unquote(f.text(x))
			return false
		}
		return reason == ""
	})
	return reason
}

func (jf *jvmFile) javaClass(n *sitter.Node, prefix string, classSkip *sitter.Node, outer jvmScope) []facts.Test {
	f := jf.f
	name := prefix + f.field(n, "name")
	anns := annotations(f, n)
	for k := range jvmSkipAnnotations {
		if a, ok := anns[k]; ok {
			classSkip = a
		}
	}
	body := n.ChildByFieldName("body")
	var methods []*sitter.Node
	scope := outer.nested().withInputs(anns)
	for _, m := range named(body) {
		switch {
		case m.Kind() == "field_declaration":
			jf.javaField(m, scope.fields)
		case m.Kind() == "method_declaration":
			methods = append(methods, m)
			jf.helpers[f.field(m, "name")] = m.ChildByFieldName("body")
			if hasAny(annotations(f, m), jvmSetupAnnotations) {
				scope.setups = append(scope.setups, jvmCode{jf, m.ChildByFieldName("body")})
			}
		}
	}
	var out []facts.Test
	for _, m := range methods {
		if a := annotations(f, m); hasAny(a, jvmTestAnnotations) {
			out = append(out, jf.javaTest(name+"."+f.field(m, "name"), m, a, classSkip, scope))
		}
	}
	for _, m := range named(body) {
		if javaTypes[m.Kind()] {
			out = append(out, jf.javaClass(m, name+".", classSkip, scope)...)
		}
	}
	return out
}

func hasAny(m map[string]*sitter.Node, keys map[string]bool) bool {
	for k := range m {
		if keys[k] {
			return true
		}
	}
	return false
}

func (jf *jvmFile) javaTest(name string, m *sitter.Node, anns map[string]*sitter.Node, classSkip *sitter.Node, scope jvmScope) facts.Test {
	t := newTest(jf.f, name, m)
	nodes := append(append([]*sitter.Node{}, scope.inputs...), m)
	t.fingerprint(nodes, append(jvmNameOnly(jf.f, nodes), m.ChildByFieldName("name")))
	skip := classSkip
	for k := range jvmSkipAnnotations {
		if a, ok := anns[k]; ok {
			skip = a
		}
	}
	if skip != nil {
		t.skip(annotationReason(jf.f, skip), skip)
	}
	w := jf.startTest(t, scope, func(w *jvmWalk, n *sitter.Node) { w.walk(n, javaLoops, w.javaVisit) })
	w.walk(m.ChildByFieldName("body"), javaLoops, w.javaVisit)
	return t.done()
}

// jvmWalk walks one Java or Kotlin test body.
type jvmWalk struct {
	jf      *jvmFile
	t       *testFacts
	vars    map[string]string
	locals  map[string]bool
	walking map[string]bool
	// inherited are the helpers of a test class of another file that the test's class extends.
	inherited map[string]jvmCode
	// values are the value fields of that class.
	values map[string]bool
	// builtins are the locals that hold a string or a standard collection (jvm_builtins.go).
	builtins map[string]string
}

func (w *jvmWalk) walk(body *sitter.Node, loops map[string]bool, visit func(*sitter.Node, string) bool) {
	w.t.bodyIn(w.jf.f, w.jf.t, body, func(kind string) bool { return loops[kind] }, visit)
}

// invoke handles a call to name on obj ("" for an unqualified call). It reports hazards,
// always-true assertions, targets and helpers.
func (w *jvmWalk) invoke(n *sitter.Node, obj, name string, args []*sitter.Node, follow func(*sitter.Node)) {
	f := w.jf.f
	key := name
	if obj != "" {
		key = obj + "." + name
	}
	if w.jf.drivers[name] {
		w.t.test.Drives = true
	}
	if !w.t.assertion(key, w.jf.t.asserts, w.jf.assertPrefixes...) && w.validation(obj, name) {
		w.t.test.Asserts++
	}
	if cat, ok := w.jf.t.calls[key]; ok {
		w.t.hazard(cat, n)
		return
	}
	if jvmAlwaysTrue(f, name, args) {
		w.t.hazard(facts.HazardAlwaysTrue, n)
		return
	}
	target := ""
	switch {
	case obj == "":
		target = w.unqualified(name, follow)
	case w.vars[obj] != "":
		target = w.vars[obj] + "." + name
	case w.jf.names[obj] != "":
		target = w.jf.names[obj] + "." + name
	case w.locals[obj]:
		if w.builtins[obj] == "" {
			w.t.test.Unresolved++
		}
	default:
		target = w.member(obj, name)
	}
	if target != "" {
		w.t.call(target, scopeArg(w.jf.j.scopes, target, args))
	}
}

// unqualified resolves a call by a bare name: a target the file binds, or a helper of the file or
// of a class the test's class extends, which it follows.
func (w *jvmWalk) unqualified(name string, follow func(*sitter.Node)) string {
	if tg, ok := w.jf.names[name]; ok {
		return tg
	}
	if w.walking[name] {
		return ""
	}
	w.walking[name] = true
	if h := w.helper(name); h.n != nil {
		w.within(h.jf, func() { follow(h.n) })
	}
	delete(w.walking, name)
	return ""
}

func jvmAlwaysTrue(f *file, name string, args []*sitter.Node) bool {
	switch name {
	case "assertTrue":
		return len(args) == 1 && f.text(args[0]) == "true"
	case "assertFalse":
		return len(args) == 1 && f.text(args[0]) == "false"
	case "assertEquals", "assertSame":
		return len(args) >= 2 && sameValue(f, args[0], args[1])
	}
	return false
}

func (w *jvmWalk) javaVisit(n *sitter.Node, kind string) bool {
	f := w.jf.f
	switch kind {
	case "local_variable_declaration":
		typ := f.field(n, "type")
		for _, d := range named(n) {
			if d.Kind() != "variable_declarator" {
				continue
			}
			name := f.field(d, "name")
			w.locals[name] = true
			kind := w.builtinType(typ)
			if kind == "" {
				kind = w.javaBuiltin(d.ChildByFieldName("value"))
			}
			w.noteBuiltin(name, kind)
			if class, ok := w.jf.names[typ]; ok {
				w.vars[name] = class
			} else if v := d.ChildByFieldName("value"); v != nil && v.Kind() == "object_creation_expression" {
				if class, ok := w.jf.names[f.field(v, "type")]; ok {
					w.vars[name] = class
				}
			}
		}
	case "object_creation_expression":
		typ := f.field(n, "type")
		if cat, ok := w.jf.t.calls["new "+typ]; ok {
			w.t.hazard(cat, n)
		} else if class, ok := w.jf.names[typ]; ok {
			w.t.call(class, nil)
		}
	case "method_invocation":
		w.invoke(n, f.field(n, "object"), f.field(n, "name"), arguments(n.ChildByFieldName("arguments")),
			func(body *sitter.Node) { w.walk(body, javaLoops, w.javaVisit) })
	case "identifier": // a name in a type is a type_identifier, never an identifier
		w.javaIdentifier(n)
	}
	return true
}

// javaIdentifier counts the reads of product values a name makes: the product's own name, a value
// field of the test class, or a field of a product object (cache.ttl).
func (w *jvmWalk) javaIdentifier(n *sitter.Node) {
	name := w.jf.f.text(n)
	if _, ok := w.jf.names[name]; ok {
		w.t.readName(n, javaUses)
	} else if w.readsValueField(n) {
		w.t.test.Reads++
	} else if p := n.Parent(); w.vars[name] != "" && p.Kind() == "field_access" && isField(p, "object", n) {
		w.t.test.Reads++
	}
}

var javaUses = useGrammar{
	members: map[string]string{"field_access": "object"},
	names:   map[string]string{"field_access": "field"},
	callees: map[string][]string{"method_invocation": {"object", "name"}},
}

// javaField reads a field of a test class: a mock, typed only when it is a spy, or a field typed
// into fields and noted when its initializer uses the product.
func (jf *jvmFile) javaField(m *sitter.Node, fields map[string]string) {
	anns := annotations(jf.f, m)
	mock, spy := hasAny(anns, jvmMockAnnotations), hasAny(anns, jvmSpyAnnotations)
	if mock {
		jf.mocks = append(jf.mocks, m)
	}
	if !mock || spy {
		jf.javaFieldTypes(m, fields)
	}
	if mock {
		return
	}
	for _, d := range named(m) {
		if d.Kind() == "variable_declarator" {
			jf.noteValueField(jf.f.field(d, "name"), d.ChildByFieldName("value"))
		}
	}
}

// javaFieldTypes types the fields of a test class, as ktFieldType does: by the constructor that
// builds a field, or, with no initializer (injected), by its declared type. A field built some other
// way (mock(), a factory) is left untyped.
func (jf *jvmFile) javaFieldTypes(n *sitter.Node, into map[string]string) {
	f := jf.f
	declared := typeName(f.field(n, "type"))
	for _, d := range named(n) {
		if d.Kind() != "variable_declarator" {
			continue
		}
		name, v := f.field(d, "name"), d.ChildByFieldName("value")
		switch {
		case v == nil:
			if class := jf.names[declared]; class != "" {
				into[name] = class
			}
		case v.Kind() == "object_creation_expression":
			if class := jf.names[typeName(f.field(v, "type"))]; class != "" {
				into[name] = class
			}
		}
	}
}
