//go:build cgo

package treesitter

import (
	"fmt"
	"io/fs"
	"slices"
	"sort"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
)

// The kinds of break, as the mutation design names them; the Go adapter uses the same words.
const (
	flipComparison = "flip a comparison"
	negateBoolean  = "negate a boolean"
	swapArithmetic = "swap arithmetic"
	returnZero     = "return the zero value"
	removeCall     = "remove a call statement"
)

var flipped = map[string]string{"<": ">=", ">=": "<", "<=": ">", ">": "<=", "==": "!=", "!=": "==",
	"===": "!==", "!==": "==="}

var swapped = map[string]string{"+": "-", "-": "+", "*": "/", "/": "*"}

// breaks is what one language tells the shared mutator: the node kinds that hold each break and
// what a removed call statement becomes. Operators are anonymous tokens in every grammar, so a
// binary node is broken at its operator child.
type breaks struct {
	functions map[string]bool   // the declarations a function symbol starts at; not entered when nested
	nested    map[string]bool   // lambdas, closures and classes, which are not entered either
	binary    map[string]bool   // nodes with an operator between two operands
	strings   map[string]bool   // string literals, on either side of which + is not swapped
	not       map[string]bool   // nodes whose first token negates the rest
	bools     map[string]bool   // the kinds of boolean literals
	literals  map[string]string // each boolean literal and its negation
	returns   map[string]bool   // return statements; their first named child is the value
	// call reports whether a statement is only a call, which removing it breaks.
	call func(f *file, n *sitter.Node) bool
	// removed replaces a removed call statement.
	removed string
	// tail is set where a body's last expression is its value (Rust).
	tail func(body *sitter.Node) *sitter.Node
	// body returns a function's body; nil for a function without one.
	body func(fn *sitter.Node) *sitter.Node
	// zero returns the zero value of a function's declared result type, or "" when it has none
	// that hadome mutate knows.
	zero func(f *file, fn *sitter.Node) string
}

func bodyField(fn *sitter.Node) *sitter.Node { return fn.ChildByFieldName("body") }

