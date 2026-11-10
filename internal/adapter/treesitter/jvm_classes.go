//go:build cgo

package treesitter

import (
	"regexp"
	"sort"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/rerorets/hadome/internal/facts"
)

// What a Java or Kotlin test class gives its tests beyond their own bodies: the fields, helpers and
// setup of the classes around and above it, and the members and checks a call reaches.

// jvmCode is a node of a test file with the file that holds it, which may not be the test's.
type jvmCode struct {
	jf *jvmFile
	n  *sitter.Node
}

// jvmScope is what a test class gives its tests beyond its file: its fields with those of the
// classes enclosing and extending it, the helpers and mocks of a class it extends, and the bodies
// that run before each test (@BeforeEach and the like, and Kotlin's init blocks).
type jvmScope struct {
	fields    map[string]string
	inherited map[string]jvmCode
	// values are the fields the class and the test classes it extends declare, by whether the last
	// declaration of each is a value field (jvmFile.valueFields).
	values map[string]bool
	mocks  []jvmCode
	setups []jvmCode
	// inputs are the annotations of the parameterised classes around the test (@ParameterizedClass
	// with its @ValueSource): the test's inputs, which its fingerprint includes.
	inputs []*sitter.Node
	// setupHazards are the clock and sleep hazards of each setup, by the setup's node, once it has
	// been walked for one test of the class: what the walk finds depends only on the class, so every
	// test of the class shares it. A nested class starts with none of its own, since its fields differ.
	setupHazards map[uintptr][]facts.Hazard
}

// nested is the scope a class declared inside this one starts from: a JUnit @Nested class sees the
// fields and helpers of its enclosing class, and its tests run that class's setup first.
func (s jvmScope) nested() jvmScope {
	out := jvmScope{fields: copyMap(s.fields), inherited: map[string]jvmCode{}, values: map[string]bool{},
		setupHazards: map[uintptr][]facts.Hazard{}}
	for k, v := range s.inherited {
		out.inherited[k] = v
	}
	for k := range s.values {
		out.values[k] = true
	}
	out.mocks = append(out.mocks, s.mocks...)
	out.setups = append(out.setups, s.setups...)
	out.inputs = append(out.inputs, s.inputs...)
	return out
}

// withInputs adds the annotations of a class to the scope's inputs when the class is parameterised.
// anns are the class's annotations by name; they join in the order they are written.
func (s jvmScope) withInputs(anns map[string]*sitter.Node) jvmScope {
	if anns["ParameterizedClass"] == nil {
		return s
	}
	var add []*sitter.Node
	for _, a := range anns {
		add = append(add, a)
	}
	sort.Slice(add, func(i, j int) bool { return add[i].StartByte() < add[j].StartByte() })
	s.inputs = append(append([]*sitter.Node{}, s.inputs...), add...)
	return s
}

// startTest records what a test has from its file and class before its body runs, the mocks and
// the hazards of its setup, and returns the walker for its body. walk walks a body of code.
func (jf *jvmFile) startTest(t *testFacts, scope jvmScope, walk func(*jvmWalk, *sitter.Node)) *jvmWalk {
	for _, mk := range jf.mocks {
		t.fileHazard(facts.HazardMock, mk)
	}
	for _, mk := range scope.mocks {
		t.hazardIn(mk.jf.f, facts.HazardMock, mk.n)
	}
	w := &jvmWalk{jf: jf, t: t, vars: copyMap(scope.fields), locals: map[string]bool{}, walking: map[string]bool{},
		inherited: scope.inherited, values: scope.values}
	// The setup runs before each test of the class, so reading the clock or sleeping there makes
	// every one of them depend on time. Only those hazards are its test's: what the setup calls or
	// checks is not what the test guards.
	for _, s := range scope.setups {
		hazards, ok := scope.setupHazards[s.n.Id()]
		if !ok {
			sw := &jvmWalk{jf: s.jf, t: newTest(s.jf.f, "", s.n), vars: copyMap(scope.fields), locals: map[string]bool{},
				walking: map[string]bool{}, inherited: scope.inherited}
			walk(sw, s.n)
			for _, h := range sw.t.test.Hazards {
				if h.Category == facts.HazardClock || h.Category == facts.HazardSleep {
					hazards = append(hazards, h)
				}
			}
			scope.setupHazards[s.n.Id()] = hazards
		}
		for _, h := range hazards {
			t.addHazard(h)
		}
	}
	return w
}

// within walks code of another file, an inherited helper, as part of the test: names resolve as
// that file binds them, and hazards are found where that file has them.
func (w *jvmWalk) within(jf *jvmFile, walk func()) {
	prev, prevFile := w.jf, w.t.f
	w.jf, w.t.f = jf, jf.f
	walk()
	w.jf, w.t.f = prev, prevFile
}

