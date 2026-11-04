//go:build cgo

package treesitter

import (
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/rerorets/hadome/internal/facts"
)

// dynamicCall is a call that imports whatever its first argument names when the code runs.
type dynamicCall struct {
	at *sitter.Node
	// spec is the argument when it is a string literal; empty when the destination is unknown.
	spec string
}

// dynamicCalls finds the calls of a file whose callee, as written (Class.forName,
// importlib.import_module), is one of callees. interpolates, when set, reports a string literal
// whose text substitutes values, for a grammar that keeps that inside the literal's text.
func dynamicCalls(f *file, kinds, callees map[string]bool, interpolates func(string) bool) []dynamicCall {
	var out []dynamicCall
	walk(f.root, func(n *sitter.Node) bool {
		if !kinds[n.Kind()] {
			return true
		}
		callee := strings.Join(strings.Fields(strings.SplitN(f.text(n), "(", 2)[0]), "")
		arg := firstArgument(n)
		if !callees[callee] || arg == nil {
			return true
		}
		c := dynamicCall{at: n}
		if literal(arg) && (interpolates == nil || !interpolates(f.text(arg))) {
			c.spec = strings.Trim(f.text(arg), `"'`+"`")
		}
		out = append(out, c)
		return true
	})
	return out
}

// unknownImport is the fact for a dynamic import whose destination is decided at run time.
func unknownImport(f *file, at *sitter.Node) facts.Import {
	return facts.Import{Path: f.path, Line: line(at), FromUnit: f.unit, Unknown: true,
		Text: strings.Join(strings.Fields(f.text(at)), " ")}
}

// firstArgument returns the first argument of a call in any of the five grammars.
func firstArgument(call *sitter.Node) *sitter.Node {
	var args *sitter.Node
	walk(call, func(n *sitter.Node) bool {
		switch n.Kind() {
		case "arguments", "argument_list", "value_arguments":
			if args == nil {
				args = n
			}
			return false
		}
		return args == nil
	})
	if args == nil || args.NamedChildCount() == 0 {
		return nil
	}
	a := args.NamedChild(0)
	if a.Kind() == "value_argument" && a.NamedChildCount() > 0 { // Kotlin wraps each argument
		a = a.NamedChild(0)
	}
	return a
}

// literal reports a string written out in full: no interpolation, substitution or concatenation.
func literal(n *sitter.Node) bool {
	if !strings.Contains(n.Kind(), "string") {
		return false
	}
	fixed := true
	walk(n, func(c *sitter.Node) bool {
		if k := c.Kind(); strings.Contains(k, "interpolat") || strings.Contains(k, "substitution") {
			fixed = false
		}
		return fixed
	})
	return fixed
}
