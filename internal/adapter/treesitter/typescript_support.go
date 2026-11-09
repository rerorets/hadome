//go:build cgo

package treesitter

import (
	"maps"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

// How a TypeScript test file binds what it imports, and how a test is followed into a test support
// file and recognised as driving the application in memory.

// testFile reads what a test file, or a test support file, binds.
func (r *typescript) testFile(f *file, t *tables) *tsFile {
	tf := &tsFile{f: f, r: r, t: t, names: map[string]string{}, spaces: map[string]string{}, via: map[string][2]string{},
		unresolved: map[string]bool{}, loaderFns: map[string]string{}, external: map[string]bool{}, helpers: map[string]*sitter.Node{}, comments: map[int]string{}, loaders: map[string]string{},
		outer: map[string]string{}, params: map[string]*sitter.Node{}, setups: map[string][]tsSetup{},
		setupNodes: map[uintptr]bool{}, outerLoaded: map[string]string{}, outerModules: map[string]string{}}
	tf.bind()
	tf.bindLoaders()
	tf.bindOuter()
	tf.bindSetups()
	return tf
}

// supportFile is a test support file read as helpers, or nil for a path that is not one.
func (r *typescript) supportFile(p string, t *tables) *tsFile {
	if sf, ok := r.supports[p]; ok {
		return sf
	}
	var sf *tsFile
	if f := r.files[p]; f != nil && f.support {
		sf = r.testFile(f, t)
	}
	r.supports[p] = sf
	return sf
}

// required returns the specifier of require('./x'), named by a string, or "".
func (tf *tsFile) required(call *sitter.Node) string {
	if call == nil || call.Kind() != "call_expression" || tf.f.field(call, "function") != "require" {
		return ""
	}
	if args := arguments(call.ChildByFieldName("arguments")); len(args) == 1 && args[0].Kind() == "string" {
		return unquote(tf.f.text(args[0]))
	}
	return ""
}

// bindImport binds the names an import statement, or the pattern a require() is assigned to,
// takes from spec.
func (tf *tsFile) bindImport(n *sitter.Node, spec string) {
	p, kind := tf.r.project.resolve(tf.f.path, spec)
	if kind == tsExternal || n == nil {
		tf.bindExternal(n, kind)
		return
	}
	switch n.Kind() {
	case "identifier": // const mail = require('./mail')
		tf.bindModule(tf.f.text(n), p, kind)
		return
	case "object_pattern": // const { send, list: all } = require('./mail')
		for _, c := range named(n) {
			switch c.Kind() {
			case "shorthand_property_identifier_pattern":
				tf.bindName(tf.f.text(c), p, tf.f.text(c), kind)
			case "pair_pattern":
				tf.bindName(tf.f.field(c, "value"), p, tf.f.field(c, "key"), kind)
			}
		}
		return
	}
	walk(n, func(x *sitter.Node) bool {
		switch x.Kind() {
		case "import_specifier":
			name := tf.f.field(x, "name")
			local := tf.f.field(x, "alias")
			if local == "" {
				local = name
			}
			tf.bindName(local, p, name, kind)
		case "namespace_import":
			for _, id := range named(x) {
				tf.bindModule(tf.f.text(id), p, kind)
			}
		case "import_clause":
			if first := x.NamedChild(0); first != nil && first.Kind() == "identifier" {
				tf.bindName(tf.f.text(first), p, "default", kind)
			}
		}
		return true
	})
}

// bindExternal records the names an import from outside the repository binds.
func (tf *tsFile) bindExternal(n *sitter.Node, kind tsKind) {
	if n == nil || kind != tsExternal {
		return
	}
	for _, name := range tsImportedNames(tf.f, n) {
		tf.external[name] = true
	}
}

// bindName binds a local name to a name exported by the file p.
func (tf *tsFile) bindName(local, p, name string, kind tsKind) {
	switch {
	case kind == tsMissing || kind == tsUnread:
		tf.unresolved[local] = true
	case tf.r.files[p] != nil && tf.r.files[p].support:
		file, decl := tf.r.declaring(p, name)
		tf.via[local] = [2]string{file, decl}
	default:
		tf.names[local] = tf.r.target(p, name)
	}
}

// bindModule binds a local name to the whole module p.
func (tf *tsFile) bindModule(local, p string, kind tsKind) {
	if kind == tsMissing || kind == tsUnread {
		tf.unresolved[local] = true
		return
	}
	tf.spaces[local] = p
}

// followSupport walks a function of a test support file the test calls. Its calls resolve through
// the support file's own imports, and its hazards are reported at its own lines. A name the file
// holds no function for counts as unresolved.
func (w *tsWalk) followSupport(p, name string, args []*sitter.Node) {
	sf := w.tf.r.supportFile(p, w.tf.t)
	if sf == nil || sf.helpers[name] == nil {
		w.t.test.Unresolved++
		return
	}
	w.t.callsSupport(sf.f.path)
	sub := &tsWalk{tf: sf, t: w.t, vars: sf.outerVars(), locals: map[string]bool{}, walking: w.walking,
		loaded: maps.Clone(sf.outerLoaded), modules: maps.Clone(sf.outerModules), used: map[string]bool{},
		done: map[uintptr]bool{}, fake: w.fake, plain: map[string]bool{}, rows: map[string][]string{}}
	prev := w.t.f
	w.t.f = sf.f
	sub.followFrom(w, name, args)
	w.t.f = prev
}

// appRequest recognises a request handed in memory to an application that serves HTTP the fetch
// way: Hono's app.request('/rows'), and app.fetch(new Request(...)) of any such framework.
func (w *tsWalk) appRequest(fn *sitter.Node, args []*sitter.Node) bool {
	f := w.tf.f
	if fn.Kind() != "member_expression" || len(args) == 0 {
		return false
	}
	first := args[0]
	request := first.Kind() == "new_expression" && f.field(first, "constructor") == "Request"
	switch f.field(fn, "property") {
	case "fetch":
		return request
	case "request":
		path := (first.Kind() == "string" || first.Kind() == "template_string") && strings.HasPrefix(unquote(f.text(first)), "/")
		_, imported := w.tf.names[f.field(fn, "object")]
		return request || path || imported
	}
	return false
}
