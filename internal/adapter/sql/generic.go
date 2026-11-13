package sql

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// tableDialect is a Dialect described by tables: which rules of its grammar are statements, names,
// calls and relations. Every dialect is one value of it, with its own grammar and tables.
type tableDialect struct {
	name     string
	lex      lexical
	splitter splitter
	markers  []*regexp.Regexp
	fold     func(Ident) string
	builtins Builtins
	hazards  []HazardEntry
	rules    rules
	// alterRedefines is true where ALTER PROCEDURE, FUNCTION, VIEW or TRIGGER writes the object anew
	// (SQL Server), so it is a create.
	alterRedefines bool
	// renameWithoutTo is true where ALTER TABLE t RENAME t2 renames the table (MySQL). Elsewhere
	// RENAME a TO b may rename a column.
	renameWithoutTo bool
	// viewHead matches the head of a view or table function whose query a product file does not
	// parse: its tokens from CREATE to the kind word, upper-cased and joined by one space. Nil where
	// the query's end cannot be told from its tokens (SQL Server, Oracle).
	viewHead *regexp.Regexp
	// functionHead matches, in the same form, the head of a SQL function whose expression a product
	// file does not parse. Nil where no function's body is an expression in parentheses.
	functionHead *regexp.Regexp
}

// rules name the parts of a dialect's grammar that definitions and names are found in.
type rules struct {
	// statements are the rules of one statement. Statements are not searched for below a statement,
	// except inside blocks.
	statements []string
	// blocks are statements that hold statements of their own (BEGIN … END scripts).
	blocks []string
	// names are the rules that write an object's name in a statement's head.
	names []string
	// params are the rules of a routine's parameter names.
	params []string
	// calls are the rules of a call, each with the rules of the name it calls.
	calls []callRule
	// relations are the rules that name a relation, each with the rules of the name.
	relations []nameRule
	// ctes are the rules of a WITH entry, with the rule of its name.
	ctes []nameRule
	// packageRoutines are the rules of a routine declared in an Oracle package specification, with
	// the rule of its name.
	packageRoutines []nameRule
	// callName reads the name and arguments of a call written in a way callRule cannot say.
	callName func(*Node) (*Node, []string, bool)
}

// callRule is a call: Rule's node calls the name its first child (or descendant) of Name holds.
type callRule struct {
	Rule string
	Name []string
}

// nameRule is a node of Rule whose name is in its direct children of Name.
type nameRule struct {
	Rule string
	Name []string
}

func (d tableDialect) Name() string { return d.name }

func (d tableDialect) Markers(src []byte) []Marker {
	masked := scan(string(src), d.lex).Masked
	var out []Marker
	for _, re := range d.markers {
		for _, m := range re.FindAllStringIndex(masked, -1) {
			out = append(out, Marker{Line: lineAt(masked, m[0]), Text: strings.TrimSpace(string(src[m[0]:m[1]]))})
		}
	}
	return out
}

func (d tableDialect) Split(src []byte) []Statement { return d.splitter.split(string(src), d.lex) }

// Parse reads a piece with the dialect's grammar. A dialect that is not enabled has no grammar in
// a build without -tags sqlgrammars, so it reads nothing.
func (d tableDialect) Parse(s Statement) (*Node, error) {
	g, ok := grammars[d.name]
	if !ok {
		return nil, fmt.Errorf("this build of hadome has no grammar for %s", d.name)
	}
	if m, ok := g.(antlrGrammar); ok && s.offsets != nil {
		return m.parseAt(s.Text, s.offsets, s.Start, s.Line)
	}
	return g.Parse(s.Text, s.Start, s.Line)
}

func (d tableDialect) Fold(id Ident) string { return d.fold(id) }

func (d tableDialect) Builtins() Builtins { return d.builtins }

func (d tableDialect) Hazards() []HazardEntry { return d.hazards }

// statements returns the statement nodes of the tree in order, those inside blocks included.
func (d tableDialect) statements(t *Node) []*Node {
	var out []*Node
	for _, s := range t.All(func(n *Node) bool { return slices.Contains(d.rules.statements, n.Rule) }) {
		out = append(out, s)
		if toks := s.Tokens(); len(toks) > 0 && strings.EqualFold(toks[0].Text, "CREATE") {
			continue // a routine's body runs when it is called, not when its file is applied
		}
		for _, blk := range s.All(func(n *Node) bool { return slices.Contains(d.rules.blocks, n.Rule) }) {
			out = append(out, d.statements(blk)...)
		}
	}
	return out
}

// headName returns the node that writes the statement's object name: the first name node, and the
// nodes and dots right after it that continue it (schema_name . table_name).
func (d tableDialect) headName(stmt *Node, after int) []*Node {
	first := stmt.Find(func(n *Node) bool {
		return n.Start >= after && n.Rule != "" && slices.Contains(d.rules.names, n.Rule) && n.End > n.Start
	})
	if first == nil || first.Parent == nil {
		return nil
	}
	out := []*Node{first}
	sibs := first.Parent.Children
	rest := sibs[slices.Index(sibs, first)+1:]
	for len(rest) >= 2 && rest[0].Rule == "" && rest[0].Text == "." && slices.Contains(d.rules.names, rest[1].Rule) {
		out = append(out, rest[0], rest[1])
		rest = rest[2:]
	}
	return out
}

// nameOf reads a name from nodes: their tokens joined, then split at the dots outside quotes.
func (d tableDialect) nameOf(nodes ...*Node) QualifiedName {
	var sb strings.Builder
	for _, n := range nodes {
		for _, t := range n.Tokens() {
			sb.WriteString(t.Text)
		}
	}
	return splitName(sb.String())
}

// splitName splits a written name at the dots outside quotes. A quoted part keeps its case; a
// BigQuery part `proj.ds.t` quoted whole is split at its dots.
func splitName(s string) QualifiedName {
	var out QualifiedName
	for i := 0; i < len(s); {
		switch c := s[i]; c {
		case '.':
			i++
		case '"', '`', '[':
			closing := c
			if c == '[' {
				closing = ']'
			}
			end := i + 1
			for end < len(s) && s[end] != closing {
				end++
			}
			parts := []string{s[i+1 : min(end, len(s))]}
			if c == '`' {
				parts = strings.Split(parts[0], ".")
			}
			for _, p := range parts {
				out = append(out, Ident{Text: p, Quoted: true})
			}
			i = end + 1
		default:
			end := i
			for end < len(s) && s[end] != '.' {
				end++
			}
			out = append(out, Ident{Text: s[i:end]})
			i = end
		}
	}
	return out
}
