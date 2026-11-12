//go:build cgo

package treesitter

import (
	"unsafe"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

// node is a Go copy of one visible tree-sitter node. Its methods have the names and meanings of
// the tree-sitter methods the readers use, and none of them calls C: a reader walks the tree as
// often as it likes without crossing cgo.
type node struct {
	kind, field uint16 // indexes into nodeNames.text; field is 0 for a child in no field
	named       bool
	childCount  uint32
	index       uint32 // the node's index among its parent's children
	startByte   uint32
	endByte     uint32
	startRow    uint32
	startColumn uint32
	endRow      uint32
	endColumn   uint32
	parent      *node
	firstChild  *node
	nextSibling *node
	prevSibling *node
}

// point is a row and a column, both from 0. Columns count bytes, as tree-sitter's do.
type point struct {
	Row, Column uint
}

func (n *node) Kind() string     { return nodeNames.text[n.kind] }
func (n *node) IsNamed() bool    { return n.named }
func (n *node) StartByte() uint  { return uint(n.startByte) }
func (n *node) EndByte() uint    { return uint(n.endByte) }
func (n *node) ChildCount() uint { return uint(n.childCount) }
func (n *node) Parent() *node    { return n.parent }

// Id is the node's address: the same node always has the same ID, and two nodes never share one.
func (n *node) Id() uintptr { return uintptr(unsafe.Pointer(n)) }

func (n *node) StartPosition() point {
	return point{Row: uint(n.startRow), Column: uint(n.startColumn)}
}

func (n *node) EndPosition() point { return point{Row: uint(n.endRow), Column: uint(n.endColumn)} }

// Utf8Text returns the node's text in source, the bytes the node was read from.
func (n *node) Utf8Text(source []byte) string { return string(source[n.startByte:n.endByte]) }

// Child returns the child at index i, or nil.
func (n *node) Child(i uint) *node {
	c := n.firstChild
	for ; c != nil && i > 0; i-- {
		c = c.nextSibling
	}
	return c
}

func (n *node) NamedChildCount() uint {
	count := uint(0)
	for c := n.firstChild; c != nil; c = c.nextSibling {
		if c.named {
			count++
		}
	}
	return count
}

// NamedChild returns the named child at index i among the named children, or nil.
func (n *node) NamedChild(i uint) *node {
	for c := n.firstChild; c != nil; c = c.nextSibling {
		if !c.named {
			continue
		}
		if i == 0 {
			return c
		}
		i--
	}
	return nil
}

// ChildByFieldName returns the first child in the field, or nil.
func (n *node) ChildByFieldName(name string) *node {
	for c := n.firstChild; c != nil; c = c.nextSibling {
		if c.field != 0 && nodeNames.text[c.field] == name {
			return c
		}
	}
	return nil
}

// FieldNameForChild returns the field of the child at index i, or "".
func (n *node) FieldNameForChild(i uint32) string {
	if c := n.Child(uint(i)); c != nil && c.field != 0 {
		return nodeNames.text[c.field]
	}
	return ""
}

func (n *node) NextSibling() *node { return n.nextSibling }
func (n *node) PrevSibling() *node { return n.prevSibling }

func (n *node) NextNamedSibling() *node {
	for s := n.nextSibling; s != nil; s = s.nextSibling {
		if s.named {
			return s
		}
	}
	return nil
}

func (n *node) PrevNamedSibling() *node {
	for s := n.prevSibling; s != nil; s = s.prevSibling {
		if s.named {
			return s
		}
	}
	return nil
}

// names is the table of kind and field names of every grammar. Nodes hold names as indexes into
// text, so a node stays small and its Kind is a slice read. It is made once, before any file is
// read, and never changes.
type names struct {
	text []string
	// kinds and fields map a grammar's symbol and field numbers to indexes into text. Grammars
	// number them differently, even two of one language (TypeScript and TSX).
	kinds  map[*sitter.Language][]uint16
	fields map[*sitter.Language][]uint16
	err    uint16 // the index of "ERROR", the kind of an error node in every grammar
}

// errorSymbol is the symbol tree-sitter gives an error node; it is outside every grammar's table.
const errorSymbol = 0xFFFF

var nodeNames = newNames(typescriptGrammar, tsxGrammar, pythonGrammar, rustGrammar, javaGrammar, kotlinGrammar)

func newNames(grammars ...*sitter.Language) *names {
	t := &names{text: []string{""}, kinds: map[*sitter.Language][]uint16{}, fields: map[*sitter.Language][]uint16{}}
	index := map[string]uint16{"": 0}
	intern := func(s string) uint16 {
		if i, ok := index[s]; ok {
			return i
		}
		i := uint16(len(t.text))
		t.text = append(t.text, s)
		index[s] = i
		return i
	}
	t.err = intern("ERROR")
	for _, g := range grammars {
		kinds := make([]uint16, g.NodeKindCount())
		for id := range kinds {
			kinds[id] = intern(g.NodeKindForId(uint16(id)))
		}
		t.kinds[g] = kinds
		fields := make([]uint16, g.FieldCount()+1) // field numbers start at 1; 0 is no field
		for id := 1; id < len(fields); id++ {
			fields[id] = intern(g.FieldNameForId(uint16(id)))
		}
		t.fields[g] = fields
	}
	return t
}

// copyTree copies the visible nodes of tree, which grammar g read, walking it once with a cursor.
// The nodes are allocated together, in one array sized from the root's descendant count, so their
// addresses never move.
func copyTree(tree *sitter.Tree, g *sitter.Language) *node {
	kinds, fields := nodeNames.kinds[g], nodeNames.fields[g]
	root := tree.RootNode()
	nodes := make([]node, root.DescendantCount())
	next := 0
	alloc := func() *node {
		if next == len(nodes) { // never expected: the descendant count is every visible node
			nodes, next = make([]node, 64), 0
		}
		next++
		return &nodes[next-1]
	}
	c := tree.Walk()
	defer c.Close()
	read := func(parent, prev *node) *node {
		tn := c.Node()
		n := alloc()
		if symbol := tn.KindId(); symbol == errorSymbol {
			n.kind = nodeNames.err
		} else {
			n.kind = kinds[symbol]
		}
		if parent != nil {
			n.field = fields[c.FieldId()]
		}
		n.named = tn.IsNamed()
		n.startByte, n.endByte = uint32(tn.StartByte()), uint32(tn.EndByte())
		start, end := tn.StartPosition(), tn.EndPosition()
		n.startRow, n.startColumn = uint32(start.Row), uint32(start.Column)
		n.endRow, n.endColumn = uint32(end.Row), uint32(end.Column)
		n.parent, n.prevSibling = parent, prev
		if parent != nil {
			if prev == nil {
				parent.firstChild = n
			} else {
				prev.nextSibling = n
				n.index = prev.index + 1
			}
			parent.childCount++
		}
		return n
	}
	top := read(nil, nil)
	at := top
	for {
		if c.GotoFirstChild() {
			at = read(at, nil)
			continue
		}
		for !c.GotoNextSibling() {
			if !c.GotoParent() {
				return top
			}
			at = at.parent
		}
		at = read(at.parent, at)
	}
}
