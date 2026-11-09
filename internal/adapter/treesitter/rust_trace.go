//go:build cgo

package treesitter

import (
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/rerorets/hadome/internal/facts"
)

// What Rust product code reaches, and its entry points (docs/design/suggest.md).

// rustRouteAttributes serve a function for a path (actix-web, Rocket); rustRouteCalls hand a
// handler to axum's router (route("/orders", post(create))).
var (
	rustRouteAttributes = set("get", "post", "put", "patch", "delete", "head", "route")
	rustRouteCalls      = set("get", "post", "put", "patch", "delete", "head", "any", "on")
)

func (r *rust) traceSyntax() tsyntax {
	return tsyntax{
		idents: set("identifier"),
		selves: set("self"),
		funcs:  set("function_item"),
		member: func(f *file, n *sitter.Node) (*sitter.Node, *sitter.Node) {
			if n.Kind() != "field_expression" {
				return nil, nil
			}
			return n.ChildByFieldName("value"), n.ChildByFieldName("field")
		},
		built: func(f *file, n *sitter.Node) string {
			switch n.Kind() {
			case "struct_expression":
				return simpleType(f.field(n, "name"))
			case "call_expression": // Store::new(…)
				if fn := n.ChildByFieldName("function"); fn != nil && fn.Kind() == "scoped_identifier" {
					return simpleType(f.field(fn, "path"))
				}
			}
			return ""
		},
		vars: func(f *file, n *sitter.Node) map[string]string {
			switch n.Kind() {
			case "parameter", "let_declaration":
				if t := f.field(n, "type"); t != "" {
					return map[string]string{f.field(n, "pattern"): t}
				}
			case "field_declaration":
				return map[string]string{f.field(n, "name"): f.field(n, "type")}
			}
			return nil
		},
		special: r.path,
	}
}

// path reads a::b::f: Self::f, a name a use declaration binds followed by a member, or a whole
// path of the repository or of a crate that does input or output.
func (r *rust) path(tr *tracing, fn tfunc, n *sitter.Node, got *reached) bool {
	if n.Kind() != "scoped_identifier" {
		return false
	}
	f := tr.f
	segs := strings.Split(strings.ReplaceAll(f.text(n), " ", ""), "::") // a scoped identifier has two or more
	last := segs[len(segs)-1]
	if segs[0] == "Self" && fn.class != "" {
		if tr.funcs[fn.class+"."+last] {
			got.calls[fn.class+"."+last] = true
		}
		return true
	}
	if b, ok := tr.binds[segs[0]]; ok {
		id := b.id + "." + strings.Join(segs[1:], ".")
		switch {
		case b.io:
			got.io = true
		case tr.funcs[id]:
			got.calls[id] = true
		case b.unit != "":
			got.units[b.unit] = true
		}
		return true
	}
	if own := fn.own + "." + strings.Join(segs, "."); tr.funcs[own] { // Type::f of the same module
		got.calls[own] = true
		return true
	}
	loc, internal := r.resolve(f.path, segs)
	switch {
	case !internal:
		got.io = got.io || tr.ioPath(strings.Join(segs, "::"))
	case tr.funcs[loc+"."+last]:
		got.calls[loc+"."+last] = true
	default:
		if t := loc + "." + strings.Join(segs[len(segs)-2:], "."); tr.funcs[t] { // module::Type::f
			got.calls[t] = true
		} else {
			got.units[loc] = true
		}
	}
	return true
}

// ioPath reports whether a path names something of a crate that does input or output.
func (tr *tracing) ioPath(p string) bool { return tr.io != nil && tr.io(p) }

