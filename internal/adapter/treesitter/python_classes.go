//go:build cgo

package treesitter

import (
	"strconv"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

// What a Python test class inherits: the setup methods and autouse fixtures of its base classes,
// found in its own file or in the modules its file imports.

// pyCode is a node of Python code with the file that holds it, which may not be the test's.
type pyCode struct {
	pf *pyFile
	n  *sitter.Node
}

func (c pyCode) key() string {
	return c.pf.f.path + "#" + strconv.FormatUint(uint64(c.n.StartByte()), 10)
}

// mro lists a test class and the base classes hadome finds, in the order Python looks up a method
// on it (C3 linearization). class is the class's possibly decorated definition.
func (pf *pyFile) mro(class *sitter.Node) []pyCode {
	return pyMRO(pyCode{pf, definition(class)}, map[string]bool{})
}

// pyMRO linearizes a class; it returns nil for a class already being linearized, which only a
// class that extends itself reaches.
func pyMRO(c pyCode, walking map[string]bool) []pyCode {
	if walking[c.key()] {
		return nil
	}
	walking[c.key()] = true
	defer delete(walking, c.key())
	var bases []pyCode
	var seqs [][]pyCode
	for _, b := range c.pf.bases(c.n) {
		if m := pyMRO(b, walking); m != nil {
			bases = append(bases, b)
			seqs = append(seqs, m)
		}
	}
	return append([]pyCode{c}, c3merge(append(seqs, bases))...)
}

// c3merge merges linearizations as Python does: it takes the first head that is in no tail. A
// hierarchy where none is, which Python refuses to create, ends the merge there.
func c3merge(seqs [][]pyCode) []pyCode {
	var out []pyCode
	for {
		var left [][]pyCode
		for _, s := range seqs {
			if len(s) > 0 {
				left = append(left, s)
			}
		}
		if len(left) == 0 {
			return out
		}
		var next *pyCode
		for _, s := range left {
			if !inTails(left, s[0]) {
				next = &s[0]
				break
			}
		}
		if next == nil {
			return out
		}
		out = append(out, *next)
		k := next.key()
		for i, s := range left {
			if s[0].key() == k {
				left[i] = s[1:]
			}
		}
		seqs = left
	}
}

func inTails(seqs [][]pyCode, c pyCode) bool {
	for _, s := range seqs {
		for _, t := range s[1:] {
			if t.key() == c.key() {
				return true
			}
		}
	}
	return false
}

// bases are the base classes of a class definition that hadome finds.
func (pf *pyFile) bases(def *sitter.Node) []pyCode {
	list := def.ChildByFieldName("superclasses")
	if list == nil {
		return nil
	}
	var out []pyCode
	for _, a := range named(list) {
		if b, ok := pf.classOf(a); ok {
			out = append(out, b)
		}
	}
	return out
}

// classOf finds the class an expression of the file names: a class of the file, a class imported
// by name, or a class of an imported module (base.BaseCase).
func (pf *pyFile) classOf(e *sitter.Node) (pyCode, bool) {
	f := pf.f
	switch e.Kind() {
	case "identifier":
		name := f.text(e)
		if def := pf.classDef(name); def != nil {
			return pyCode{pf, def}, true
		}
		if v, ok := pf.via[name]; ok && v[1] != "" {
			return pf.classIn(v[0], v[1])
		}
		if tg, ok := pf.names[name]; ok {
			if i := strings.LastIndex(tg, "."); i > 0 {
				return pf.classIn(tg[:i], tg[i+1:])
			}
		}
	case "attribute":
		obj, attr := f.field(e, "object"), f.field(e, "attribute")
		if mod, ok := pf.spaces[obj]; ok {
			return pf.classIn(mod, attr)
		}
		if v, ok := pf.via[obj]; ok && v[1] == "" {
			return pf.classIn(v[0], attr)
		}
	}
	return pyCode{}, false
}

// classIn finds a class at the top of the module at a module path.
func (pf *pyFile) classIn(mod, name string) (pyCode, bool) {
	if mf := pf.r.moduleFile(mod, pf.t); mf != nil {
		if def := mf.classDef(name); def != nil {
			return pyCode{mf, def}, true
		}
	}
	return pyCode{}, false
}

// classDef is the class of a name at the top of the file.
func (pf *pyFile) classDef(name string) *sitter.Node {
	for _, n := range named(pf.f.root) {
		if def := definition(n); def.Kind() == "class_definition" && pf.f.field(def, "name") == name {
			return def
		}
	}
	return nil
}

// moduleFile reads the file of a module of the repository, whatever its role: a class a test class
// extends may live in a test support module, a test file or the product's code.
func (r *python) moduleFile(mod string, t *tables) *pyFile {
	if sf := r.supportFile(mod, t); sf != nil {
		return sf
	}
	p := mod + ".py"
	if r.files[p] == nil {
		p = mod + "/__init__.py"
	}
	if mf, ok := r.modules[p]; ok {
		return mf
	}
	var mf *pyFile
	if f := r.files[p]; f != nil {
		mf = r.testFile(f, t)
	}
	r.modules[p] = mf
	return mf
}

// classSetups are the setup methods that run for a test of a class: for each name of pySetupNames,
// the first definition along the class's MRO, and the next one each time a definition calls the
// method of its name on super() or on a base class by name.
func (pf *pyFile) classSetups(class *sitter.Node) []pyCode {
	mro := pf.mro(class)
	var out []pyCode
	for _, name := range pySetupNames {
		for _, c := range mro {
			m := c.pf.method(c.n, name)
			if m == nil {
				continue
			}
			body := m.ChildByFieldName("body")
			out = append(out, pyCode{c.pf, body})
			if !c.pf.callsInherited(body, name) {
				break
			}
		}
	}
	return out
}

// classAutouse are the bodies of the autouse fixtures a test class and its base classes hold. A
// fixture of a name already found earlier in the MRO overrides a later one, as pytest does.
func (pf *pyFile) classAutouse(class *sitter.Node) []pyCode {
	seen := map[string]bool{}
	var out []pyCode
	for _, c := range pf.mro(class) {
		for _, m := range named(c.n.ChildByFieldName("body")) {
			md := definition(m)
			if md.Kind() != "function_definition" || m.Kind() != "decorated_definition" || !pyFixture(c.pf.f, m) {
				continue
			}
			name := c.pf.f.field(md, "name")
			if seen[name] {
				continue
			}
			seen[name] = true
			if pyAutouse(c.pf.f, m) {
				out = append(out, pyCode{c.pf, md.ChildByFieldName("body")})
			}
		}
	}
	return out
}

// method is the method of a name a class definition holds itself.
func (pf *pyFile) method(def *sitter.Node, name string) *sitter.Node {
	for _, m := range named(def.ChildByFieldName("body")) {
		if md := definition(m); md.Kind() == "function_definition" && pf.f.field(md, "name") == name {
			return md
		}
	}
	return nil
}

// callsInherited reports whether a method body calls the method of its name on super(), or on a
// class by name (Base.setUp(self)), which runs the next definition along the MRO.
func (pf *pyFile) callsInherited(body *sitter.Node, name string) bool {
	found := false
	walk(body, func(n *sitter.Node) bool {
		if found || n.Kind() == "function_definition" || n.Kind() == "class_definition" {
			return false
		}
		if n.Kind() != "call" {
			return true
		}
		fn := n.ChildByFieldName("function")
		if fn == nil || fn.Kind() != "attribute" || pf.f.field(fn, "attribute") != name {
			return true
		}
		switch obj := fn.ChildByFieldName("object"); {
		case obj.Kind() == "call" && pf.f.field(obj, "function") == "super":
			found = true
		case obj.Kind() == "identifier" && pf.f.text(obj) != "self" && pf.f.text(obj) != "cls":
			found = true
		}
		return true
	})
	return found
}
