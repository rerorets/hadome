//go:build cgo

package treesitter

import (
	"strings"
	"unicode"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/rerorets/hadome/internal/facts"
)

// What Java and Kotlin product code reaches, and its entry points (docs/design/suggest.md).

// jvmEntryAnnotations mark the methods that serve a request or run as a consumer or a scheduled
// job: Spring MVC, JAX-RS, Spring's message listeners and scheduler, Quarkus and MicroProfile.
var jvmEntryAnnotations = set("GetMapping", "PostMapping", "PutMapping", "PatchMapping", "DeleteMapping", "RequestMapping",
	"GET", "POST", "PUT", "PATCH", "DELETE", "KafkaListener", "RabbitListener", "JmsListener", "SqsListener",
	"Scheduled", "Incoming")

// traceBinds binds the classes of the file's package and what it imports: a class of the
// repository, a static import of one of its members, or a class of a package that does input or
// output.
func (j *jvm) traceBinds(f *file, t *tables, imports func(visit func(name string, wildcard bool, n *sitter.Node))) map[string]tbind {
	out := map[string]tbind{}
	for c := range j.classes[f.unit] {
		out[c] = tbind{id: f.unit + "." + c, unit: f.unit}
	}
	imports(func(name string, wildcard bool, _ *sitter.Node) {
		pkg, class, internal := j.resolve(name, wildcard)
		switch {
		case !internal && !wildcard:
			out[lastElem(name)] = tbind{io: t.isIO(name)}
		case !internal:
		case wildcard && class == "":
			for c := range j.classes[pkg] {
				out[c] = tbind{id: pkg + "." + c, unit: pkg}
			}
		case class != "":
			rest := name[len(pkg)+1:]
			out[lastElem(rest)] = tbind{id: pkg + "." + rest, unit: pkg}
		}
	})
	return out
}

func newJVMTracing(f *file, syn tsyntax, binds map[string]tbind, funcs map[string]bool) *tracing {
	return &tracing{f: f, syn: syn, binds: binds, values: map[string]*sitter.Node{}, funcs: funcs,
		types: func(name string) tbind { return binds[simpleType(name)] }}
}

var javaSyntax = tsyntax{
	idents: set("identifier"),
	selves: set("this"),
	funcs:  set("method_declaration", "constructor_declaration"),
	member: func(f *file, n *sitter.Node) (*sitter.Node, *sitter.Node) {
		switch n.Kind() {
		case "method_invocation":
			if obj := n.ChildByFieldName("object"); obj != nil {
				return obj, n.ChildByFieldName("name")
			}
		case "field_access":
			return n.ChildByFieldName("object"), n.ChildByFieldName("field")
		}
		return nil, nil
	},
	built: func(f *file, n *sitter.Node) string {
		if n.Kind() == "object_creation_expression" {
			return simpleType(f.field(n, "type"))
		}
		return ""
	},
	vars: func(f *file, n *sitter.Node) map[string]string {
		switch n.Kind() {
		case "formal_parameter":
			return map[string]string{f.field(n, "name"): f.field(n, "type")}
		case "field_declaration", "local_variable_declaration":
			out := map[string]string{}
			for _, d := range named(n) {
				if d.Kind() == "variable_declarator" {
					out[f.field(d, "name")] = f.field(n, "type")
				}
			}
			return out
		}
		return nil
	},
}

func (j *java) trace(files []*file, t *tables, a *facts.Analysis) {
	traceFiles(files, a, func(f *file, funcs map[string]bool) ([]tfunc, *tracing, func() []facts.Entry) {
		tr := newJVMTracing(f, javaSyntax, j.traceBinds(f, t, func(v func(string, bool, *sitter.Node)) { javaImports(f, v) }), funcs)
		var fns []tfunc
		var collect func(n *sitter.Node, prefix string)
		collect = func(n *sitter.Node, prefix string) {
			class := prefix + "." + f.field(n, "name")
			body := n.ChildByFieldName("body")
			for _, m := range named(body) {
				switch {
				case javaTypes[m.Kind()]:
					collect(m, class)
				case m.Kind() == "method_declaration" || m.Kind() == "constructor_declaration":
					fns = append(fns, tfunc{id: class + "." + f.field(m, "name"), class: class, own: class, decl: m,
						body: m.ChildByFieldName("body"), scope: body})
				}
			}
		}
		for _, n := range named(f.root) {
			if javaTypes[n.Kind()] {
				collect(n, f.unit)
			}
		}
		return fns, tr, func() []facts.Entry {
			var out []facts.Entry
			for _, fn := range fns {
				for name := range annotations(f, fn.decl) {
					if jvmEntryAnnotations[lastElem(name)] {
						out = append(out, facts.Entry{Path: f.path, Line: line(fn.decl), Symbol: fn.id})
						break
					}
				}
			}
			return out
		}
	})
}

