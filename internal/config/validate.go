package config

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
)

// Error is one problem in a configuration file, written so that an agent can fix it without
// reading the specification.
type Error struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Key     string `json:"key"`
	Problem string `json:"problem"`
	Allowed string `json:"allowed,omitempty"`
	Example string `json:"example,omitempty"`
}

// Error formats the problem with what is allowed and an example on the following lines.
func (e Error) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s:%d: %s: %s", e.File, e.Line, e.Key, e.Problem)
	if e.Allowed != "" {
		b.WriteString("\n  allowed: " + e.Allowed)
	}
	if e.Example != "" {
		b.WriteString("\n  example: " + strings.ReplaceAll(e.Example, "\n", "\n           "))
	}
	return b.String()
}

// Errors are all the problems found in a configuration. A configuration with any error never runs a check.
type Errors []Error

// Error formats every problem, one after another.
func (es Errors) Error() string {
	parts := make([]string, len(es))
	for i, e := range es {
		parts[i] = e.Error()
	}
	return strings.Join(parts, "\n")
}

// validator walks a syntax tree against the specification and builds plain values from it:
// map[string]any, []any, string and int.
type validator struct {
	file  string
	errs  Errors
	lines map[string]int // path -> line, for errors found after validation
}

func (v *validator) fail(n ast.Node, path, problem string, k Key) {
	v.errs = append(v.errs, Error{File: v.file, Line: line(n), Key: displayPath(path), Problem: problem,
		Allowed: allowedText(k), Example: k.Example})
}

func line(n ast.Node) int {
	if n == nil || n.GetToken() == nil {
		return 1
	}
	return n.GetToken().Position.Line
}

func displayPath(p string) string {
	if p == "" {
		return "(top level)"
	}
	return p
}

func (v *validator) value(n ast.Node, path string, k Key) any {
	v.lines[path] = line(n)
	if t, ok := n.(*ast.TagNode); ok {
		v.fail(n, path, "YAML tags are not accepted", k)
		return v.value(t.Value, path, k)
	}
	switch k.Kind {
	case KindObject:
		return v.object(n, path, k)
	case KindList:
		return v.list(n, path, k)
	case KindInt:
		return v.integer(n, path, k)
	case KindAny:
		return anyValue(n)
	default:
		return v.str(n, path, k)
	}
}

func (v *validator) str(n ast.Node, path string, k Key) any {
	var s string
	switch n := n.(type) {
	case *ast.StringNode:
		s = n.Value
	case *ast.LiteralNode:
		s = n.Value.Value
	default:
		v.fail(n, path, "must be a string, got "+describe(n), k)
		return nil
	}
	if !allowed(k, s) {
		v.fail(n, path, fmt.Sprintf("%q is not allowed", s), k)
	} else if k.Pattern != "" && !regexp.MustCompile(k.Pattern).MatchString(s) {
		v.fail(n, path, fmt.Sprintf("%q does not match %s", s, k.Pattern), k)
	} else if _, err := regexp.Compile(s); k.Regexp && err != nil {
		v.fail(n, path, "is not a valid regular expression: "+err.Error(), k)
	}
	return s
}

func (v *validator) integer(n ast.Node, path string, k Key) any {
	in, ok := n.(*ast.IntegerNode)
	if !ok {
		v.fail(n, path, "must be an integer, got "+describe(n), k)
		return nil
	}
	i, err := strconv.Atoi(in.GetToken().Value)
	if err != nil {
		v.fail(n, path, "must be a decimal integer", k)
		return nil
	}
	if !allowed(k, strconv.Itoa(i)) {
		v.fail(n, path, fmt.Sprintf("%d is not allowed", i), k)
	} else if i < k.Min {
		v.fail(n, path, fmt.Sprintf("must be at least %d, got %d", k.Min, i), k)
	}
	return i
}

func (v *validator) list(n ast.Node, path string, k Key) any {
	seq, ok := n.(*ast.SequenceNode)
	if !ok {
		v.fail(n, path, "must be a list, got "+describe(n), k)
		return nil
	}
	if len(seq.Values) < k.MinItems {
		v.fail(n, path, fmt.Sprintf("needs at least %d entries, got %d", k.MinItems, len(seq.Values)), k)
	}
	out := make([]any, 0, len(seq.Values))
	for i, el := range seq.Values {
		elem := *k.Elem
		if elem.Example == "" {
			elem.Example = k.Example
		}
		out = append(out, v.value(el, fmt.Sprintf("%s[%d]", path, i), elem))
	}
	return out
}

// anyValue turns a syntax tree into plain values without checking it.
func anyValue(n ast.Node) any {
	switch n := n.(type) {
	case *ast.MappingNode, *ast.MappingValueNode:
		pairs, _ := entries(n)
		m := map[string]any{}
		for _, p := range pairs {
			m[p.Key.GetToken().Value] = anyValue(p.Value)
		}
		return m
	case *ast.SequenceNode:
		out := make([]any, len(n.Values))
		for i, el := range n.Values {
			out[i] = anyValue(el)
		}
		return out
	case *ast.NullNode:
		return nil
	case *ast.TagNode:
		return anyValue(n.Value)
	default:
		var v any
		if err := yaml.NodeToValue(n, &v); err == nil {
			return v
		}
		return n.String()
	}
}

