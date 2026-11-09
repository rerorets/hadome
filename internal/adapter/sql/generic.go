package sql

import (
	"regexp"
	"slices"
	"strings"
)

// tableDialect is a Dialect described by tables: which rules of its grammar are statements, names,
// calls and relations. Every dialect is one value of it, with its own grammar and tables.
type tableDialect struct {
	name     string
	grammar  Grammar
	lex      lexical
	splitter splitter
	markers  []*regexp.Regexp
	fold     func(Ident) string
	builtins Builtins
	hazards  []HazardEntry
	rules    rules
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

func (d tableDialect) Parse(s Statement) (*Node, error) {
	return d.grammar.Parse(s.Text, s.Start, s.Line)
}

func (d tableDialect) Fold(id Ident) string { return d.fold(id) }

func (d tableDialect) Builtins() Builtins { return d.builtins }

func (d tableDialect) Hazards() []HazardEntry { return d.hazards }

// statements returns the statement nodes of the tree in order, those inside blocks included.
func (d tableDialect) statements(t *Node) []*Node {
	var out []*Node
	for _, s := range t.All(func(n *Node) bool { return slices.Contains(d.rules.statements, n.Rule) }) {
		out = append(out, s)
		if blk := s.Find(func(n *Node) bool { return slices.Contains(d.rules.blocks, n.Rule) }); blk != nil {
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
	i := slices.Index(sibs, first)
	for j := i + 1; j+1 < len(sibs) && sibs[j].Rule == "" && sibs[j].Text == "." && slices.Contains(d.rules.names, sibs[j+1].Rule); j += 2 {
		out = append(out, sibs[j], sibs[j+1])
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
			body := s[i+1 : min(end, len(s))]
			if c == '`' && strings.Contains(body, ".") {
				for _, p := range strings.Split(body, ".") {
					out = append(out, Ident{Text: p, Quoted: true})
				}
			} else {
				out = append(out, Ident{Text: body, Quoted: true})
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

// kindWords map the words of a statement's head to object kinds. Two-word kinds are looked up
// first.
var kindWords = map[string]ObjectKind{
	"FUNCTION": KindFunction, "AGGREGATE": KindFunction, "PROCEDURE": KindProcedure, "PROC": KindProcedure,
	"VIEW": KindView, "TABLE": KindTable, "SEQUENCE": KindSequence, "TRIGGER": KindTrigger,
	"PACKAGE": KindPackage, "TASK": KindTask, "STREAM": KindStream,
	"INDEX": KindOther, "TYPE": KindOther, "DOMAIN": KindOther, "SCHEMA": KindOther, "DATABASE": KindOther,
	"EXTENSION": KindOther, "ROLE": KindOther, "USER": KindOther, "POLICY": KindOther, "STAGE": KindOther,
	"PIPE": KindOther, "RULE": KindOther, "SYNONYM": KindOther, "EVENT": KindOther, "SERVER": KindOther,
	"COLLATION": KindOther, "CAST": KindOther, "OPERATOR": KindOther, "LANGUAGE": KindOther,
	"PUBLICATION": KindOther, "SUBSCRIPTION": KindOther, "WAREHOUSE": KindOther, "CONSTRAINT": KindOther,
}

var kindPairs = map[[2]string]ObjectKind{
	{"MATERIALIZED", "VIEW"}: KindMatView, {"PACKAGE", "BODY"}: KindPackageBody, {"TABLE", "FUNCTION"}: KindFunction,
	{"DYNAMIC", "TABLE"}: KindMatView, {"EXTERNAL", "TABLE"}: KindTable, {"FILE", "FORMAT"}: KindOther,
	{"FOREIGN", "TABLE"}: KindTable, {"MATERIALIZED", "TABLE"}: KindMatView,
}

// headKind reads the object kind and whether the object is temporary from the words of a head.
// The last kind word wins: CREATE CONSTRAINT TRIGGER is a trigger.
func headKind(words []string) (ObjectKind, bool) {
	kind, temp := KindOther, false
	for i := 0; i < len(words); i++ {
		w := words[i]
		switch w {
		case "TEMP", "TEMPORARY", "VOLATILE":
			temp = true
		}
		if i+1 < len(words) {
			if k, ok := kindPairs[[2]string{w, words[i+1]}]; ok {
				kind = k
				i++
				continue
			}
		}
		if k, ok := kindWords[w]; ok {
			kind = k
		}
	}
	return kind, temp
}

// words returns the upper-cased texts of tokens.
func words(toks []*Node) []string {
	out := make([]string, len(toks))
	for i, t := range toks {
		out[i] = strings.ToUpper(t.Text)
	}
	return out
}

// tokensBefore returns the tokens of stmt that start before offset.
func tokensBefore(stmt *Node, offset int) []*Node {
	var out []*Node
	for _, t := range stmt.Tokens() {
		if t.Start >= offset {
			break
		}
		out = append(out, t)
	}
	return out
}

func (d tableDialect) Definitions(t *Node) []Definition {
	var out []Definition
	for _, s := range d.statements(t) {
		out = append(out, d.definitions(s)...)
	}
	return out
}

// definitions reads what one statement does to objects from its head: its first words, its kind
// word and its name.
func (d tableDialect) definitions(stmt *Node) []Definition {
	toks := stmt.Tokens()
	if len(toks) == 0 {
		return nil
	}
	all := words(toks)
	verb := all[0]
	if verb == "EXEC" || verb == "EXECUTE" {
		return d.spRename(stmt, toks)
	}
	if verb == "RENAME" {
		return d.renameTables(stmt)
	}
	if verb != "CREATE" && verb != "ALTER" && verb != "DROP" && verb != "REPLACE" && verb != "COMMENT" {
		return nil
	}
	name := d.headName(stmt, toks[0].End)
	if name == nil {
		return nil
	}
	head := words(tokensBefore(stmt, name[0].Start))
	kind, temp := headKind(head)
	def := Definition{Kind: kind, Name: d.nameOf(name...), Temp: temp, Line: stmt.Line, Start: stmt.Start,
		EndLine: lastLine(stmt)}
	switch verb {
	case "DROP":
		return d.drops(stmt, def)
	case "COMMENT":
		def.Effect = EffectDoc
		return []Definition{def}
	case "ALTER":
		def.Effect = EffectAlter
		if newName, ok := d.renamed(stmt, name[len(name)-1].End); ok {
			def.Effect, def.NewName = EffectRename, newName
		}
		if slices.Contains(head, "CREATE") || d.name == "tsql" && (kind == KindProcedure || kind == KindFunction ||
			kind == KindView || kind == KindTrigger) {
			def.Effect = EffectCreate // T-SQL's ALTER PROCEDURE and CREATE OR ALTER redefine the object
		}
	default:
		def.Effect = EffectCreate
	}
	if def.Effect == EffectCreate {
		d.describe(stmt, name[len(name)-1].End, &def)
	}
	out := []Definition{def}
	if kind == KindPackage && def.Effect == EffectCreate {
		out = append(out, d.packageRoutines(stmt, def)...)
	}
	return out
}

// describe fills in what a created object's statement says beyond its name: parameters, and a
// trigger's events, table and routine.
func (d tableDialect) describe(stmt *Node, afterName int, def *Definition) {
	for _, p := range stmt.All(func(n *Node) bool { return slices.Contains(d.rules.params, n.Rule) }) {
		if body := p.Ancestor(d.rules.statements...); body != stmt {
			continue
		}
		def.Params = append(def.Params, paramName(p))
	}
	if def.Kind != KindTrigger {
		return
	}
	toks := stmt.Tokens()
	inHead := true // the events are written before the body: AS, BEGIN, EXECUTE, FOR EACH ROW, WHEN
	for i, t := range toks {
		if t.Start < afterName {
			continue
		}
		w := strings.ToUpper(t.Text)
		switch w {
		case "AS", "BEGIN", "ROW", "WHEN":
			inHead = false
		case "INSERT", "UPDATE", "DELETE":
			if inHead && !slices.Contains(def.Events, w) {
				def.Events = append(def.Events, w)
			}
		case "ON":
			if def.On == nil {
				if n := d.headName(stmt, t.End); n != nil {
					def.On = d.nameOf(n...)
				}
			}
		case "EXECUTE":
			inHead = false
			if def.Executes == nil && i+1 < len(toks) {
				if n := d.headName(stmt, toks[i+1].End); n != nil {
					def.Executes = d.nameOf(n...)
				}
			}
		}
	}
}

// paramName is the first word of a parameter that is not a mode (IN, OUT, INOUT, VARIADIC).
func paramName(p *Node) string {
	for _, t := range p.Tokens() {
		switch strings.ToUpper(t.Text) {
		case "IN", "OUT", "INOUT", "VARIADIC":
			continue
		}
		return strings.TrimLeft(t.Text, "@")
	}
	return ""
}

// renamed finds RENAME TO name, RENAME name, or SET SCHEMA schema after the object's name.
func (d tableDialect) renamed(stmt *Node, after int) (QualifiedName, bool) {
	toks := stmt.Tokens()
	for i, t := range toks {
		if t.Start < after {
			continue
		}
		w := strings.ToUpper(t.Text)
		next := ""
		if i+1 < len(toks) {
			next = strings.ToUpper(toks[i+1].Text)
		}
		switch {
		case w == "RENAME" && (next == "TO" || next == "AS"):
			if i+2 < len(toks) {
				return d.nameFrom(stmt, toks[i+1].End), true
			}
		case w == "RENAME" && next != "COLUMN" && next != "CONSTRAINT" && next != "INDEX" && next != "KEY" && i+1 < len(toks) &&
			d.name != "postgresql":
			return d.nameFrom(stmt, toks[i].End), true
		case w == "SET" && next == "SCHEMA" && i+2 < len(toks):
			return QualifiedName{{Text: toks[i+2].Text}, {}}, true
		}
	}
	return nil, false
}

// nameFrom reads the name that starts after offset: a name node, or else the plain tokens up to the
// end of the statement or the next word that is not part of a name.
func (d tableDialect) nameFrom(stmt *Node, offset int) QualifiedName {
	if n := d.headName(stmt, offset); n != nil {
		return d.nameOf(n...)
	}
	var sb strings.Builder
	for _, t := range stmt.Tokens() {
		if t.Start < offset {
			continue
		}
		if t.Text == ";" || t.Text == "," || t.Token == "EOF" {
			break
		}
		sb.WriteString(t.Text)
	}
	return splitName(sb.String())
}

// drops returns a drop of every name the DROP statement lists.
func (d tableDialect) drops(stmt *Node, def Definition) []Definition {
	var out []Definition
	toks := stmt.Tokens()
	after := toks[0].End
	for {
		n := d.headName(stmt, after)
		if n == nil {
			break
		}
		def := def
		def.Effect, def.Name = EffectDrop, d.nameOf(n...)
		out = append(out, def)
		after = n[len(n)-1].End
		// A drop names more objects only after a comma.
		comma := false
		for _, t := range toks {
			if t.Start >= after {
				comma = t.Text == ","
				break
			}
		}
		if !comma {
			break
		}
	}
	return out
}

// renameTables reads MySQL's RENAME TABLE a TO b, c TO d.
func (d tableDialect) renameTables(stmt *Node) []Definition {
	var out []Definition
	toks := stmt.Tokens()
	after := toks[0].End
	for {
		from := d.headName(stmt, after)
		if from == nil {
			break
		}
		to := d.headName(stmt, from[len(from)-1].End)
		if to == nil {
			break
		}
		out = append(out, Definition{Kind: KindTable, Effect: EffectRename, Name: d.nameOf(from...), NewName: d.nameOf(to...),
			Line: stmt.Line, Start: stmt.Start, EndLine: lastLine(stmt)})
		after = to[len(to)-1].End
	}
	return out
}

// spRename reads SQL Server's EXEC sp_rename 'old', 'new' for objects (not columns or indexes).
func (d tableDialect) spRename(stmt *Node, toks []*Node) []Definition {
	if d.name != "tsql" || len(toks) < 4 {
		return nil
	}
	var args []string
	named := false
	for _, t := range toks[1:] {
		switch {
		case strings.EqualFold(t.Text, "sp_rename") || strings.EqualFold(t.Text, "dbo") || t.Text == ".":
			named = named || strings.EqualFold(t.Text, "sp_rename")
		case strings.HasPrefix(t.Text, "'") || strings.HasPrefix(t.Text, "N'"):
			args = append(args, strings.Trim(strings.TrimPrefix(t.Text, "N"), "'"))
		}
	}
	if !named || len(args) < 2 || len(args) > 2 && !strings.EqualFold(args[2], "OBJECT") {
		return nil
	}
	return []Definition{{Kind: KindOther, Effect: EffectRename, Name: splitName(args[0]), NewName: splitName(args[1]),
		Line: stmt.Line, Start: stmt.Start, EndLine: lastLine(stmt)}}
}

// packageRoutines returns the routines an Oracle package specification declares, as
// schema.package.routine.
func (d tableDialect) packageRoutines(stmt *Node, pkg Definition) []Definition {
	var out []Definition
	for _, r := range d.rules.packageRoutines {
		for _, n := range stmt.All(func(m *Node) bool { return m.Rule == r.Rule }) {
			var name *Node
			for _, c := range n.Children {
				if slices.Contains(r.Name, c.Rule) {
					name = c
					break
				}
			}
			if name == nil {
				continue
			}
			def := Definition{Kind: KindProcedure, Effect: EffectCreate, Package: true, Line: n.Line, Start: n.Start,
				EndLine: lastLine(n), Name: append(slices.Clone(pkg.Name), d.nameOf(name)...)}
			if strings.EqualFold(n.Tokens()[0].Text, "FUNCTION") {
				def.Kind = KindFunction
			}
			for _, p := range n.All(func(m *Node) bool { return slices.Contains(d.rules.params, m.Rule) }) {
				def.Params = append(def.Params, paramName(p))
			}
			out = append(out, def)
		}
	}
	return out
}

func lastLine(n *Node) int {
	toks := n.Tokens()
	for i := len(toks) - 1; i >= 0; i-- {
		if toks[i].Token != "EOF" {
			return toks[i].Line + strings.Count(toks[i].Text, "\n")
		}
	}
	return n.Line
}

func (d tableDialect) References(t *Node) []Reference {
	hidden := map[string]bool{}
	for _, c := range d.rules.ctes {
		for _, n := range t.All(func(m *Node) bool { return m.Rule == c.Rule }) {
			for _, ch := range n.Children {
				if slices.Contains(c.Name, ch.Rule) {
					hidden[d.key(d.nameOf(ch))] = true
					break
				}
			}
		}
	}
	toks := t.Tokens()
	at := map[int]int{} // token start -> index
	for i, tk := range toks {
		at[tk.Start] = i
	}
	var out []Reference
	t.Walk(func(n *Node) bool {
		if n.Rule == "" {
			return true
		}
		if d.rules.callName != nil {
			if name, args, ok := d.rules.callName(n); ok {
				out = append(out, Reference{Name: d.nameOf(name), Use: UseCall, Args: args, Line: name.Line})
				return true
			}
		}
		for _, c := range d.rules.calls {
			if n.Rule != c.Rule {
				continue
			}
			name := firstNamed(n, c.Name)
			if name == nil {
				continue
			}
			out = append(out, Reference{Name: d.nameOf(name), Use: UseCall, Args: arguments(n, name), Line: name.Line})
		}
		for _, r := range d.rules.relations {
			if n.Rule != r.Rule {
				continue
			}
			var names []*Node
			for _, ch := range n.Children {
				if slices.Contains(r.Name, ch.Rule) || (len(names) > 0 && ch.Rule == "" && ch.Text == ".") {
					names = append(names, ch)
				} else if len(names) > 0 {
					break
				}
			}
			if len(names) == 0 {
				continue
			}
			name := d.nameOf(names...)
			if len(name) == 1 && hidden[d.key(name)] {
				continue
			}
			ref := Reference{Name: name, Use: UseRead, Line: names[0].Line}
			if i, ok := at[names[0].Start]; ok {
				if ev := writeEvent(toks, i); ev != "" {
					ref.Use, ref.Event = UseWrite, ev
				}
			}
			out = append(out, ref)
		}
		return true
	})
	return out
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

// writeEvent returns the write a relation name at token i is the target of, from the words before
// it: INSERT INTO t, UPDATE t, DELETE FROM t, MERGE INTO t, REPLACE INTO t.
func writeEvent(toks []*Node, i int) string {
	prev := func(k int) string {
		if i-k < 0 {
			return ""
		}
		return strings.ToUpper(toks[i-k].Text)
	}
	p1, p2 := prev(1), prev(2)
	if p1 == "ONLY" || p1 == "IGNORE" || p1 == "LOW_PRIORITY" || p1 == "TOP" {
		p1, p2 = p2, prev(3)
	}
	switch {
	case p1 == "INTO" && (p2 == "INSERT" || p2 == "REPLACE" || p2 == "IGNORE"):
		return "INSERT"
	case p1 == "INSERT" || p1 == "REPLACE":
		return "INSERT"
	case p1 == "UPDATE":
		return "UPDATE"
	case p1 == "FROM" && p2 == "DELETE", p1 == "DELETE":
		return "DELETE"
	case p1 == "INTO" && p2 == "MERGE", p1 == "MERGE":
		return "MERGE"
	case p1 == "TRUNCATE" || p1 == "TABLE" && p2 == "TRUNCATE":
		return "TRUNCATE"
	}
	return ""
}

// arguments returns the texts of a call's arguments: the tokens after its name, inside its
// parentheses or, for EXEC without them, up to the end of the call, split at top-level commas.
func arguments(call, name *Node) []string {
	var toks []*Node
	for _, t := range call.Tokens() {
		if t.Start >= name.End {
			toks = append(toks, t)
		}
	}
	if len(toks) == 0 {
		return nil
	}
	paren := toks[0].Text == "("
	if paren {
		toks = toks[1:]
	}
	var out []string
	var cur strings.Builder
	depth := 0
	for _, t := range toks {
		switch t.Text {
		case "(", "[":
			depth++
		case ")", "]":
			if depth == 0 && paren {
				if s := strings.TrimSpace(cur.String()); s != "" {
					out = append(out, s)
				}
				return out
			}
			depth--
		case ",":
			if depth == 0 {
				out = append(out, strings.TrimSpace(cur.String()))
				cur.Reset()
				continue
			}
		}
		if t.Token == "EOF" || t.Text == ";" {
			break
		}
		if cur.Len() > 0 {
			cur.WriteByte(' ')
		}
		cur.WriteString(t.Text)
	}
	if s := strings.TrimSpace(cur.String()); s != "" {
		out = append(out, s)
	}
	return out
}
