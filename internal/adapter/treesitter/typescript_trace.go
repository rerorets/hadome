//go:build cgo

package treesitter

import (
	"path"
	"regexp"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/rerorets/hadome/internal/facts"
)

// What the functions of TypeScript product code reach, and the entry points that can be retried
// (docs/design/suggest.md, "Conditions of idempotent").

// tsImported is what a name imported at the top of a file stands for.
type tsImported struct {
	io   bool   // imported from a package that does input or output
	file string // the repository's file it comes from; empty for a package
	name string // the name in that file: "default", "*" for the whole module, or the exported name
}

// tsTrace reads one file.
type tsTrace struct {
	r        *typescript
	f        *file
	bindings map[string]tsImported
	top      map[string]*sitter.Node // names declared at the top of the file, and their declarations
	funcs    map[string]bool         // the IDs of the version's functions and methods
}

func (r *typescript) trace(files []*file, t *tables, a *facts.Analysis) {
	funcs := map[string]bool{}
	at := map[string]int{}
	for i, s := range a.Symbols {
		if s.Kind == facts.SymbolFunction || s.Kind == facts.SymbolMethod {
			funcs[s.ID] = true
			at[s.ID] = i
		}
	}
	for _, f := range files {
		if f.test || f.generatedGlob || declarationFile(f.path) {
			continue
		}
		tr := &tsTrace{r: r, f: f, bindings: map[string]tsImported{}, top: map[string]*sitter.Node{}, funcs: funcs}
		tr.bind(t)
		for id, body := range tr.bodies() {
			i, ok := at[id]
			if !ok {
				continue
			}
			got := tr.reach(body)
			s := &a.Symbols[i]
			s.IO, s.Calls, s.Units = got.io, keys(got.calls), keys(got.units)
		}
		a.Entries = append(a.Entries, tr.entries()...)
	}
}

// bind reads the imports and the top-level declarations of the file.
func (tr *tsTrace) bind(t *tables) {
	f := tr.f
	for _, top := range named(f.root) {
		decl := top
		if top.Kind() == "export_statement" {
			if d := top.ChildByFieldName("declaration"); d != nil {
				decl = d
			}
		}
		switch decl.Kind() {
		case "import_statement":
			if !typeOnly(decl) {
				tr.bindImport(decl, t)
			}
		case "function_declaration", "generator_function_declaration", "class_declaration", "abstract_class_declaration":
			tr.top[f.field(decl, "name")] = decl
		case "lexical_declaration", "variable_declaration":
			for _, d := range named(decl) {
				if d.Kind() == "variable_declarator" {
					tr.top[f.field(d, "name")] = d
				}
			}
		}
	}
}

func (tr *tsTrace) bindImport(n *sitter.Node, t *tables) {
	f := tr.f
	spec := unquote(f.field(n, "source"))
	p, kind := tr.r.project.resolve(f.path, spec)
	var b tsImported
	switch kind {
	case tsExternal:
		b.io = t.isIO(spec)
	case tsFound:
		b.file = p
	default:
		return
	}
	add := func(local, name string) {
		b.name = name
		tr.bindings[local] = b
	}
	walk(n, func(x *sitter.Node) bool {
		switch x.Kind() {
		case "import_specifier":
			name, local := f.field(x, "name"), f.field(x, "alias")
			if local == "" {
				local = name
			}
			add(local, name)
		case "namespace_import":
			for _, id := range named(x) {
				add(f.text(id), "*")
			}
		case "import_clause":
			if first := x.NamedChild(0); first != nil && first.Kind() == "identifier" {
				add(f.text(first), "default")
			}
		}
		return true
	})
}

