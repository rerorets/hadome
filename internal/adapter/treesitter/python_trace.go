//go:build cgo

package treesitter

import (
	"strings"

	"github.com/rerorets/hadome/internal/facts"
)

// What Python product code reaches, and its entry points (docs/design/suggest.md).

var pySyntax = tsyntax{
	idents: set("identifier"),
	selves: set("self", "cls"),
	funcs:  set("function_definition"),
	built: func(f *file, n *node) string {
		if fn := n.ChildByFieldName("function"); n.Kind() == "call" && fn != nil && fn.Kind() == "identifier" {
			return f.text(fn)
		}
		return ""
	},
	member: func(f *file, n *node) (*node, *node) {
		if n.Kind() != "attribute" {
			return nil, nil
		}
		return n.ChildByFieldName("object"), n.ChildByFieldName("attribute")
	},
}

func (r *python) trace(files []*file, t *tables, a *facts.Analysis) {
	traceFiles(files, a, func(f *file, funcs map[string]bool) ([]tfunc, *tracing, func() []facts.Entry) {
		tr := &tracing{f: f, syn: pySyntax, binds: r.traceBinds(f, t), values: map[string]*node{}, funcs: funcs,
			types: func(string) tbind { return tbind{} }}
		mod := module(f.path)
		var fns []tfunc
		for _, n := range named(f.root) {
			def := definition(n)
			switch def.Kind() {
			case "function_definition":
				fns = append(fns, tfunc{id: mod + "." + f.field(def, "name"), own: mod, decl: n, body: def.ChildByFieldName("body")})
			case "class_definition":
				class := mod + "." + f.field(def, "name")
				for _, m := range named(def.ChildByFieldName("body")) {
					if md := definition(m); md.Kind() == "function_definition" {
						fns = append(fns, tfunc{id: class + "." + f.field(md, "name"), class: class, own: mod, decl: m,
							body: md.ChildByFieldName("body")})
					}
				}
			case "expression_statement":
				if as := def.NamedChild(0); as != nil && as.Kind() == "assignment" && as.ChildByFieldName("right") != nil {
					tr.values[f.field(as, "left")] = as.ChildByFieldName("right")
				}
			}
		}
		return fns, tr, func() []facts.Entry { return r.entries(tr, fns, mod) }
	})
}

// traceBinds binds what the file imports: a package that does input or output, or a function,
// class or module of the repository.
func (r *python) traceBinds(f *file, t *tables) map[string]tbind {
	out := map[string]tbind{}
	for _, im := range r.parseImports(f) {
		for local, name := range im.names {
			switch {
			case !im.internal:
				out[local] = tbind{io: t.isIO(im.dotted)}
			case name == "":
				out[local] = tbind{id: im.mod, unit: im.unit}
			case r.exists[im.mod+"/"+name+".py"]: // from app import store, a module
				out[local] = tbind{id: im.mod + "/" + name, unit: im.mod}
			default:
				out[local] = tbind{id: im.mod + "." + name, unit: im.unit}
			}
		}
	}
	return out
}

// pyRouteDecorators are the decorators that serve a function for a path (Flask, FastAPI and the
// like), pyTaskDecorators those that make it a task a queue runs or a scheduler starts (Celery,
// APScheduler, Django REST framework's api_view).
var (
	pyRouteDecorators = set("route", "get", "post", "put", "patch", "delete", "api_route", "websocket")
	pyTaskDecorators  = set("task", "shared_task", "periodic_task", "scheduled_job", "api_view", "actor")
	pyViewMethods     = set("get", "post", "put", "patch", "delete", "create", "update", "partial_update", "destroy", "list", "retrieve")
)

// entries lists the functions the file's decorators serve or run, and the views its URL
// configuration routes to: path('orders/', views.create_order), OrderView.as_view(), and the
// view sets a router registers.
func (r *python) entries(tr *tracing, fns []tfunc, mod string) []facts.Entry {
	f := tr.f
	var out []facts.Entry
	for _, fn := range fns {
		if fn.decl.Kind() == "decorated_definition" && pyEntryDecorated(f, fn.decl) {
			out = append(out, facts.Entry{Path: f.path, Line: line(fn.decl), Symbol: fn.id})
		}
	}
	top := tfunc{own: mod}
	walk(f.root, func(n *node) bool {
		if n.Kind() != "call" {
			return true
		}
		callee := f.field(n, "function")
		args := arguments(n.ChildByFieldName("arguments"))
		if len(args) < 2 || args[0].Kind() != "string" {
			return true
		}
		switch lastElem(callee) {
		case "path", "re_path", "url":
			out = append(out, r.view(tr, top, args[1])...)
		case "register":
			out = append(out, r.viewClass(tr, top, args[1])...)
		}
		return true
	})
	return out
}

// pyEntryDecorated reports a definition with a decorator that serves it for a path or runs it as a
// task: @app.post("/orders"), @router.get("/"), @shared_task, @app.task.
func pyEntryDecorated(f *file, decl *node) bool {
	for _, d := range named(decl) {
		if d.Kind() != "decorator" || d.NamedChildCount() == 0 {
			continue
		}
		e := d.NamedChild(0)
		var args []*node
		if e.Kind() == "call" {
			args = arguments(e.ChildByFieldName("arguments"))
			e = e.ChildByFieldName("function")
		}
		name := lastElem(f.text(e))
		if pyTaskDecorators[name] || pyRouteDecorators[name] && e.Kind() == "attribute" && len(args) > 0 && args[0].Kind() == "string" {
			return true
		}
	}
	return false
}

// view returns the entries a URL pattern routes to: a view function, or a class-based view's
// methods for OrderView.as_view().
func (r *python) view(tr *tracing, top tfunc, a *node) []facts.Entry {
	if a.Kind() == "call" {
		if fn := a.ChildByFieldName("function"); fn != nil && fn.Kind() == "attribute" && tr.f.field(fn, "attribute") == "as_view" {
			return r.viewClass(tr, top, fn.ChildByFieldName("object"))
		}
	}
	return tr.handlers(top, a, nil)
}

// viewClass returns the request handling methods of the class a name stands for.
func (r *python) viewClass(tr *tracing, top tfunc, a *node) []facts.Entry {
	class := ""
	text := tr.f.text(a)
	if b, ok := tr.binds[text]; ok {
		class = b.id
	} else if obj, name := pySyntax.member(tr.f, a); obj != nil {
		if b, ok := tr.binds[tr.f.text(obj)]; ok {
			class = b.id + "." + tr.f.text(name)
		}
	} else if !strings.Contains(text, ".") {
		class = top.own + "." + text
	}
	var out []facts.Entry
	for _, m := range keys(pyViewMethods) {
		if id := class + "." + m; class != "" && tr.funcs[id] {
			out = append(out, facts.Entry{Path: tr.f.path, Line: line(a), Symbol: id})
		}
	}
	return out
}
