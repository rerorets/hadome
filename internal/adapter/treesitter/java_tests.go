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
	jvmMockAnnotations = set("Mock", "MockBean", "InjectMocks", "Spy", "MockK", "RelaxedMockK", "SpyK",
		"InjectMock", "InjectSpy")
	javaLoops = set("for_statement", "enhanced_for_statement", "while_statement", "do_statement")
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
}

// bindClasses binds the classes of the file's own package and of what it imports.
func (jf *jvmFile) bindClasses(imports func(visit func(name string, wildcard bool, n *sitter.Node))) {
	for c := range jf.j.classes[jf.f.unit] {
		jf.names[c] = jf.f.unit + "." + c
	}
	imports(func(name string, wildcard bool, n *sitter.Node) {
		if jf.t.isMock(name) {
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
			out = append(out, jf.javaClass(n, "", nil)...)
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

func (jf *jvmFile) javaClass(n *sitter.Node, prefix string, classSkip *sitter.Node) []facts.Test {
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
	fields := map[string]string{}
	for _, m := range named(body) {
		switch {
		case m.Kind() == "field_declaration":
			if hasAny(annotations(f, m), jvmMockAnnotations) {
				jf.mocks = append(jf.mocks, m)
			} else {
				jf.javaFieldTypes(m, fields)
				for _, d := range named(m) {
					if d.Kind() == "variable_declarator" {
						jf.noteValueField(f.field(d, "name"), d.ChildByFieldName("value"))
					}
				}
			}
		case m.Kind() == "method_declaration":
			methods = append(methods, m)
			jf.helpers[f.field(m, "name")] = m.ChildByFieldName("body")
		}
	}
	var out []facts.Test
	for _, m := range methods {
		if a := annotations(f, m); hasAny(a, jvmTestAnnotations) {
			out = append(out, jf.javaTest(name+"."+f.field(m, "name"), m, a, classSkip, fields))
		}
	}
	for _, m := range named(body) {
		if javaTypes[m.Kind()] {
			out = append(out, jf.javaClass(m, name+".", classSkip)...)
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

func (jf *jvmFile) javaTest(name string, m *sitter.Node, anns map[string]*sitter.Node, classSkip *sitter.Node, fields map[string]string) facts.Test {
	t := newTest(jf.f, name, m)
	t.fingerprint([]*sitter.Node{m}, m.ChildByFieldName("name"))
	skip := classSkip
	for k := range jvmSkipAnnotations {
		if a, ok := anns[k]; ok {
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
}

func (w *jvmWalk) walk(body *sitter.Node, loops map[string]bool, visit func(*sitter.Node) bool) {
	w.t.body(body, func(n *sitter.Node) bool { return loops[n.Kind()] }, visit)
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
	if !w.t.assertion(key, w.jf.t.asserts, w.jf.assertPrefixes...) && jvmValidation(obj, name) {
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
		if tg, ok := w.jf.names[name]; ok {
			target = tg
		} else if body, ok := w.jf.helpers[name]; ok && !w.walking[name] {
			w.walking[name] = true
			follow(body)
			delete(w.walking, name)
		}
	case w.vars[obj] != "":
		target = w.vars[obj] + "." + name
	case w.jf.names[obj] != "":
		target = w.jf.names[obj] + "." + name
	case w.locals[obj]:
		w.t.test.Unresolved++
	}
	if target != "" {
		w.t.call(target, scopeArg(w.jf.j.scopes, target, args))
	}
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

func (w *jvmWalk) javaVisit(n *sitter.Node) bool {
	f := w.jf.f
	switch n.Kind() {
	case "local_variable_declaration":
		typ := f.field(n, "type")
		for _, d := range named(n) {
			if d.Kind() != "variable_declarator" {
				continue
			}
			name := f.field(d, "name")
			w.locals[name] = true
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
		if _, ok := w.jf.names[f.text(n)]; ok {
			w.t.readName(n, javaUses)
		} else if w.readsValueField(n) {
			w.t.test.Reads++
		}
	}
	return true
}

var javaUses = useGrammar{
	members: map[string]string{"field_access": "object"},
	names:   map[string]string{"field_access": "field"},
	callees: map[string][]string{"method_invocation": {"object", "name"}},
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

// restAssuredChecks are the checks of REST Assured's validation chain, which starts at then().
var restAssuredChecks = set("statusCode", "statusLine", "body", "header", "headers", "contentType", "cookie",
	"cookies", "time")

// jvmValidation recognises checks that do not start with assert: REST Assured's
// given()…then().statusCode(200).body(…), where each call after then() checks the response, and
// Spring MockMvc's perform(…).andExpect(…).
func jvmValidation(obj, name string) bool {
	if name == "andExpect" {
		return true
	}
	return restAssuredChecks[name] && strings.Contains(strings.Join(strings.Fields(obj), ""), "then()")
}

// jvmDrivers are the HTTP test clients, by the package a test file imports, and the calls with which
// a test drives the application through them.
var jvmDrivers = map[string][]string{
	"io.restassured":                               {"given"},
	"org.springframework.test.web.servlet":         {"perform"},
	"org.springframework.test.web.reactive.server": {"exchange"},
}

// noteValueField records a field of a test class whose initializer names something of the product.
func (jf *jvmFile) noteValueField(name string, init *sitter.Node) {
	if name == "" || init == nil {
		return
	}
	uses := false
	walk(init, func(n *sitter.Node) bool {
		uses = uses || (strings.Contains(n.Kind(), "identifier") && jf.names[jf.f.text(n)] != "")
		return !uses
	})
	if uses {
		if jf.valueFields == nil {
			jf.valueFields = map[string]bool{}
		}
		jf.valueFields[name] = true
	}
}

// readsValueField reports whether an identifier in a test reads a value field of its class: not a
// local of the same name, and not the member side of a.b.
func (w *jvmWalk) readsValueField(n *sitter.Node) bool {
	name := w.jf.f.text(n)
	if !w.jf.valueFields[name] || w.locals[name] {
		return false
	}
	// The member side of a.b names a's field, unless a is this.
	switch p := n.Parent(); p.Kind() {
	case "navigation_expression":
		return !isField(p, "-", n) || w.jf.f.text(p.NamedChild(0)) == "this"
	case "field_access":
		return !isField(p, "field", n) || w.jf.f.field(p, "object") == "this"
	}
	return true
}