// Mutants makes the mutants of the given functions of a version. It reads syntax only and runs
// nothing. The order is that of the files' paths, then of the functions' lines, then of positions.
func (l *lang) Mutants(v engine.Version, funcs []facts.Symbol) ([]facts.Mutant, error) {
	byPath := map[string][]facts.Symbol{}
	for _, s := range funcs {
		if l.Claims(s.Path) && (s.Kind == facts.SymbolFunction || s.Kind == facts.SymbolMethod) {
			byPath[s.Path] = append(byPath[s.Path], s)
		}
	}
	paths := make([]string, 0, len(byPath))
	for p := range byPath {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	parser := sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(l.r.grammar()); err != nil {
		return nil, err
	}
	var out []facts.Mutant
	for _, p := range paths {
		src, err := fs.ReadFile(v.FS, p)
		if err != nil {
			return nil, err
		}
		tree := parser.Parse(src, nil)
		f := &file{path: p, src: src, root: tree.RootNode()}
		syms := byPath[p]
		sort.Slice(syms, func(i, j int) bool { return syms[i].Line < syms[j].Line })
		for _, s := range syms {
			fn := l.functionAt(f, s.Line)
			if fn == nil {
				continue
			}
			body := l.breaks.body(fn)
			if body == nil {
				continue
			}
			m := mutator{b: l.breaks, f: f, fn: s.ID, zero: l.breaks.zero(f, fn), seen: map[string]int{}}
			m.function(body)
			out = append(out, m.out...)
		}
		tree.Close()
	}
	return out, nil
}

// functionAt returns the outermost function that starts on a line, as the symbol's line names it.
func (l *lang) functionAt(f *file, at int) *sitter.Node {
	var found *sitter.Node
	walk(f.root, func(n *sitter.Node) bool {
		if found != nil || endLine(n) < at || line(n) > at {
			return false
		}
		if line(n) == at && l.breaks.functions[n.Kind()] {
			found = n
			return false
		}
		return true
	})
	return found
}

// mutator collects the mutants of one function.
type mutator struct {
	b    *breaks
	f    *file
	fn   string
	zero string
	seen map[string]int // anchor without its index -> how many so far
	out  []facts.Mutant
}

func (m *mutator) function(body *sitter.Node) {
	walk(body, func(n *sitter.Node) bool {
		kind := n.Kind()
		if n != body && (m.b.functions[kind] || m.b.nested[kind]) || strings.Contains(kind, "comment") {
			return false // a nested function is not broken, as with Go's function literals
		}
		m.node(n)
		return true
	})
	if m.b.tail != nil {
		m.returned(m.b.tail(body))
	}
}

// node makes the mutants of one node, without its children.
func (m *mutator) node(n *sitter.Node) {
	kind := n.Kind()
	switch {
	case m.b.binary[kind]:
		m.binary(n)
	case m.b.not[kind]:
		m.negation(n)
	case m.b.bools[kind]:
		if to, ok := m.b.literals[m.f.text(n)]; ok {
			m.add(negateBoolean, n.StartByte(), n.EndByte(), to)
		}
	case m.b.returns[kind]:
		m.returned(value(n))
	case m.b.call(m.f, n):
		m.add(removeCall, n.StartByte(), n.EndByte(), m.b.removed)
	}
}

// negation drops the operator of a negation: !x becomes x.
func (m *mutator) negation(n *sitter.Node) {
	op := n.Child(0)
	if op != nil && !op.IsNamed() && (m.f.text(op) == "!" || m.f.text(op) == "not") && n.ChildCount() > 1 {
		m.add(negateBoolean, op.StartByte(), n.Child(1).StartByte(), "")
	}
}

// returned breaks a returned value into the zero value, unless negating a boolean already breaks it
// or it is the zero value already.
func (m *mutator) returned(v *sitter.Node) {
	if v == nil || m.zero == "" {
		return
	}
	text := m.f.text(v)
	if _, boolean := m.b.literals[text]; boolean || text == m.zero {
		return
	}
	m.add(returnZero, v.StartByte(), v.EndByte(), m.zero)
}

// value is the first named child of n that is not a comment.
func value(n *sitter.Node) *sitter.Node {
	for _, c := range named(n) {
		if !strings.Contains(c.Kind(), "comment") {
			return c
		}
	}
	return nil
}

func (m *mutator) binary(n *sitter.Node) {
	for i := uint(0); i < n.ChildCount(); i++ {
		op := n.Child(i)
		if op.IsNamed() {
			continue
		}
		text := m.f.text(op)
		if to, ok := flipped[text]; ok {
			m.add(flipComparison, op.StartByte(), op.EndByte(), to)
		}
		if to, ok := swapped[text]; ok && !m.stringy(n) {
			m.add(swapArithmetic, op.StartByte(), op.EndByte(), to)
		}
	}
}

// stringy reports a string literal on either side: + on strings joins them.
func (m *mutator) stringy(n *sitter.Node) bool {
	for _, c := range named(n) {
		if m.b.strings[c.Kind()] {
			return true
		}
	}
	return false
}

func (m *mutator) add(kind string, start, end uint, replacement string) {
	row := uint(strings.Count(string(m.f.src[:start]), "\n"))
	key := m.fn + "|" + kind + "|" + tokens(m.f, row)
	m.seen[key]++
	m.out = append(m.out, facts.Mutant{
		Path: m.f.path, Line: int(row) + 1, Func: m.fn, Operator: kind,
		Original: string(m.f.src[start:end]), Replacement: replacement, Start: int(start), End: int(end),
		Anchor: fmt.Sprintf("%s|%d", key, m.seen[key]),
	})
}

// tokens is the text of the leaves that start on a row, comments left out, joined by one space: so
// reformatting the line or commenting it does not make its mutants new.
func tokens(f *file, row uint) string {
	var out []string
	walk(f.root, func(n *sitter.Node) bool {
		if n.EndPosition().Row < row || n.StartPosition().Row > row || strings.Contains(n.Kind(), "comment") {
			return false
		}
		if n.ChildCount() == 0 && n.StartPosition().Row == row {
			if t := strings.TrimSpace(f.text(n)); t != "" {
				out = append(out, t)
			}
		}
		return true
	})
	return strings.Join(out, " ")
}

// only reports whether a statement holds nothing but one node of a kind (and its semicolon).
func only(n *sitter.Node, kinds ...string) bool {
	v := value(n)
	return v != nil && n.NamedChildCount() == 1 && slices.Contains(kinds, v.Kind())
}
