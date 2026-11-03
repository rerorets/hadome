//go:build cgo

package treesitter

import (
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/rerorets/hadome/internal/facts"
)

// branches are a language's node kinds that add one to cyclomatic complexity, and its boolean
// operators that do.
type branches struct {
	kinds map[string]bool
	ops   map[string]bool
}

// complexity is 1, plus one per branch inside n.
func (b branches) complexity(f *file, n *sitter.Node) int {
	c := 1
	walk(n, func(x *sitter.Node) bool {
		if b.kinds[x.Kind()] && !fallback(f.text(x)) {
			c++
		}
		if !x.IsNamed() && b.ops[f.text(x)] { // operators are anonymous tokens in every grammar
			c++
		}
		return true
	})
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
func funcSymbol(f *file, n *sitter.Node, s facts.Symbol, params []string, complexity int) facts.Symbol {
	for _, p := range params {
		s.Scoped = s.Scoped || facts.IsScopeName(p)
	}
	s.Path, s.Line, s.EndLine, s.Params, s.Complexity = f.path, line(n), endLine(n), len(params), complexity
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
