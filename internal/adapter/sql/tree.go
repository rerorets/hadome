package sql

import "slices"

// Node is one node of a syntax tree, the same shape for every dialect: a rule of the dialect's
// grammar with its children, or a token. Dialects find definitions and names by rule names.
type Node struct {
	Rule  string // the grammar rule; empty for a token
	Token string // the token's symbolic name; empty for a rule
	Text  string // the token's text; empty for a rule
	// Start and End are byte offsets of the node in the file, End exclusive.
	Start, End int
	Line       int
	Children   []*Node
	Parent     *Node
}

// Find returns the first node at or below n, in source order, for which match is true. Nodes below a
// match are not searched.
func (n *Node) Find(match func(*Node) bool) *Node {
	if n == nil {
		return nil
	}
	if match(n) {
		return n
	}
	for _, c := range n.Children {
		if m := c.Find(match); m != nil {
			return m
		}
	}
	return nil
}

// All returns every node below n, in source order, for which match is true, without searching
// below a match.
func (n *Node) All(match func(*Node) bool) []*Node {
	var out []*Node
	var walk func(*Node)
	walk = func(m *Node) {
		for _, c := range m.Children {
			if match(c) {
				out = append(out, c)
				continue
			}
			walk(c)
		}
	}
	if n != nil {
		walk(n)
	}
	return out
}

// Walk calls visit for every node at or below n in source order. visit returns false to skip the
// node's children.
func (n *Node) Walk(visit func(*Node) bool) {
	if n == nil || !visit(n) {
		return
	}
	for _, c := range n.Children {
		c.Walk(visit)
	}
}

// Child returns the first direct child that is the rule (or token) named, or nil.
func (n *Node) Child(name string) *Node {
	if n == nil {
		return nil
	}
	for _, c := range n.Children {
		if c.Rule == name || c.Token == name {
			return c
		}
	}
	return nil
}

// Tokens returns the tokens at or below n in source order.
func (n *Node) Tokens() []*Node {
	var out []*Node
	n.Walk(func(m *Node) bool {
		if m.Rule == "" {
			out = append(out, m)
		}
		return true
	})
	return out
}

// Ancestor returns the nearest node above n whose rule is one of names, or nil.
func (n *Node) Ancestor(names ...string) *Node {
	for p := n.Parent; p != nil; p = p.Parent {
		if slices.Contains(names, p.Rule) {
			return p
		}
	}
	return nil
}