// member resolves a call on a member of a product class: Kind.ORDERS.toTable() calls the class's
// own method on one of its enum constants, and Outer.Inner.build() a method of a nested class. Any
// other member, such as a property of an object, holds a value of the product the test reads, but
// of a class unknown here, so the call counts as that read.
func (w *jvmWalk) member(obj, name string) string {
	head, rest, ok := strings.Cut(strings.Join(strings.Fields(obj), ""), ".")
	class := w.jf.names[head]
	if !ok || w.locals[head] || !w.jf.j.isClass(class) || !jvmDottedName.MatchString(rest) {
		return ""
	}
	if w.jf.j.entries[class][rest] {
		return class + "." + name
	}
	if _, ok := w.jf.j.scopes[class+"."+rest+"."+name]; ok {
		return class + "." + rest + "." + name
	}
	w.t.test.Reads++
	return ""
}

var jvmDottedName = regexp.MustCompile(`^[A-Za-z_$][\w$]*(\.[A-Za-z_$][\w$]*)*$`)

// restAssuredChecks are the checks of REST Assured's validation chain, which starts at then().
var restAssuredChecks = set("statusCode", "statusLine", "body", "header", "headers", "contentType", "cookie",
	"cookies", "time")

// validation recognises checks that do not start with assert: REST Assured's
// given()…then().statusCode(200).body(…), where each call after then() checks the response, and
// Spring MockMvc's perform(…).andExpect(…). The chain may also start at a helper of the file, or of
// a class the test's class extends, whose body ends in then() or that is declared to return what
// then() returns: list().statusCode(200).
func (w *jvmWalk) validation(obj, name string) bool {
	if name == "andExpect" {
		return true
	}
	if !restAssuredChecks[name] {
		return false
	}
	chain := strings.Join(strings.Fields(obj), "")
	if strings.Contains(chain, "then()") {
		return true
	}
	head, _, called := strings.Cut(chain, "(")
	if !called || strings.Contains(head, ".") {
		return false
	}
	code := w.helper(head)
	if code.n == nil {
		return false
	}
	if restAssuredResponses[lastElem(jvmReturnType(code.jf.f, code.n.Parent()))] {
		return true
	}
	body := strings.Join(strings.Fields(code.jf.f.text(code.n)), "")
	body = strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(body, "}"), ";"), ";")
	return strings.HasSuffix(body, ".then()")
}

// restAssuredResponses are the types REST Assured's then() returns.
var restAssuredResponses = set("ValidatableResponse", "ValidatableResponseOptions")

// jvmReturnType is the declared return type of a Java method or a Kotlin function, without its type
// arguments and nullability; "" when it declares none. A Kotlin function's receiver type comes
// before its parameters, and its return type after them.
func jvmReturnType(f *file, decl *sitter.Node) string {
	if decl == nil {
		return ""
	}
	if decl.Kind() == "method_declaration" {
		return typeName(f.field(decl, "type"))
	}
	params := false
	for _, c := range named(decl) {
		switch c.Kind() {
		case "function_value_parameters":
			params = true
		case "user_type", "nullable_type":
			if params {
				return typeName(f.text(c))
			}
		}
	}
	return ""
}

// jvmDrivers are the HTTP test clients, by the package a test file imports, and the calls with which
// a test drives the application through them.
var jvmDrivers = map[string][]string{
	"io.restassured":                               {"given"},
	"org.springframework.test.web.servlet":         {"perform"},
	"org.springframework.test.web.reactive.server": {"exchange"},
}

// noteValueField records a field of a test class whose initializer names something of the product,
// and reports whether it does.
func (jf *jvmFile) noteValueField(name string, init *sitter.Node) bool {
	uses := false
	if name != "" && init != nil {
		walk(init, func(n *sitter.Node) bool {
			uses = uses || (strings.Contains(n.Kind(), "identifier") && jf.names[jf.f.text(n)] != "")
			return !uses
		})
	}
	if uses {
		jf.markValueField(name)
	}
	return uses
}

// markValueField records name as a value field of the file's test classes.
func (jf *jvmFile) markValueField(name string) {
	if jf.valueFields == nil {
		jf.valueFields = map[string]bool{}
	}
	jf.valueFields[name] = true
}

// valueField reports whether name is a value field of the test's class, or one it inherits, and not
// a local of the same name. What the class or the classes it extends declare last decides: an
// override with a literal is no value field, though the class it extends built one of that name.
func (w *jvmWalk) valueField(name string) bool {
	if v, ok := w.values[name]; ok {
		return v && !w.locals[name]
	}
	return w.jf.valueFields[name] && !w.locals[name]
}

// readsValueField reports whether an identifier in a test reads a value field of its class: not a
// local of the same name, and not the member side of a.b.
func (w *jvmWalk) readsValueField(n *sitter.Node) bool {
	if !w.valueField(w.jf.f.text(n)) {
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
