//go:build cgo

package treesitter

import (
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/rerorets/hadome/internal/facts"
)

// branches are a language's node kinds that add one to cyclomatic complexity, its boolean
// operators that do, and the node kinds that start a nested function, whose branches are counted
// on their own.
type branches struct {
	kinds  map[string]bool
	ops    map[string]bool
	nested map[string]bool
}

// counted is the cyclomatic complexity of a function without its nested functions, and those
// nested functions at any depth, each counted the same way.
type counted struct {
	own    int
	nested []facts.Nested
}

// complexity is 1, plus one per branch inside nodes outside nested functions. A node may be nil.
func (b branches) complexity(f *file, nodes ...*sitter.Node) counted {
	c := counted{own: 1}
	for _, n := range nodes {
		walk(n, func(x *sitter.Node) bool {
			if x.IsNamed() && b.nested[x.Kind()] { // Python names both the lambda and its keyword "lambda"
				var children []*sitter.Node // not x itself, which would be nested again
				for i := uint(0); i < x.ChildCount(); i++ {
					children = append(children, x.Child(i))
				}
				inner := b.complexity(f, children...)
				c.nested = append(append(c.nested, facts.Nested{Line: line(x), Complexity: inner.own}), inner.nested...)
				return false
			}
			if b.kinds[x.Kind()] && !fallback(f.text(x)) {
				c.own++
			}
			if !x.IsNamed() && b.ops[f.text(x)] { // operators are anonymous tokens in every grammar
				c.own++
			}
			return true
		})
	}
	return c
}

// fallback reports the branch taken when no other is: a switch's default, a when's else.
func fallback(text string) bool {
	return strings.HasPrefix(text, "default") || strings.HasPrefix(text, "else ") || strings.HasPrefix(text, "else-") ||
		strings.HasPrefix(text, "_ ") // Rust's catch-all match arm
}

func set(items ...string) map[string]bool {
	m := map[string]bool{}
	for _, i := range items {
		m[i] = true
	}
	return m
}

// docBefore reports whether a doc comment (one starting with one of prefixes) ends on the line
// right before n, or on n's own first line.
func docBefore(f *file, n *sitter.Node, prefixes ...string) bool {
	prev := n.PrevSibling()
	for prev != nil && strings.Contains(prev.Kind(), "comment") {
		text := strings.TrimSpace(f.text(prev))
		for _, p := range prefixes {
			if strings.HasPrefix(text, p) && endLine(prev) >= line(n)-1 {
				return true
			}
		}
		if !strings.HasPrefix(text, "///") { // a run of /// lines is one doc comment
			return false
		}
		prev = prev.PrevSibling()
	}
	return false
}

// funcSymbol completes the symbol of a function or method declared at n. s carries what the
// language knows (ID, kind, exported, doc); params are the parameter names in order.
func funcSymbol(f *file, n *sitter.Node, s facts.Symbol, params []string, cx counted) facts.Symbol {
	for _, p := range params {
		s.Scoped = s.Scoped || facts.IsScopeName(p)
	}
	s.Path, s.Line, s.EndLine, s.Params, s.Complexity, s.Nested = f.path, line(n), endLine(n), len(params), cx.own, cx.nested
	return s
}

// scopeOf returns the position of the scope parameter: the first whose name says user, tenant and
// the like, or else the first. It is -1 without parameters.
func scopeOf(params []string) int {
	for i, p := range params {
		if facts.IsScopeName(p) {
			return i
		}
	}
	if len(params) == 0 {
		return -1
	}
	return 0
}
