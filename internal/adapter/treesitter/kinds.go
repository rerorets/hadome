// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

//go:build cgo

package treesitter

// What a value in a test is, for the methods called on it (docs/design/adapters-treesitter.md,
// "Kinds of values in tests"): one of the repository's types, whose methods are targets; a type
// of the language or of an external library, whose methods count as nothing; or unknown, whose
// methods are unresolved. The zero value is unknown.
type vkind struct {
	repo    string // the repository's type, when the value is one
	outside bool   // a type of the language or of an external library
	// elem is the kind of what is taken out of an outside container when its type or value says,
	// as for Store[]; nil takes it from the container (see element).
	elem *vkind
	// holds is true for an outside container with the repository's names in it, as [new Store()],
	// whose elements are unknown unless elem says.
	holds bool
}

var outsideKind = vkind{outside: true}

func repoKind(id string) vkind { return vkind{repo: id} }

// containerOf is an outside container whose elements are of kind e.
func containerOf(e vkind) vkind { return vkind{outside: true, elem: &e} }

// known reports whether the value's kind was decided: a source that leaves it unknown gives way to
// the next one.
func (k vkind) known() bool { return k.repo != "" || k.outside }

// pure reports whether the value and everything taken out of it are outside: no name of the
// repository can be reached through it.
func (k vkind) pure() bool {
	return k.outside && !k.holds && (k.elem == nil || k.elem.pure())
}

// element is the kind of an element taken out of the value: by index, by a loop or by
// destructuring. Only a pure value's elements are outside without its type saying so.
func (k vkind) element() vkind {
	switch {
	case k.elem != nil:
		return *k.elem
	case k.pure():
		return outsideKind
	}
	return vkind{}
}

// member is the kind of a member of the value or a method's result: outside only for a pure value,
// since hadome reads no type a member or a method declares.
func (k vkind) member() vkind {
	if k.pure() {
		return outsideKind
	}
	return vkind{}
}

// or is k when it is known, else the kind next gives.
func (k vkind) or(next func() vkind) vkind {
	if k.known() {
		return k
	}
	return next()
}
