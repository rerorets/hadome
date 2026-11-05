package ansible

import (
	"strings"

	"github.com/goccy/go-yaml/ast"
)

// entries returns the key-value pairs of a mapping; nil when the node is not one. A mapping of one
// pair comes from the parser as the pair itself.
func entries(n ast.Node) []*ast.MappingValueNode {
	switch n := unwrap(n).(type) {
	case *ast.MappingNode:
		return n.Values
	case *ast.MappingValueNode:
		return []*ast.MappingValueNode{n}
	}
	return nil
}

// items returns the elements of a sequence; nil when the node is not one.
func items(n ast.Node) []ast.Node {
	if s, ok := unwrap(n).(*ast.SequenceNode); ok {
		return s.Values
	}
	return nil
}

// value returns the value of a mapping's key, or nil.
func value(n ast.Node, key string) ast.Node {
	for _, kv := range entries(n) {
		if keyOf(kv) == key {
			return kv.Value
		}
	}
	return nil
}

// unwrap removes tags and anchors around a node.
func unwrap(n ast.Node) ast.Node {
	for {
		switch t := n.(type) {
		case *ast.TagNode:
			n = t.Value
		case *ast.AnchorNode:
			n = t.Value
		default:
			return n
		}
	}
}

func keyOf(kv *ast.MappingValueNode) string {
	s, _ := scalar(kv.Key)
	return s
}

// scalar returns a string, number or boolean as written, and false for anything else.
func scalar(n ast.Node) (string, bool) {
	switch n := unwrap(n).(type) {
	case *ast.StringNode:
		return n.Value, true
	case *ast.LiteralNode:
		return n.Value.Value, true
	case *ast.IntegerNode, *ast.FloatNode, *ast.BoolNode:
		return n.GetToken().Value, true
	}
	return "", false
}

// text is a scalar's value, or the strings of a list joined by line breaks (a description may be
// either).
func text(n ast.Node) string {
	if s, ok := scalar(n); ok {
		return s
	}
	var lines []string
	for _, it := range items(n) {
		if s, ok := scalar(it); ok {
			lines = append(lines, s)
		}
	}
	return strings.Join(lines, "\n")
}

func line(n ast.Node) int {
	if n == nil || n.GetToken() == nil {
		return 1
	}
	return n.GetToken().Position.Line
}

// lastLine is the last line a node's tokens reach.
func lastLine(n ast.Node) int {
	last := line(n)
	ast.Walk(visitor(func(m ast.Node) {
		if tk := m.GetToken(); tk != nil {
			last = max(last, tk.Position.Line+strings.Count(strings.TrimRight(tk.Value, "\n"), "\n"))
		}
	}), n)
	return last
}

type visitor func(ast.Node)

func (v visitor) Visit(n ast.Node) ast.Visitor {
	v(n)
	return v
}
