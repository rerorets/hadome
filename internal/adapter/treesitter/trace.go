//go:build cgo

package treesitter

import (
	"sort"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/rerorets/hadome/internal/facts"
)

// What the functions of product code reach, and the entry points that can be retried
// (docs/design/suggest.md, "Conditions of idempotent"). TypeScript has its own reading
// (typescript_trace.go); Python, Java, Kotlin and Rust share this one.

// tracer is a reader that also fills in Symbol.Calls, Units and IO and lists Analysis.Entries.
type tracer interface {
	trace(files []*file, t *tables, a *facts.Analysis)
}

// reached is what a piece of code reaches.
type reached struct {
	io    bool
	calls map[string]bool
	units map[string]bool
}

func newReached() reached { return reached{calls: map[string]bool{}, units: map[string]bool{}} }

// keys returns the keys set to true, sorted.
func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		if v {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// tbind is what a name stands for in a file of product code.
type tbind struct {
	io   bool   // a package that does input or output
	id   string // the repository's function, class or module it names; a member of it is <id>.<name>
	unit string // the repository's unit it belongs to
}

// tfunc is a function of a file, to be traced.
type tfunc struct {
	id    string
	class string       // the ID of its class or impl, whose members self and this name; "" outside one
	own   string       // the prefix of the names its file declares at the top: <own>.<name>
	decl  *sitter.Node // the declaration: its parameters and annotations
	body  *sitter.Node
	scope *sitter.Node // the class or impl body whose fields it sees, or nil
}

// tsyntax is how a language writes what tracing reads.
type tsyntax struct {
	idents map[string]bool // kinds of identifier nodes
	selves map[string]bool // the names, or node kinds, of the object itself: self, this
	funcs  map[string]bool // kinds of function declarations
	// member returns the object and the member's name node of a member access (a.b), or nil.
	member func(f *file, n *sitter.Node) (*sitter.Node, *sitter.Node)
	// built returns the class name a new object is made of (Store() in Python, new Store() in
	// Java), or "".
	built func(f *file, n *sitter.Node) string
	// vars returns the variables a declaration types, by name: a parameter, a field, a local.
	vars func(f *file, n *sitter.Node) map[string]string
	// special reads a node the generic reading does not, such as Rust's a::b::f; it reports whether
	// it did.
	special func(tr *tracing, fn tfunc, n *sitter.Node, got *reached) bool
}

// tracing reads the functions of one file.
type tracing struct {
	f      *file
	syn    tsyntax
	binds  map[string]tbind        // names the file binds: imports, and its package's classes
	values map[string]*sitter.Node // values declared at the top of the file, by name: their initial values
	funcs  map[string]bool         // the IDs of the version's functions and methods
	types  func(name string) tbind // what a type name stands for in the file
	io     func(path string) bool  // whether a whole path names something that does input or output
	locals map[string]string       // the variables of the function being read, and their types
	fields map[string]string       // the fields of its class, and their types
	seen   map[string]bool         // the values at the top of the file already read for it
}

// traceFiles fills in the facts of the functions each file lists, and appends the entry points
// each file declares or registers.
func traceFiles(files []*file, a *facts.Analysis, read func(f *file, funcs map[string]bool) ([]tfunc, *tracing, func() []facts.Entry)) {
	funcs := map[string]bool{}
	at := map[string]int{}
	for i, s := range a.Symbols {
		if s.Kind == facts.SymbolFunction || s.Kind == facts.SymbolMethod {
			funcs[s.ID] = true
			at[s.ID] = i
		}
	}
	for _, f := range files {
		if f.test || f.generatedGlob {
			continue
		}
		fns, tr, entries := read(f, funcs)
		for _, fn := range fns {
			i, ok := at[fn.id]
			if !ok || fn.body == nil {
				continue
			}
			got := tr.reach(fn, fn.body)
			s := &a.Symbols[i]
			s.IO, s.Calls, s.Units = got.io, keys(got.calls), keys(got.units)
		}
		a.Entries = append(a.Entries, entries()...)
	}
}

// reach reads what code of a function reaches.
func (tr *tracing) reach(fn tfunc, code *sitter.Node) reached {
	got := newReached()
	tr.locals, tr.fields = map[string]string{}, map[string]string{}
	if tr.syn.vars != nil {
		if fn.scope != nil { // the class's fields, not the parameters of its other functions
			walk(fn.scope, func(n *sitter.Node) bool {
				if tr.syn.funcs[n.Kind()] {
					return false
				}
				for k, v := range tr.syn.vars(tr.f, n) {
					tr.fields[k] = v
				}
				return true
			})
		}
		if fn.decl != nil {
			walk(fn.decl, func(n *sitter.Node) bool {
				for k, v := range tr.syn.vars(tr.f, n) {
					tr.locals[k] = v
				}
				return true
			})
		}
	}
	tr.seen = map[string]bool{}
	tr.reachInto(fn, code, &got)
	return got
}

func (tr *tracing) reachInto(fn tfunc, code *sitter.Node, got *reached) {
	f := tr.f
	walk(code, func(n *sitter.Node) bool {
		if tr.syn.special != nil && tr.syn.special(tr, fn, n, got) {
			return false
		}
		if obj, name := tr.syn.member(f, n); obj != nil && name != nil {
			// A known name is read here; any other object is read further down.
			return !tr.memberOf(fn, obj, f.text(name), got)
		}
		if !tr.syn.idents[n.Kind()] || tr.memberName(n) {
			return true
		}
		tr.name(fn, f.text(n), got)
		return true
	})
}

// memberName reports whether an identifier is the member side of a member access.
func (tr *tracing) memberName(n *sitter.Node) bool {
	p := n.Parent()
	if p == nil {
		return false
	}
	_, name := tr.syn.member(tr.f, p)
	return name != nil && name.Id() == n.Id()
}

// name records the use of a bare name.
func (tr *tracing) name(fn tfunc, x string, got *reached) {
	if _, local := tr.locals[x]; local {
		return
	}
	if b, ok := tr.binds[x]; ok {
		tr.use(b, got)
		return
	}
	switch {
	case fn.class != "" && tr.funcs[fn.class+"."+x]:
		got.calls[fn.class+"."+x] = true
	case tr.funcs[fn.own+"."+x]:
		got.calls[fn.own+"."+x] = true
	default:
		if v, ok := tr.values[x]; ok && !tr.seen[x] { // a value made from another reaches what that one does
			tr.seen[x] = true
			tr.reachInto(fn, v, got)
		}
	}
}

// use records the use of what a name binds.
func (tr *tracing) use(b tbind, got *reached) {
	switch {
	case b.io:
		got.io = true
	case tr.funcs[b.id]:
		got.calls[b.id] = true
	case b.unit != "":
		got.units[b.unit] = true
	}
}

// memberOf records a.b where a is the object itself, a variable or field of a known type, a name
// the file binds, a field of the object itself or a new object of a known class. It reports
// whether a is the object itself or a name it knew, which then needs no reading of its own.
func (tr *tracing) memberOf(fn tfunc, obj *sitter.Node, name string, got *reached) bool {
	if tr.self(obj) {
		if fn.class != "" && tr.funcs[fn.class+"."+name] {
			got.calls[fn.class+"."+name] = true
		}
		return true
	}
	var b tbind
	known := tr.syn.idents[obj.Kind()]
	if known {
		if b, known = tr.named(tr.f.text(obj)); !known {
			return false
		}
	} else {
		b = tr.object(fn, obj)
	}
	switch {
	case b.io:
		got.io = true
	case b.id != "" && tr.funcs[b.id+"."+name]:
		got.calls[b.id+"."+name] = true
	case b.unit != "":
		got.units[b.unit] = true
	}
	return known
}

// self reports whether a node is the object itself: self, this.
func (tr *tracing) self(n *sitter.Node) bool {
	return tr.syn.selves[n.Kind()] || tr.syn.selves[tr.f.text(n)]
}

// typed returns what a variable's written type stands for; nothing for a variable written without
// one.
func (tr *tracing) typed(t string) tbind {
	if t == "" {
		return tbind{}
	}
	return tr.types(t)
}

// named returns what a name used as an object stands for, and whether it is one the function
// knows: a variable, a name the file binds, or a field of its class.
func (tr *tracing) named(x string) (tbind, bool) {
	if t, isLocal := tr.locals[x]; isLocal {
		return tr.typed(t), true
	}
	if b, bound := tr.binds[x]; bound {
		return b, true
	}
	if t, isField := tr.fields[x]; isField {
		return tr.typed(t), true
	}
	return tbind{}, false
}

// object returns what an object of more than a name stands for: a field of the object itself, or
// a new object of a known class. Anything else stands for nothing known.
func (tr *tracing) object(fn tfunc, obj *sitter.Node) tbind {
	f := tr.f
	if inner, field := tr.syn.member(f, obj); inner != nil && field != nil && tr.self(inner) {
		return tr.typed(tr.fields[f.text(field)])
	}
	if tr.syn.built != nil {
		if t := tr.syn.built(f, obj); t != "" {
			return tr.class(fn, t)
		}
	}
	return tbind{}
}

// enclosing returns the function whose declaration holds n, whose parameters and class code there
// sees, or top when n is outside every function. The functions listed never nest, so at most one
// holds n.
func enclosing(fns []tfunc, n *sitter.Node, top tfunc) tfunc {
	for _, fn := range fns {
		if fn.decl.StartByte() <= n.StartByte() && n.EndByte() <= fn.decl.EndByte() {
			return fn
		}
	}
	return top
}

// class returns what a class name stands for: a class the file binds, or one it declares.
func (tr *tracing) class(fn tfunc, name string) tbind {
	if b, ok := tr.binds[name]; ok {
		return b
	}
	if b := tr.types(name); b.id != "" || b.io {
		return b
	}
	return tbind{id: fn.own + "." + name}
}

// simpleType reduces a written type to the simple name of its class: List<Order>? is List,
// &mut PgPool is PgPool, a.b.Store is Store.
func simpleType(s string) string {
	s = strings.TrimSpace(s)
	for _, p := range []string{"&", "mut ", "dyn ", "impl "} {
		s = strings.TrimSpace(strings.TrimPrefix(s, p))
	}
	if i := strings.IndexAny(s, "<(["); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSuffix(strings.TrimSpace(s), "?")
	s = s[strings.LastIndexAny(s, ".:")+1:]
	return s
}

// handlers returns the entries a handler value is: the functions of the repository it names, or a
// function written in place, whose calls are listed.
func (tr *tracing) handlers(fn tfunc, a *sitter.Node, inline map[string]bool) []facts.Entry {
	got := tr.reach(fn, a)
	if inline[a.Kind()] {
		return []facts.Entry{{Path: tr.f.path, Line: line(a), Calls: keys(got.calls)}}
	}
	var out []facts.Entry
	for _, id := range keys(got.calls) {
		out = append(out, facts.Entry{Path: tr.f.path, Line: line(a), Symbol: id})
	}
	return out
}
