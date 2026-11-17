//go:build cgo

package treesitter

import (
	"strings"

	"github.com/rerorets/hadome/internal/facts"
)

// How the Python reader finds the calls through a value's declared type (Symbol.Through) and the
// classes a class extends or, for a Protocol, matches (Symbol.Supertypes), for the call diagram
// (docs/design/rules-deps.md, "Call facts").

// pyClass is what matching Protocols needs of one class.
type pyClass struct {
	protocol bool
	bases    []string       // the repository's classes it names as bases
	methods  map[string]int // name -> parameters, self and cls aside
}

// pyClasses collects the classes of the version's files.
type pyClasses map[string]*pyClass

// pyTypeName reduces an annotation to the class name it names: Finder, "Finder", Optional[Finder],
// Finder | None and pkg.Finder are all the class Finder names.
func pyTypeName(annotation string) string {
	t := strings.Trim(strings.TrimSpace(annotation), `"'`)
	if inner, ok := strings.CutPrefix(t, "Optional["); ok {
		t = strings.TrimSuffix(inner, "]")
	}
	for _, part := range strings.Split(t, "|") {
		if part = strings.TrimSpace(part); part != "" && part != "None" {
			t = part
			break
		}
	}
	if i := strings.Index(t, "["); i >= 0 {
		t = t[:i]
	}
	return strings.TrimSpace(t)
}

// resolveClass returns the ID of the repository's class a name stands for in a file: one it
// imports, by name or through its module (pkg.Finder), or one its module declares.
func resolveClass(tr *tracing, mod, name string, repo map[string]bool) string {
	if repo[name] { // the class of a value made in another module (madeValues)
		return name
	}
	id := mod + "." + name
	if dot := strings.LastIndex(name, "."); dot >= 0 {
		if b, ok := tr.binds[name[:dot]]; ok && b.id != "" {
			id = tr.memberID(b.id, name[dot+1:])
		}
	} else if b, ok := tr.binds[name]; ok && b.id != "" {
		id = b.id
	}
	if repo[id] {
		return id
	}
	return ""
}

// read records a class: its bases, whether it is a Protocol, and its methods.
func (cs pyClasses) read(tr *tracing, mod string, def *node, repo map[string]bool) {
	f := tr.f
	c := &pyClass{methods: map[string]int{}}
	if list := def.ChildByFieldName("superclasses"); list != nil {
		for _, b := range named(list) {
			name := f.text(b)
			if name == "Protocol" || strings.HasSuffix(name, ".Protocol") || strings.HasPrefix(name, "Protocol[") {
				c.protocol = true
			} else if id := resolveClass(tr, mod, pyTypeName(name), repo); id != "" {
				c.bases = append(c.bases, id)
			}
		}
	}
	for _, m := range named(def.ChildByFieldName("body")) {
		if md := definition(m); md.Kind() == "function_definition" {
			params := 0
			for i, p := range named(md.ChildByFieldName("parameters")) {
				if i > 0 || (p.Kind() != "identifier" || (f.text(p) != "self" && f.text(p) != "cls")) {
					params++
				}
			}
			c.methods[f.field(md, "name")] = params
		}
	}
	cs[mod+"."+f.field(def, "name")] = c
}

// supertypes returns, by class, its bases and the Protocols it matches by method names and numbers
// of parameters. Annotations are not compared: parameters may have none.
func (cs pyClasses) supertypes() supers {
	out := supers{}
	for id, c := range cs {
		out[id] = append(out[id], c.bases...)
		if c.protocol {
			continue
		}
		have := cs.methodsOf(id, map[string]bool{})
		for pid, p := range cs {
			if p.protocol && pid != id && len(p.methods) > 0 && matches(have, p.methods) {
				out[id] = append(out[id], pid)
			}
		}
	}
	return out
}

// methodsOf returns a class's methods with those of the repository's classes it extends.
func (cs pyClasses) methodsOf(id string, seen map[string]bool) map[string]int {
	c := cs[id]
	if c == nil || seen[id] {
		return nil
	}
	seen[id] = true
	out := map[string]int{}
	for _, b := range c.bases {
		for name, n := range cs.methodsOf(b, seen) {
			out[name] = n
		}
	}
	for name, n := range c.methods {
		out[name] = n
	}
	return out
}