// entries returns the key-value pairs of a mapping, or false if the node is not a mapping.
func entries(n ast.Node) ([]*ast.MappingValueNode, bool) {
	switch n := n.(type) {
	case *ast.MappingNode:
		return n.Values, true
	case *ast.MappingValueNode:
		return []*ast.MappingValueNode{n}, true
	}
	return nil, false
}

func (v *validator) object(n ast.Node, path string, k Key) any {
	pairs, ok := entries(n)
	if !ok {
		v.fail(n, path, "must be a mapping of keys to values, got "+describe(n), k)
		return nil
	}
	fields, exactlyOne := k.Fields, []string(nil)
	if len(k.Variants) > 0 {
		var ok bool
		fields, exactlyOne, ok = v.variant(n, pairs, path, k)
		if !ok {
			return nil
		}
	}
	byName := map[string]Key{}
	for _, f := range fields {
		byName[f.Name] = f
	}
	out := map[string]any{}
	seen := map[string]bool{}
	for _, p := range pairs {
		name := p.Key.GetToken().Value
		f, known := byName[name]
		sub := joinPath(path, name)
		switch {
		case !known:
			v.fail(p.Key, sub, "unknown key"+didYouMean(name, fields), Key{Allowed: fieldNames(fields), Example: k.Example})
		case seen[name]:
			v.fail(p.Key, sub, "appears twice", f)
		default:
			seen[name] = true
			if val := v.value(p.Value, sub, f); val != nil {
				out[name] = val
			}
		}
	}
	v.required(n, path, fields, seen)
	v.exactlyOneOf(n, path, k, exactlyOne, seen)
	return out
}

func (v *validator) required(n ast.Node, path string, fields []Key, seen map[string]bool) {
	for _, f := range fields {
		if f.Required && !seen[f.Name] {
			v.fail(n, joinPath(path, f.Name), "is required", f)
		}
	}
}

func (v *validator) exactlyOneOf(n ast.Node, path string, k Key, names []string, seen map[string]bool) {
	if len(names) == 0 {
		return
	}
	var present []string
	for _, name := range names {
		if seen[name] {
			present = append(present, name)
		}
	}
	if len(present) != 1 {
		v.fail(n, path, fmt.Sprintf("needs exactly one of %s, got %d", strings.Join(names, ", "), len(present)), k)
	}
}

// variant picks the shape of an object from its discriminator key.
func (v *validator) variant(n ast.Node, pairs []*ast.MappingValueNode, path string, k Key) ([]Key, []string, bool) {
	var match []Variant
	for _, vr := range k.Variants {
		for _, p := range pairs {
			if p.Key.GetToken().Value == vr.Discriminator {
				match = append(match, vr)
				break
			}
		}
	}
	if len(match) == 1 {
		return match[0].Fields, match[0].ExactlyOne, true
	}
	var shapes []string
	for _, vr := range k.Variants {
		shapes = append(shapes, fmt.Sprintf("%s (has `%s`)", vr.Name, vr.Discriminator))
	}
	v.fail(n, path, "must be exactly one of: "+strings.Join(shapes, ", "), k)
	return nil, nil, false
}

func joinPath(path, name string) string {
	if path == "" {
		return name
	}
	return path + "." + name
}

func fieldNames(fields []Key) []Value {
	out := make([]Value, len(fields))
	for i, f := range fields {
		out[i] = Value{Name: f.Name}
	}
	return out
}

func allowed(k Key, s string) bool {
	if len(k.Allowed) == 0 {
		return true
	}
	for _, a := range k.Allowed {
		if a.Name == s {
			return true
		}
	}
	return false
}

func allowedText(k Key) string {
	if len(k.Allowed) == 0 {
		return ""
	}
	names := make([]string, len(k.Allowed))
	for i, a := range k.Allowed {
		names[i] = a.Name
	}
	return strings.Join(names, ", ")
}

func describe(n ast.Node) string {
	switch n.(type) {
	case *ast.NullNode:
		return "nothing"
	case *ast.StringNode, *ast.LiteralNode:
		return "a string"
	case *ast.IntegerNode:
		return "an integer"
	case *ast.FloatNode:
		return "a number with a fraction"
	case *ast.BoolNode:
		return "true or false"
	case *ast.SequenceNode:
		return "a list"
	case *ast.MappingNode, *ast.MappingValueNode:
		return "a mapping"
	}
	return "a " + n.Type().String()
}

// didYouMean suggests the known key closest to a misspelt one, when one is close enough to be a typo.
func didYouMean(name string, fields []Key) string {
	best, bestDist := "", 3
	for _, f := range fields {
		if d := distance(name, f.Name); d < bestDist {
			best, bestDist = f.Name, d
		}
	}
	if best == "" {
		return ""
	}
	return "; did you mean " + best + "?"
}

// distance is the Levenshtein distance between two short strings.
func distance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}