// bodies maps the IDs of the file's functions and methods to their bodies, as symbols lists them.
func (tr *tsTrace) bodies() map[string]*sitter.Node {
	f, r := tr.f, tr.r
	out := map[string]*sitter.Node{}
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

// reach reads what code reaches: global input or output APIs, imported names, and names declared
// at the top of the file. Only the name is read, so a local variable of the same name counts too.
// A value declared at the top of the file (const db = drizzle(pool)) gives what its initial value
// reaches.
func (tr *tsTrace) reach(n *sitter.Node) reached {
	got := newReached()
	tr.reachInto(n, &got, true)
	return got
}

func (tr *tsTrace) reachInto(n *sitter.Node, got *reached, values bool) {
	f := tr.f
	walk(n, func(x *sitter.Node) bool {
		if tsGlobalIO(f, x) != "" {
			got.io = true
		}
		if x.Kind() != "identifier" && x.Kind() != "shorthand_property_identifier" {
			return true
		}
		name := f.text(x)
		if b, ok := tr.bindings[name]; ok {
			tr.imported(x, b, got)
			return true
		}
		d, ok := tr.top[name]
		if !ok {
			return true
		}
		if id := tr.r.symbolID(f.path, name); tr.funcs[id] {
			got.calls[id] = true
		} else if v := d.ChildByFieldName("value"); values && v != nil && d.Kind() == "variable_declarator" && v != n {
			tr.reachInto(v, got, false)
		}
		return true
	})
}

// imported records the use of an imported name: a package that does input or output, a function
// of the repository, or a value of one of its units.
func (tr *tsTrace) imported(x *sitter.Node, b tsImported, got *reached) {
	if b.file == "" {
		got.io = got.io || b.io
		return
	}
	name := b.name
	if name == "*" {
		parent := x.Parent()
		if parent == nil || parent.Kind() != "member_expression" {
			return
		}
		name = tr.f.field(parent, "property")
	}
	if id := tr.r.target(b.file, name); tr.funcs[id] {
		got.calls[id] = true
		return
	}
	got.units[path.Dir(b.file)] = true
}

var (
	tsSvelteServer = regexp.MustCompile(`(^|/)\+server\.[cm]?[jt]s$`)
	tsSveltePage   = regexp.MustCompile(`(^|/)\+(page|layout)\.server\.[cm]?[jt]s$`)
	tsNextRoute    = regexp.MustCompile(`(^|/)app/(.+/)?route\.[cm]?[jt]sx?$`)
	tsNextAPI      = regexp.MustCompile(`(^|/)pages/api/`)
	tsHTTPVerbs    = set("GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "HEAD")
	tsRouteMethods = set("get", "post", "put", "patch", "delete", "all", "use", "on", "schedule", "consume")
	// tsConsumerKeys are the options that hand a consumer its handler: kafkajs's run({ eachMessage })
	// and sqs-consumer's Consumer.create({ handleMessage }).
	tsConsumerKeys = set("eachMessage", "eachBatch", "handleMessage", "handleMessageBatch")
)

// entries lists the entry points of the file: the exports a framework serves by their file's name,
// the exports of a 'use server' file, and the functions handed to a router with a string path.
func (tr *tsTrace) entries() []facts.Entry {
	f := tr.f
	served := func(name string) bool {
		switch {
		case tsSvelteServer.MatchString(f.path):
			return tsHTTPVerbs[name] || name == "fallback"
		case tsSveltePage.MatchString(f.path):
			return name == "load" || name == "actions"
		case tsNextRoute.MatchString(f.path):
			return tsHTTPVerbs[name]
		case tsNextAPI.MatchString(f.path):
			return name == "default"
		}
		return useServer(f)
	}
	var out []facts.Entry
	for _, top := range named(f.root) {
		if top.Kind() == "export_statement" {
			out = append(out, tr.exported(top, served)...)
		}
	}
	walk(f.root, func(x *sitter.Node) bool {
		switch x.Kind() {
		case "call_expression":
			out = append(out, tr.route(x)...)
			out = append(out, tr.consumer(x)...)
		case "new_expression": // BullMQ's new Worker('orders', processor)
			args := arguments(x.ChildByFieldName("arguments"))
			if f.field(x, "constructor") == "Worker" && len(args) >= 2 && args[0].Kind() == "string" {
				out = append(out, tr.handler(args[1])...)
			}
		}
		return true
	})
	return out
}

// exported returns the entries an export statement declares, by the names the framework serves.
func (tr *tsTrace) exported(top *sitter.Node, served func(string) bool) []facts.Entry {
	f := tr.f
	d := top.ChildByFieldName("declaration")
	if d == nil {
		if v := top.ChildByFieldName("value"); v != nil && served("default") { // export default handler
			return tr.handler(v)
		}
		return nil
	}
	isDefault := strings.HasPrefix(f.text(top), "export default")
	var out []facts.Entry
	switch d.Kind() {
	case "function_declaration", "generator_function_declaration":
		name := f.field(d, "name")
		if served(name) || isDefault && served("default") {
			out = append(out, facts.Entry{Path: f.path, Line: line(d), Symbol: tr.r.symbolID(f.path, name)})
		}
	case "lexical_declaration", "variable_declaration":
		for _, v := range named(d) {
			if v.Kind() == "variable_declarator" && served(f.field(v, "name")) {
				if val := v.ChildByFieldName("value"); val != nil {
					out = append(out, tr.declared(f.field(v, "name"), val)...)
				}
			}
		}
	}
	return out
}

// declared returns the entry a served const is: its function when it holds one, or else the code
// of its value (an actions object, a handler made by a call).
func (tr *tsTrace) declared(name string, val *sitter.Node) []facts.Entry {
	if id := tr.r.symbolID(tr.f.path, name); tr.funcs[id] {
		return []facts.Entry{{Path: tr.f.path, Line: line(val), Symbol: id}}
	}
	return []facts.Entry{{Path: tr.f.path, Line: line(val), Calls: keys(tr.reach(val).calls)}}
}

// route returns the entries a router call registers: app.post('/orders', createOrder).
func (tr *tsTrace) route(call *sitter.Node) []facts.Entry {
	fn := call.ChildByFieldName("function")
	if fn == nil || fn.Kind() != "member_expression" || !tsRouteMethods[tr.f.field(fn, "property")] {
		return nil
	}
	args := arguments(call.ChildByFieldName("arguments"))
	if len(args) < 2 || (args[0].Kind() != "string" && args[0].Kind() != "template_string") {
		return nil
	}
	var out []facts.Entry
	for _, a := range args[1:] {
		out = append(out, tr.handler(a)...)
	}
	return out
}

// consumer returns the handlers a queue consumer is given: Bull's queue.process(handler) and the
// handler options of kafkajs and sqs-consumer.
func (tr *tsTrace) consumer(call *sitter.Node) []facts.Entry {
	f := tr.f
	fn := call.ChildByFieldName("function")
	if fn == nil || fn.Kind() != "member_expression" {
		return nil
	}
	var out []facts.Entry
	args := arguments(call.ChildByFieldName("arguments"))
	switch f.field(fn, "property") {
	case "process":
		for _, a := range args {
			out = append(out, tr.handler(a)...)
		}
	case "run", "create":
		for _, a := range args {
			if a.Kind() != "object" {
				continue
			}
			for _, p := range named(a) {
				if p.Kind() == "pair" && tsConsumerKeys[f.field(p, "key")] {
					out = append(out, tr.handler(p.ChildByFieldName("value"))...)
				}
				if p.Kind() == "shorthand_property_identifier" && tsConsumerKeys[f.text(p)] {
					out = append(out, tr.handler(p)...)
				}
			}
		}
	}
	return out
}

// handler returns the entry a handler value is: a function of the repository it names, or the code
// of a function written in place. Anything else is no entry.
func (tr *tsTrace) handler(a *sitter.Node) []facts.Entry {
	switch a.Kind() {
	case "arrow_function", "function_expression", "function":
		return []facts.Entry{{Path: tr.f.path, Line: line(a), Calls: keys(tr.reach(a).calls)}}
	case "identifier", "shorthand_property_identifier":
		got := tr.reach(a)
		var out []facts.Entry
		for _, id := range keys(got.calls) {
			out = append(out, facts.Entry{Path: tr.f.path, Line: line(a), Symbol: id})
		}
		return out
	}
	return nil
}

// useServer reports a file whose first statement is the 'use server' directive: every function it
// exports is a server action, which a form posts to.
func useServer(f *file) bool {
	for _, top := range named(f.root) {
		if top.Kind() == "comment" {
			continue
		}
		if top.Kind() != "expression_statement" {
			return false
		}
		s := strings.Trim(strings.TrimSuffix(strings.TrimSpace(f.text(top)), ";"), `"'`)
		return s == "use server"
	}
	return false
}
