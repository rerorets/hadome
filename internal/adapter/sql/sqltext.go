package sql

import (
	"regexp"
	"slices"
	"strings"

	"github.com/rerorets/hadome/internal/facts"
)

// firstWord returns the first word of text outside comments.
func firstWord(text string, lx lexical) string {
	masked := scan(text, lx).Masked
	s := strings.TrimLeft(masked, " \t\r\n(")
	end := strings.IndexFunc(s, func(r rune) bool {
		return !(r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z')
	})
	if end < 0 {
		return s
	}
	return s[:end]
}

var jdbcCall = regexp.MustCompile(`(?is)^\s*\{\s*(\?\s*=\s*)?call\s+(.*?)\s*\}\s*$`)

// callEscape turns JDBC's {call p(?)} into the dialect's call statement.
func callEscape(text, dialect string) string {
	m := jdbcCall.FindStringSubmatch(text)
	if m == nil {
		return text
	}
	if dialect == "tsql" {
		return "EXEC " + m[2]
	}
	return "CALL " + m[2]
}

var driverParam = regexp.MustCompile(`\$\d+|\?|%\(\w+\)s|%s|@\w+|:[A-Za-z_]\w*`)

// prepare turns the holes of the host language and the drivers' parameters ($1, ?, :name, @p,
// %s) into a value, 0, so that the grammar reads them where a value goes. A hole where a name goes
// then does not read, which is what makes the string unresolved.
func prepare(text string, holes []int, d tableDialect) string {
	return replaceParams(fillHoles(text, holes, d.lex), d)
}

// fillHoles puts 0 at each hole outside string literals; a hole inside a literal is part of it.
func fillHoles(text string, holes []int, lx lexical) string {
	masked := scan(text, lx).Masked
	var sb strings.Builder
	last := 0
	for _, h := range sortedHoles(holes, len(text)) {
		if insideString(masked, h) {
			continue
		}
		sb.WriteString(text[last:h])
		sb.WriteString(" 0 ")
		last = h
	}
	sb.WriteString(text[last:])
	return sb.String()
}

func sortedHoles(holes []int, n int) []int {
	var out []int
	for _, h := range holes {
		if h >= 0 && h <= n && !slices.Contains(out, h) {
			out = append(out, h)
		}
	}
	slices.Sort(out)
	return out
}

// replaceParams turns the drivers' parameters outside strings into 0, except where the dialect
// writes the same form in its own syntax.
func replaceParams(text string, d tableDialect) string {
	masked := scan(text, d.lex).Masked
	var sb strings.Builder
	last := 0
	for _, m := range driverParam.FindAllStringIndex(masked, -1) {
		if ownSyntax(masked, m[0], m[1], d.name) {
			continue
		}
		sb.WriteString(text[last:m[0]])
		sb.WriteString("0")
		last = m[1]
	}
	sb.WriteString(text[last:])
	return sb.String()
}

// ownSyntax reports whether the parameter-like text masked[start:end] is the dialect's own syntax:
// PostgreSQL's jsonb ?| and ?& operators, a :: cast or a:b, and the @name variables of SQL Server
// and MySQL.
func ownSyntax(masked string, start, end int, dialect string) bool {
	p := masked[start:end]
	switch p[0] {
	case '?':
		return end < len(masked) && (masked[end] == '|' || masked[end] == '&')
	case ':':
		return start > 0 && (masked[start-1] == ':' || isWordByte(masked[start-1]))
	case '@':
		return dialect == "tsql" || dialect == "mysql"
	}
	return false
}

func isWordByte(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// insideString reports whether offset i is inside a string literal: an odd number of quote
// characters stands before it in the masked text, where string bodies are blank.
func insideString(masked string, i int) bool {
	quotes := strings.Count(masked[:min(i, len(masked))], "'")
	return quotes%2 == 1
}

var wordRe = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_$]*`)

// mentions reports whether text holds a word equal to the last part of one of the database's object
// names.
func mentions(db *database, d tableDialect, text string) bool {
	names := map[string]bool{}
	for k := range db.objects {
		names[lastPart(k)] = true
	}
	for _, w := range wordRe.FindAllString(text, -1) {
		if names[d.fold(Ident{Text: w})] {
			return true
		}
	}
	return false
}

// hazardsIn finds the dialect's hazards among the calls and keywords of a tree.
func hazardsIn(d tableDialect, t *Node, tx facts.Text) []facts.Hazard {
	var out []facts.Hazard
	add := func(cat, evidence string) {
		h := facts.Hazard{Category: facts.HazardCategory(cat), Path: tx.Path, Line: tx.Line, Evidence: evidence}
		for _, have := range out {
			if have == h {
				return
			}
		}
		out = append(out, h)
	}
	calls := map[string]bool{}
	for _, r := range d.References(t) {
		if r.Use == UseCall {
			calls[d.key(r.Name)] = true
		}
	}
	toks := words(t.Tokens())
	for _, h := range d.hazards {
		if h.Call != "" {
			parts := splitName(h.Call)
			if calls[d.key(parts)] {
				add(h.Category, h.Call)
			}
			continue
		}
		for i := range toks {
			if i+len(h.Keywords) <= len(toks) && equalWords(toks[i:i+len(h.Keywords)], h.Keywords) {
				add(h.Category, strings.Join(h.Keywords, " "))
				break
			}
		}
	}
	return out
}

func equalWords(a, b []string) bool {
	for i := range b {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