func matches(have, want map[string]int) bool {
	for name, n := range want {
		if got, ok := have[name]; !ok || got != n {
			return false
		}
	}
	return true
}

// pyModule is what reading the functions of one Python module needs: its tracing, its name, the IDs
// of the version's classes, and its values at the top (pyModuleValues).
type pyModule struct {
	tr     *tracing
	mod    string
	repo   map[string]bool
	values map[string]string
}

// unknownType stands, in the maps of written types below, for a value from outside whose type is
// not written: a call on it is one hadome cannot follow (Symbol.Unfollowed).
const unknownType = "?"

// pyThrough returns the methods a function's body calls on values whose annotation names one of
// the repository's classes: its parameters, the attributes of its class (pyAttributes), and the
// variables at the top of its module (pyModuleValues). It also counts the calls on such values
// whose type is not written.
func pyThrough(m pyModule, fn tfunc, classDef *node) (callLines, int) {
	tr, mod, repo, module := m.tr, m.mod, m.repo, m.values
	f := tr.f
	attrs, vars := pyAttributes(f, classDef), map[string]string{}
	pyAnnotated(f, definition(fn.decl), vars)
	pyUntyped(f, definition(fn.decl), vars)
	locals := pyLocals(f, fn.body)
	out, unfollowed := callLines{}, 0
	walk(fn.body, func(n *node) bool {
		fun := n.ChildByFieldName("function")
		if n.Kind() != "call" || fun == nil || fun.Kind() != "attribute" {
			return true
		}
		obj, method := fun.ChildByFieldName("object"), f.field(fun, "attribute")
		written := ""
		switch {
		case obj.Kind() == "identifier" && vars[f.text(obj)] != "":
			written = vars[f.text(obj)]
		case obj.Kind() == "identifier" && !locals[f.text(obj)]:
			written = module[f.text(obj)]
		case obj.Kind() == "attribute" && f.field(obj, "object") == "self":
			written = attrs[f.field(obj, "attribute")]
		}
		if written == unknownType {
			unfollowed++
		} else if id := resolveClass(tr, mod, pyTypeName(written), repo); written != "" && id != "" {
			out.add(id+"."+method, line(n))
		}
		return true
	})
	for id, at := range madeInPlace(f, fn.body, pyMade, func(w string) string { return resolveClass(tr, mod, w, repo) }) {
		out.add(id, at)
	}
	return out, unfollowed
}

// pyLocals returns the names a function's body assigns, which hide the module's names there.
func pyLocals(f *file, body *node) map[string]bool {
	out := map[string]bool{}
	walk(body, func(n *node) bool {
		if left := n.ChildByFieldName("left"); n.Kind() == "assignment" && left != nil && left.Kind() == "identifier" {
			out[f.text(left)] = true
		}
		return true
	})
	return out
}

// pyModuleValues returns, by name, the written types of the values at the top of a module that come
// from the repository's code: one of its classes made there (repo = Repo()) or in the module it is
// imported from, or unknownType for the result of one of its functions (repo = make_repo()) and
// for another value imported from one of its modules that is neither a function, a class nor a
// module.
func pyModuleValues(tr *tracing, mod string, repo map[string]bool, made map[string]string) map[string]string {
	out := pyImportedValues(tr, repo, made)
	for _, n := range named(tr.f.root) {
		if name, written := pyModuleValue(tr, mod, repo, n); name != "" {
			out[name] = written
		}
	}
	return out
}

// pyImportedValues returns, for each name imported from one of the repository's modules that is
// neither a function, a class nor a module, the class it was made from there (madeValues), or
// unknownType.
func pyImportedValues(tr *tracing, repo map[string]bool, made map[string]string) map[string]string {
	out := map[string]string{}
	for name, b := range tr.binds {
		if b.id != "" && strings.Contains(b.id[strings.LastIndex(b.id, "/")+1:], ".") && !tr.funcs[b.id] && !repo[b.id] {
			out[name] = unknownType
			if class := made[b.id]; class != "" {
				out[name] = class
			}
		}
	}
	return out
}

