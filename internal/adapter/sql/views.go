package sql

import (
	"regexp"
	"strings"
)

// withoutBodies returns s, for a product file, with the query of every view and table function in
// it replaced by SELECT 1, and the expression of every BigQuery SQL function by 1: the catalog uses
// none of them, and parsing them is most of the time spent reading SQL (docs/design/adapters-sql.md,
// "View queries" and "Function expressions").
//
// The replacement keeps the line breaks it replaces, the 1 on the line of the last token, so the
// lines the grammar counts are the piece's. The offset of every rune is kept in the statement, so
// the offsets are the piece's too.
func (d tableDialect) withoutBodies(s Statement) Statement {
	sc := scan(s.Text, d.lex)
	if sc.Unclosed {
		return s // where the statement ends is not known; the grammar reports the error
	}
	toks := sqlTokens(sc.Masked)
	w := rewrite{text: s.Text}
	done := 0 // the bytes of s.Text written so far
	for start := 0; start < len(toks); start = nextStatement(toks, start) {
		prefix := " SELECT "
		from, to := d.viewQuery(toks, start, len(s.Text))
		if to <= from {
			prefix = "" // the expression keeps its parentheses, and 1 goes between them
			from, to = d.functionExpr(toks, start)
		}
		last := lastToken(sc.Masked, from, to)
		if last < 0 {
			continue // no body, or one with no token: the grammar reads the statement whole
		}
		w.keep(done, from)
		w.stub(prefix, from, last, to)
		done = to
	}
	if w.offsets == nil {
		return s
	}
	w.keep(done, len(s.Text))
	return Statement{Text: w.b.String(), Start: s.Start, Line: s.Line, offsets: append(w.offsets, len(s.Text))}
}

// viewQuery finds the query of the statement starting at token start: the text after the first AS
// outside parentheses that follows the kind word, up to the next ; or the end of the text. Both are
// 0 when the statement is not a view or table function of the dialect, or has no such AS.
func (d tableDialect) viewQuery(toks []sqlToken, start, end int) (from, to int) {
	as, ok := firstAS(toks, d.kindWord(toks, start, d.viewHead))
	if !ok {
		return 0, 0
	}
	to = end
	if next := nextStatement(toks, as); toks[next-1].text == ";" {
		to = toks[next-1].start
	}
	return toks[as].end, to
}

// functionExpr finds the expression of the statement starting at token start: the text inside the
// parentheses that follow the first AS outside parentheses after the kind word, up to the one that
// closes them. Both are 0 when the statement is not a SQL function of the dialect, its AS is not
// followed by (, or no ) closes it before a ; or the end of the text.
func (d tableDialect) functionExpr(toks []sqlToken, start int) (from, to int) {
	as, ok := firstAS(toks, d.kindWord(toks, start, d.functionHead))
	if !ok || as+1 == len(toks) || toks[as+1].text != "(" {
		return 0, 0
	}
	depth := 0
	for i := as + 1; i < len(toks) && toks[i].text != ";"; i++ {
		switch toks[i].text {
		case "(":
			depth++
		case ")":
			depth--
			if depth == 0 {
				return toks[as+1].end, toks[i].start
			}
		}
	}
	return 0, 0
}

// firstAS is the index of the first AS outside parentheses after token kind and before the next ;.
// ok is false when there is none, or kind is -1.
func firstAS(toks []sqlToken, kind int) (as int, ok bool) {
	if kind < 0 {
		return 0, false
	}
	depth := 0
	for i := kind + 1; i < len(toks) && toks[i].text != ";"; i++ {
		switch t := toks[i].text; {
		case t == "(":
			depth++
		case t == ")":
			depth--
		case depth == 0 && strings.EqualFold(t, "AS"):
			return i, true
		}
	}
	return 0, false
}

// kindWord is the index of the kind word (VIEW or FUNCTION) of the statement starting at token
// start when head matches its head, and -1 otherwise or when head is nil.
func (d tableDialect) kindWord(toks []sqlToken, start int, head *regexp.Regexp) int {
	if head == nil {
		return -1
	}
	var words []string
	for i := start; i < len(toks) && toks[i].text != ";"; i++ {
		w := strings.ToUpper(toks[i].text)
		words = append(words, w)
		if w == "VIEW" || w == "FUNCTION" {
			if head.MatchString(strings.Join(words, " ")) {
				return i
			}
			return -1
		}
	}
	return -1
}

// lastToken is the offset of the last byte that is no blank in masked[from:to], or -1 when there
// is none.
func lastToken(masked string, from, to int) int {
	for i := to - 1; i >= from; i-- {
		if !isSpace(masked[i]) {
			return i
		}
	}
	return -1
}

// rewrite builds the text of a statement whose bodies are replaced, with the offset in the original
// text of each of its runes.
type rewrite struct {
	text    string
	b       strings.Builder
	offsets []int
}

// keep writes text[from:to] unchanged.
func (w *rewrite) keep(from, to int) {
	for i := range w.text[from:to] {
		w.offsets = append(w.offsets, from+i)
	}
	w.b.WriteString(w.text[from:to])
}

// stub writes, for the body text[from:to] whose last token ends at last, prefix, the line breaks
// before its last token, 1, and the line breaks after it. 1 stands at the body's last token, so the
// statement ends where the body did.
func (w *rewrite) stub(prefix string, from, last, to int) {
	w.b.WriteString(prefix)
	for range prefix {
		w.offsets = append(w.offsets, from)
	}
	w.lineBreaks(from, last)
	w.b.WriteByte('1')
	w.offsets = append(w.offsets, last)
	w.lineBreaks(last, to)
}

// lineBreaks writes the line breaks of text[from:to], each with its own offset.
func (w *rewrite) lineBreaks(from, to int) {
	for i := from; i < to; i++ {
		if w.text[i] == '\n' {
			w.b.WriteByte('\n')
			w.offsets = append(w.offsets, i)
		}
	}
}

// sqlToken is a word, a quoted name or string, or one punctuation byte of masked SQL text.
type sqlToken struct {
	text       string
	start, end int
}

// sqlTokens splits masked text (strings and comments blanked, quotes kept) into tokens. A quoted
// name is one token, so a ; or an AS inside it is not seen.
func sqlTokens(masked string) []sqlToken {
	var out []sqlToken
	for i := 0; i < len(masked); {
		c := masked[i]
		switch {
		case isSpace(c):
			i++
			continue
		case c == '"' || c == '`' || c == '\'':
			end, _ := quotedEnd(masked, i, c, false)
			out = append(out, sqlToken{masked[i:end], i, end})
			i = end
			continue
		case nameByte(c):
			j := i
			for j < len(masked) && nameByte(masked[j]) {
				j++
			}
			out = append(out, sqlToken{masked[i:j], i, j})
			i = j
			continue
		}
		out = append(out, sqlToken{masked[i : i+1], i, i + 1})
		i++
	}
	return out
}

// nextStatement is the index of the first token after the ; that ends the statement starting at
// token start, or len(toks).
func nextStatement(toks []sqlToken, start int) int {
	for i := start; i < len(toks); i++ {
		if toks[i].text == ";" {
			return i + 1
		}
	}
	return len(toks)
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v'
}

// nameByte is a byte of a keyword or an unquoted name; bytes of non-ASCII letters count.
func nameByte(c byte) bool {
	return c == '_' || c == '$' || c == '#' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' ||
		c >= 0x80
}
