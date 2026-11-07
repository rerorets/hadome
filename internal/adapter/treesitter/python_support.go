//go:build cgo

package treesitter

import (
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// How a Python test is followed into its file's helpers, the test support modules it imports, and
// the pytest fixtures it receives.

// testFile reads what a test file, or a test support file, binds.
func (r *python) testFile(f *file, t *tables) *pyFile {
	pf := &pyFile{f: f, r: r, t: t, names: map[string]string{}, spaces: map[string]string{}, via: map[string][2]string{},
		unresolved: map[string]bool{}, helpers: map[string]*sitter.Node{}, fixtures: map[string]string{},
		fixtureDefs: map[string]*sitter.Node{}}
	pf.bind()
	pf.bindSetup()
	return pf
}

// supportFile is the test support module at a module path, or nil when it is not one.
func (r *python) supportFile(mod string, t *tables) *pyFile {
	p := mod + ".py"
	if r.files[p] == nil {
		p = mod + "/__init__.py"
	}
	if sf, ok := r.supports[p]; ok {
		return sf
	}
	var sf *pyFile
	if f := r.files[p]; f != nil && f.support {
		sf = r.testFile(f, t)
	}
	r.supports[p] = sf
	return sf
}

// support reports whether a module path is a test support module.
func (r *python) support(mod string) bool {
	for _, p := range []string{mod + ".py", mod + "/__init__.py"} {
		if f := r.files[p]; f != nil {
			return f.support
		}
	}
	return false
}

// follow walks a helper of the walker's file, once per chain of calls.
func (w *pyWalk) follow(name string) {
	body, ok := w.pf.helpers[name]
	key := w.pf.f.path + "#" + name
	if !ok || w.walking[key] {
		return
	}
	w.walking[key] = true
	w.walk(body)
	delete(w.walking, key)
}

// followSupport walks a function of a test support module the test calls. Its calls resolve
// through the module's own imports. A name the module holds no function for counts as unresolved.
func (w *pyWalk) followSupport(mod, name string) {
	sf := w.pf.r.supportFile(mod, w.pf.t)
	if sf == nil || sf.helpers[name] == nil {
		w.t.test.Unresolved++
		return
	}
	w.in(sf, func(sub *pyWalk) { sub.follow(name) })
}

// in runs walk with a walker of another file of the same test, whose hazards are reported at
// that file's lines.
func (w *pyWalk) in(pf *pyFile, walk func(*pyWalk)) {
	if pf == w.pf {
		walk(w)
		return
	}
	sub := &pyWalk{body: w.body, pf: pf, t: w.t, vars: map[string]string{}, locals: map[string]bool{}, walking: w.walking}
	prev := w.t.f
	w.t.f = pf.f
	walk(sub)
	w.t.f = prev
}

// fixture walks the body of the pytest fixture a test or fixture receives as a parameter, and the
// fixtures it receives in turn: the test runs the product code they call.
func (w *pyWalk) fixture(name string) {
	owner, def := w.pf.fixtureDef(name)
	if def == nil {
		return
	}
	key := owner.f.path + "#fixture " + name
	if w.walking[key] {
		return
	}
	w.walking[key] = true
	w.in(owner, func(sub *pyWalk) {
		for _, p := range pyParams(owner.f, def) {
			sub.fixture(p)
		}
		sub.walk(def.ChildByFieldName("body"))
	})
	delete(w.walking, key)
}