var kotlinSyntax = tsyntax{
	idents: set("identifier"),
	selves: set("this_expression", "this"),
	funcs:  set("function_declaration"),
	member: func(f *file, n *sitter.Node) (*sitter.Node, *sitter.Node) {
		if n.Kind() != "navigation_expression" || n.NamedChildCount() < 2 {
			return nil, nil
		}
		name := n.NamedChild(n.NamedChildCount() - 1)
		if name.Kind() != "identifier" {
			return nil, nil
		}
		return n.NamedChild(0), name
	},
	built: func(f *file, n *sitter.Node) string {
		if n.Kind() != "call_expression" || n.NamedChildCount() == 0 {
			return ""
		}
		callee := n.NamedChild(0)
		if name := f.text(callee); callee.Kind() == "identifier" && name != "" && unicode.IsUpper(rune(name[0])) {
			return name
		}
		return ""
	},
	vars: func(f *file, n *sitter.Node) map[string]string {
		switch n.Kind() {
		case "class_parameter", "parameter", "variable_declaration":
			name, typ := "", ""
			for _, c := range named(n) {
				switch c.Kind() {
				case "identifier":
					if name == "" {
						name = f.text(c)
					}
				case "user_type", "nullable_type":
					typ = f.text(c)
				}
			}
			if name != "" {
				return map[string]string{name: typ}
			}
		}
		return nil
	},
}

// ktorRoutes are the calls of Ktor's routing that serve the lambda given to them.
var ktorRoutes = set("get", "post", "put", "patch", "delete", "head", "options")

func (k *kotlin) trace(files []*file, t *tables, a *facts.Analysis) {
	traceFiles(files, a, func(f *file, funcs map[string]bool) ([]tfunc, *tracing, func() []facts.Entry) {
		tr := newJVMTracing(f, kotlinSyntax, k.traceBinds(f, t, func(v func(string, bool, *sitter.Node)) { ktImports(f, v) }), funcs)
		var fns []tfunc
		var collect func(n *sitter.Node, prefix string)
		collect = func(n *sitter.Node, prefix string) {
			class := prefix + "." + f.field(n, "name")
			for _, m := range ktMembers(n) {
				switch {
				case ktTypes[m.Kind()]:
					collect(m, class)
				case m.Kind() == "function_declaration":
					fns = append(fns, tfunc{id: class + "." + f.field(m, "name"), class: class, own: f.unit, decl: m,
						body: ktBody(m), scope: n})
				}
			}
		}
		for _, n := range named(f.root) {
			switch {
			case ktTypes[n.Kind()]:
				collect(n, f.unit)
			case n.Kind() == "function_declaration":
				fns = append(fns, tfunc{id: f.unit + "." + f.field(n, "name"), own: f.unit, decl: n, body: ktBody(n)})
			case n.Kind() == "property_declaration":
				for _, c := range named(n) {
					if c.Kind() == "variable_declaration" {
						if v := n.NamedChild(n.NamedChildCount() - 1); v.Id() != c.Id() {
							tr.values[strings.TrimSpace(f.text(c.NamedChild(0)))] = v
						}
					}
				}
			}
		}
		return fns, tr, func() []facts.Entry { return k.entries(tr, fns) }
	})
}

// entries lists the functions an annotation serves or runs, and the lambdas Ktor's routing serves:
// post("/orders") { … }.
func (k *kotlin) entries(tr *tracing, fns []tfunc) []facts.Entry {
	f := tr.f
	var out []facts.Entry
	for _, fn := range fns {
		anns := ktAnnotations(f, fn.decl)
		leading, _ := ktLeading(f, fn.decl)
		for name := range leading {
			anns[name] = nil
		}
		for name := range anns {
			if jvmEntryAnnotations[name] {
				out = append(out, facts.Entry{Path: f.path, Line: line(fn.decl), Symbol: fn.id})
				break
			}
		}
	}
	top := tfunc{own: f.unit}
	walk(f.root, func(n *sitter.Node) bool {
		if n.Kind() != "call_expression" || n.NamedChildCount() < 2 {
			return true
		}
		lambda, callee := n.NamedChild(n.NamedChildCount()-1), n.NamedChild(0)
		path := true
		if callee.Kind() == "call_expression" && callee.NamedChildCount() == 2 { // post("/orders") { … }
			if args := named(callee.NamedChild(1)); len(args) > 0 {
				path = strings.HasPrefix(strings.TrimSpace(f.text(args[0])), `"`)
			}
			callee = callee.NamedChild(0)
		}
		if lambda.Kind() == "annotated_lambda" && ktorRoutes[f.text(callee)] && path {
			out = append(out, facts.Entry{Path: f.path, Line: line(n), Calls: keys(tr.reach(enclosing(fns, n, top), lambda).calls)})
		}
		return true
	})
	return out
}
