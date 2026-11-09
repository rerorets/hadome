package sql

import (
	"slices"
	"strings"
)

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
	switch verb := strings.ToUpper(toks[0].Text); verb {
	case "EXEC", "EXECUTE":
		return d.spRename(stmt, toks)
	case "RENAME":
		return d.renameTables(stmt)
	case "CREATE", "ALTER", "DROP", "REPLACE", "COMMENT":
		return d.objectStatement(stmt, verb, toks[0].End)
	}
	return nil
}

// objectStatement reads a CREATE, ALTER, DROP or COMMENT of the object named first after its verb.
func (d tableDialect) objectStatement(stmt *Node, verb string, afterVerb int) []Definition {
	name := d.headName(stmt, afterVerb)
	if name == nil {
		return nil
	}
	head := words(tokensBefore(stmt, name[0].Start))
	kind, temp := headKind(head)
	def := Definition{Kind: kind, Effect: EffectCreate, Name: d.nameOf(name...), Temp: temp, Line: stmt.Line,
		Start: stmt.Start, EndLine: lastLine(stmt)}
	afterName := name[len(name)-1].End
	switch verb {
	case "DROP":
		return d.drops(stmt, def)
	case "COMMENT":
		def.Effect = EffectDoc
		return []Definition{def}
	case "ALTER":
		def.Effect = d.alterEffect(stmt, head, afterName, &def)
	}
	if def.Effect != EffectCreate {
		return []Definition{def}
	}
	def.Params = d.params(stmt)
	if kind == KindTrigger {
		d.describeTrigger(stmt, afterName, &def)
	}
	if kind == KindPackage {
		return append([]Definition{def}, d.packageRoutines(stmt, def)...)
	}
	return []Definition{def}
}

// alterEffect is what an ALTER does: CREATE OR ALTER, and ALTER of a routine in a dialect where that
// redefines it (SQL Server), create; RENAME and SET SCHEMA rename; anything else keeps the object.
func (d tableDialect) alterEffect(stmt *Node, head []string, afterName int, def *Definition) Effect {
	if slices.Contains(head, "CREATE") || d.alterRedefines && objectKinds[def.Kind].Symbol == "routine" ||
		d.alterRedefines && def.Kind == KindView {
		return EffectCreate
	}
	if newName, ok := d.renamed(stmt, afterName); ok {
		def.NewName = newName
		return EffectRename
	}
	return EffectAlter
}

// params returns the parameter names of the statement's routine, not those of routines inside it.
func (d tableDialect) params(stmt *Node) []string {
	var out []string
	for _, p := range stmt.All(func(n *Node) bool { return slices.Contains(d.rules.params, n.Rule) }) {
		if p.Ancestor(d.rules.statements...) == stmt {
			out = append(out, paramName(p))
		}
	}
	return out
}

// describeTrigger reads a trigger's events, its table and the routine it executes. The events are
// written before the body: AS, BEGIN, EXECUTE, FOR EACH ROW, WHEN.
func (d tableDialect) describeTrigger(stmt *Node, afterName int, def *Definition) {
	toks := tokensFrom(stmt, afterName)
	inHead := true
	for i, t := range toks {
		switch w := strings.ToUpper(t.Text); w {
		case "AS", "BEGIN", "ROW", "WHEN":
			inHead = false
		case "INSERT", "UPDATE", "DELETE":
			if inHead && !slices.Contains(def.Events, w) {
				def.Events = append(def.Events, w)
			}
		case "ON":
			if def.On == nil {
				def.On = d.nameAfter(stmt, t.End)
			}
		case "EXECUTE":
			inHead = false
			if def.Executes == nil && i+1 < len(toks) {
				def.Executes = d.nameAfter(stmt, toks[i+1].End)
			}
		}
	}
}

// nameAfter returns the first name node's name after offset, or nil.
func (d tableDialect) nameAfter(stmt *Node, offset int) QualifiedName {
	if n := d.headName(stmt, offset); n != nil {
		return d.nameOf(n...)
	}
	return nil
}

// tokensFrom returns the tokens of stmt that start at or after offset.
func tokensFrom(stmt *Node, offset int) []*Node {
	toks := stmt.Tokens()
	for i, t := range toks {
		if t.Start >= offset {
			return toks[i:]
		}
	}
	return nil
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

// renameKeywords follow RENAME when a column, constraint or index is renamed, not the object.
var renameKeywords = map[string]bool{"COLUMN": true, "CONSTRAINT": true, "INDEX": true, "KEY": true}

// renamed finds RENAME TO name, RENAME name (where the dialect allows it), or SET SCHEMA schema
// after the object's name.
func (d tableDialect) renamed(stmt *Node, after int) (QualifiedName, bool) {
	toks := tokensFrom(stmt, after)
	word := func(i int) string {
		if i < len(toks) {
			return strings.ToUpper(toks[i].Text)
		}
		return ""
	}
	for i := range toks {
		w, next := word(i), word(i+1)
		switch {
		case w == "RENAME" && (next == "TO" || next == "AS") && i+2 < len(toks):
			return d.nameFrom(stmt, toks[i+1].End), true
		case w == "RENAME" && d.renameWithoutTo && next != "" && !renameKeywords[next]:
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