// pyModuleValue returns the name and written type a statement at the top of a module gives a value
// of the repository's code, or "".
func pyModuleValue(tr *tracing, mod string, repo map[string]bool, n *node) (string, string) {
	f := tr.f
	as := n.NamedChild(0)
	if n.Kind() != "expression_statement" || as == nil || as.Kind() != "assignment" || f.field(as, "type") != "" {
		return "", ""
	}
	left, right := as.ChildByFieldName("left"), as.ChildByFieldName("right")
	if left == nil || left.Kind() != "identifier" || right == nil {
		return "", ""
	}
	made := pyMade.made(f, right)
	switch {
	case made != "" && resolveClass(tr, mod, made, repo) != "":
		return f.text(left), made
	case made != "" && pyRepoCallee(tr, mod, made):
		return f.text(left), unknownType
	}
	return "", ""
}

// pyRepoCallee reports whether a callee, as written, names one of the repository's functions: one
// the module declares or imports, or a function of a module it imports.
func pyRepoCallee(tr *tracing, mod, callee string) bool {
	if tr.funcs[mod+"."+callee] {
		return true
	}
	root, rest, dotted := strings.Cut(callee, ".")
	b, ok := tr.binds[root]
	if !ok || b.id == "" {
		return false
	}
	if dotted {
		return tr.funcs[tr.memberID(b.id, rest)]
	}
	return tr.funcs[b.id]
}

// pyAnnotated records the annotations of a function's annotated parameters, by name.
func pyAnnotated(f *file, def *node, into map[string]string) {
	for _, p := range named(def.ChildByFieldName("parameters")) {
		if p.Kind() == "typed_parameter" || p.Kind() == "typed_default_parameter" {
			name := f.field(p, "name")
			if name == "" && p.NamedChildCount() > 0 {
				name = f.text(p.NamedChild(0))
			}
			into[name] = f.field(p, "type")
		}
	}
}

// pyUntyped records a function's parameters without an annotation as unknownType, self and cls
// aside.
func pyUntyped(f *file, def *node, into map[string]string) {
	for i, p := range named(def.ChildByFieldName("parameters")) {
		name := ""
		switch p.Kind() {
		case "identifier":
			name = f.text(p)
		case "default_parameter":
			name = f.field(p, "name")
		}
		if name != "" && !(i == 0 && (name == "self" || name == "cls")) {
			into[name] = unknownType
		}
	}
}

// pyAttributes returns the annotations of a class's attributes, by name: those the class body
// annotates (finder: Finder), and those __init__ annotates, sets from an annotated parameter
// (self.finder = finder) or sets to an object made in place (self.repo = Repo()), whose callee
// stands for its class. One set from a parameter without an annotation is unknownType.
func pyAttributes(f *file, classDef *node) map[string]string {
	attrs := map[string]string{}
	if classDef == nil {
		return attrs
	}
	for _, m := range named(classDef.ChildByFieldName("body")) {
		md := definition(m)
		switch {
		case md.Kind() == "expression_statement" && md.NamedChild(0) != nil && md.NamedChild(0).Kind() == "assignment":
			if as := md.NamedChild(0); f.field(as, "type") != "" {
				attrs[f.field(as, "left")] = f.field(as, "type")
			}
		case md.Kind() == "function_definition" && f.field(md, "name") == "__init__":
			params := map[string]string{}
			pyAnnotated(f, md, params)
			pyUntyped(f, md, params)
			walk(md.ChildByFieldName("body"), func(n *node) bool {
				if n.Kind() != "assignment" {
					return true
				}
				left := n.ChildByFieldName("left")
				if left == nil || left.Kind() != "attribute" || f.field(left, "object") != "self" {
					return true
				}
				if t := f.field(n, "type"); t != "" {
					attrs[f.field(left, "attribute")] = t
				} else if t, ok := params[f.field(n, "right")]; ok {
					attrs[f.field(left, "attribute")] = t
				} else if made := pyMade.made(f, n.ChildByFieldName("right")); made != "" { // self.repo = Repo()
					attrs[f.field(left, "attribute")] = made
				}
				return true
			})
		}
	}
	return attrs
}

// pyRepoTypes returns the IDs of the version's classes.
func pyRepoTypes(a *facts.Analysis) map[string]bool { return tsRepoTypes(a) }