func (r *rust) trace(files []*file, t *tables, a *facts.Analysis) {
	syn := r.traceSyntax()
	traceFiles(files, a, func(f *file, funcs map[string]bool) ([]tfunc, *tracing, func() []facts.Entry) {
		binds := r.traceBinds(f, t)
		tr := &tracing{f: f, syn: syn, binds: binds, values: map[string]*sitter.Node{}, funcs: funcs, io: t.isIO,
			types: func(name string) tbind {
				if b, ok := binds[simpleType(name)]; ok {
					return b
				}
				return tbind{id: f.unit + "." + simpleType(name), unit: f.unit}
			}}
		structs := map[string]*sitter.Node{}
		var fns []tfunc
		var collect func(body *sitter.Node, prefix string)
		collect = func(body *sitter.Node, prefix string) {
			for _, c := range named(body) {
				switch {
				case c.Kind() == "function_item":
					fns = append(fns, tfunc{id: prefix + "." + f.field(c, "name"), own: prefix, decl: c, body: c.ChildByFieldName("body")})
				case rustTypes[c.Kind()]:
					structs[prefix+"."+f.field(c, "name")] = c
				case c.Kind() == "impl_item" || c.Kind() == "trait_item":
					self := f.field(c, "name")
					if c.Kind() == "impl_item" {
						self = strings.SplitN(f.field(c, "type"), "<", 2)[0]
					}
					class := prefix + "." + self
					for _, m := range named(c.ChildByFieldName("body")) {
						if m.Kind() == "function_item" {
							fns = append(fns, tfunc{id: class + "." + f.field(m, "name"), class: class, own: prefix, decl: m,
								body: m.ChildByFieldName("body")})
						}
					}
				case c.Kind() == "mod_item" && c.ChildByFieldName("body") != nil && !testModule(f, c):
					collect(c.ChildByFieldName("body"), prefix+"/"+f.field(c, "name"))
				case c.Kind() == "const_item" || c.Kind() == "static_item":
					tr.values[f.field(c, "name")] = c.ChildByFieldName("value")
				}
			}
		}
		collect(f.root, f.unit)
		for i := range fns {
			if fns[i].class != "" {
				fns[i].scope = structs[fns[i].class]
			}
		}
		return fns, tr, func() []facts.Entry { return r.entries(tr, fns) }
	})
}

// traceBinds binds the names the file's use declarations bring in: a module or item of the
// repository, or something of a crate that does input or output.
func (r *rust) traceBinds(f *file, t *tables) map[string]tbind {
	out := map[string]tbind{}
	for _, u := range uses(f, f.root) {
		if u.local == "" || u.local == "self" {
			continue
		}
		loc, internal := r.resolve(f.path, u.segs)
		last := u.segs[len(u.segs)-1]
		switch {
		case !internal:
			out[u.local] = tbind{io: t.isIO(strings.Join(u.segs, "::"))}
		case strings.HasSuffix(loc, "/"+last) || last == "crate" || last == "super":
			out[u.local] = tbind{id: loc, unit: loc}
		default:
			out[u.local] = tbind{id: loc + "." + last, unit: loc}
		}
	}
	return out
}

// entries lists the functions an attribute serves (#[post("/orders")]) and the handlers given to a
// router: axum's route("/orders", post(create)), actix-web's .to(create).
func (r *rust) entries(tr *tracing, fns []tfunc) []facts.Entry {
	f := tr.f
	var out []facts.Entry
	for _, fn := range fns {
		for _, a := range attributes(f, fn.decl) {
			name := a
			if i := strings.IndexAny(name, "( "); i >= 0 {
				name = name[:i]
			}
			if rustRouteAttributes[name[strings.LastIndex(name, ":")+1:]] {
				out = append(out, facts.Entry{Path: f.path, Line: line(fn.decl), Symbol: fn.id})
				break
			}
		}
	}
	inline := set("closure_expression")
	top := tfunc{own: f.unit}
	walk(f.root, func(n *sitter.Node) bool {
		if n.Kind() != "call_expression" {
			return true
		}
		callee := n.ChildByFieldName("function")
		args := arguments(n.ChildByFieldName("arguments"))
		if callee == nil || callee.Kind() != "field_expression" {
			return true
		}
		ctx := enclosing(fns, n, top)
		switch f.field(callee, "field") {
		case "route":
			if len(args) == 2 && args[0].Kind() == "string_literal" {
				walk(args[1], func(c *sitter.Node) bool {
					if c.Kind() != "call_expression" {
						return true
					}
					name := f.field(c, "function")
					if rustRouteCalls[name[strings.LastIndexAny(name, ".:")+1:]] {
						for _, h := range arguments(c.ChildByFieldName("arguments")) {
							out = append(out, tr.handlers(ctx, h, inline)...)
						}
					}
					return true
				})
			}
		case "to":
			if len(args) == 1 {
				out = append(out, tr.handlers(ctx, args[0], inline)...)
			}
		}
		return true
	})
	return out
}
