package sql

import (
	"slices"
	"strings"
)

func (d tableDialect) References(t *Node) []Reference {
	r := refReader{d: d, hidden: d.hiddenNames(t), toks: t.Tokens(), at: map[int]int{}}
	for i, tk := range r.toks {
		r.at[tk.Start] = i
	}
	t.Walk(func(n *Node) bool {
		if n.Rule != "" {
			r.node(n)
		}
		return true
	})
	return r.out
}

// hiddenNames returns the names the tree's WITH entries define, folded.
func (d tableDialect) hiddenNames(t *Node) map[string]bool {
	hidden := map[string]bool{}
	for _, c := range d.rules.ctes {
		for _, n := range t.All(func(m *Node) bool { return m.Rule == c.Rule }) {
			if name := firstNamed(n, c.Name); name != nil {
				hidden[d.key(d.nameOf(name))] = true
			}
		}
	}
	return hidden
}

// refReader collects the references of one tree.
type refReader struct {
	d      tableDialect
	hidden map[string]bool
	toks   []*Node
	at     map[int]int // token start -> index in toks
	out    []Reference
}

// node adds the references a node makes: a call, or a relation.
func (r *refReader) node(n *Node) {
	if r.d.rules.callName != nil {
		if name, args, ok := r.d.rules.callName(n); ok {
			r.out = append(r.out, Reference{Name: r.d.nameOf(name), Use: UseCall, Args: args, Line: name.Line})
			return
		}
	}
	for _, c := range r.d.rules.calls {
		if name := firstNamed(n, c.Name); n.Rule == c.Rule && name != nil {
			r.out = append(r.out, Reference{Name: r.d.nameOf(name), Use: UseCall, Args: arguments(n, name), Line: name.Line})
		}
	}
	for _, rel := range r.d.rules.relations {
		if n.Rule == rel.Rule {
			r.relation(n, rel.Name)
		}
	}
}

// relation adds the relation a node names in its children of the name rules, unless a WITH entry
// of the same statement hides it. Its use is a write when the words before it write to it.
func (r *refReader) relation(n *Node, nameRules []string) {
	names := nameRun(n, nameRules)
	if len(names) == 0 {
		return
	}
	name := r.d.nameOf(names...)
	if len(name) == 1 && r.hidden[r.d.key(name)] {
		return
	}
	ref := Reference{Name: name, Use: UseRead, Line: names[0].Line}
	if i, ok := r.at[names[0].Start]; ok {
		if ev := writeEvent(r.toks, i); ev != "" {
			ref.Use, ref.Event = UseWrite, ev
		}
	}
	r.out = append(r.out, ref)
}

// nameRun returns the first run of children of n that are name nodes, with the dots between them.
func nameRun(n *Node, nameRules []string) []*Node {
	var names []*Node
	for _, ch := range n.Children {
		switch {
		case slices.Contains(nameRules, ch.Rule), len(names) > 0 && ch.Rule == "" && ch.Text == ".":
			names = append(names, ch)
		case len(names) > 0:
			return names
		}
	}
	return names
}

// key is a name folded, for comparing names written in the same string.
func (d tableDialect) key(name QualifiedName) string {
	parts := make([]string, len(name))
	for i, p := range name {
		parts[i] = d.fold(p)
	}
	return strings.Join(parts, ".")
}

// firstNamed returns the first child of n whose rule is one of names. Only children count: a call's
// name is never inside its arguments.
func firstNamed(n *Node, names []string) *Node {
	for _, c := range n.Children {
		if slices.Contains(names, c.Rule) {
			return c
		}
	}
	return nil
}

// writeWords are the words before a relation that make it a write, longest first.
var writeWords = []struct {
	before []string
	event  string
}{
	{[]string{"INSERT", "INTO"}, "INSERT"}, {[]string{"REPLACE", "INTO"}, "INSERT"}, {[]string{"IGNORE", "INTO"}, "INSERT"},
	{[]string{"DELETE", "FROM"}, "DELETE"}, {[]string{"MERGE", "INTO"}, "MERGE"}, {[]string{"TRUNCATE", "TABLE"}, "TRUNCATE"},
	{[]string{"INSERT"}, "INSERT"}, {[]string{"REPLACE"}, "INSERT"}, {[]string{"UPDATE"}, "UPDATE"},
	{[]string{"DELETE"}, "DELETE"}, {[]string{"MERGE"}, "MERGE"}, {[]string{"TRUNCATE"}, "TRUNCATE"},
}

// writeModifiers may stand between the write's words and the relation: UPDATE ONLY t, DELETE TOP (1) t.
var writeModifiers = map[string]bool{"ONLY": true, "IGNORE": true, "LOW_PRIORITY": true, "TOP": true}

// writeEvent returns the write a relation name at token i is the target of, from the words before
// it: INSERT INTO t, UPDATE t, DELETE FROM t, MERGE INTO t, REPLACE INTO t.
func writeEvent(toks []*Node, i int) string {
	if i > 0 && writeModifiers[strings.ToUpper(toks[i-1].Text)] {
		i--
	}
	for _, w := range writeWords {
		if n := len(w.before); i >= n && slices.Equal(words(toks[i-n:i]), w.before) {
			return w.event
		}
	}
	return ""
}

// arguments returns the texts of a call's arguments: the tokens after its name, inside its
// parentheses or, for EXEC without them, up to the end of the call, split at top-level commas.
func arguments(call, name *Node) []string {
	toks := tokensFrom(call, name.End)
	if len(toks) == 0 {
		return nil
	}
	if toks[0].Text != "(" {
		return splitArguments(toks, false)
	}
	return splitArguments(toks[1:], true)
}

// splitArguments joins tokens into arguments at the commas outside brackets. In parentheses, the
// closing parenthesis ends the list; otherwise the end of the statement does.
func splitArguments(toks []*Node, paren bool) []string {
	var out, cur []string
	flush := func() {
		if len(cur) > 0 {
			out = append(out, strings.Join(cur, " "))
		}
		cur = nil
	}
	depth := 0
	for _, t := range toks {
		switch {
		case t.Text == "(" || t.Text == "[":
			depth++
		case (t.Text == ")" || t.Text == "]") && depth == 0 && paren, t.Token == "EOF", t.Text == ";":
			flush()
			return out
		case t.Text == ")" || t.Text == "]":
			depth--
		case t.Text == "," && depth == 0:
			flush()
			continue
		}
		cur = append(cur, t.Text)
	}
	flush()
	return out
}
