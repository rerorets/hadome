package sql

import (
	"strings"
)

// withoutViewQueries returns s with the query of every view and table function in it replaced by
// SELECT 1, for a product file: the catalog uses only what comes before AS, and parsing the query
// is most of the time spent reading SQL (docs/design/adapters-sql.md, "View queries").
//
// The text keeps its length in bytes and its line breaks, so that every offset and line the grammar
// gives stays the original file's. SELECT goes where the query starts and 1 where its last token
// ends, so the statement still ends on its own last line.
func (d tableDialect) withoutViewQueries(s Statement) Statement {
	if d.viewHead == nil {
		return s
	}
	sc := scan(s.Text, d.lex)
	if sc.Unclosed {
		return s // where the statement ends is not known; the grammar reports the error
	}
	toks := sqlTokens(sc.Masked)
	b := []byte(s.Text)
	changed := false
	for start := 0; start < len(toks); start = nextStatement(toks, start) {
		from, to, ok := d.viewQuery(toks, start, len(b))
		if ok && replaceQuery(b, sc.Masked, from, to) {
			changed = true
		}
	}
	if !changed {
		return s
	}
	return Statement{Text: string(b), Start: s.Start, Line: s.Line}
}

// viewQuery finds the query of the statement starting at token start: the text after the first AS
// outside parentheses that follows the kind word, up to the next ; or the end of the text. ok is
// false when the statement is not a view or table function of the dialect, or has no such AS.
func (d tableDialect) viewQuery(toks []sqlToken, start, end int) (from, to int, ok bool) {
	kind := d.viewKind(toks, start)
	if kind < 0 {
		return 0, 0, false
	}
	depth := 0
	for i := kind + 1; i < len(toks) && toks[i].text != ";"; i++ {
		switch t := toks[i].text; {
		case t == "(":
			depth++
		case t == ")":
			depth--
		case depth == 0 && strings.EqualFold(t, "AS"):
			to = end
			if next := nextStatement(toks, i); toks[next-1].text == ";" {
				to = toks[next-1].start
			}
			return toks[i].end, to, true
		}
	}
	return 0, 0, false
}

// viewKind is the index of the kind word (VIEW or FUNCTION) of the statement starting at token
// start when its head is one of the dialect's views or table functions, and -1 otherwise.
func (d tableDialect) viewKind(toks []sqlToken, start int) int {
	var head []string
	for i := start; i < len(toks) && toks[i].text != ";"; i++ {
		w := strings.ToUpper(toks[i].text)
		head = append(head, w)
		if w == "VIEW" || w == "FUNCTION" {
			if d.viewHead.MatchString(strings.Join(head, " ")) {
				return i
			}
			return -1
		}
	}
	return -1
}

// replaceQuery blanks b[from:to] but its line breaks, and writes SELECT at the first place in it
// with room on one line and 1 at the last byte of its last token. It reports false, and leaves b
// as it was, when the range holds no token or has no room for SELECT before its last token: such a
// statement is read whole by the grammar.
func replaceQuery(b []byte, masked string, from, to int) bool {
	last := -1
	for i := to - 1; i >= from; i-- {
		if !isSpace(masked[i]) {
			last = i
			break
		}
	}
	if last < 0 {
		return false
	}
	at := -1
	for p := from + 1; p+len("SELECT") < last; p++ { // from + 1: a space stays after AS
		if !strings.Contains(string(b[p:p+len("SELECT")]), "\n") {
			at = p
			break
		}
	}
	if at < 0 {
		return false
	}
	for i := from; i < to; i++ {
		if b[i] != '\n' {
			b[i] = ' '
		}
	}
	copy(b[at:], "SELECT")
	b[last] = '1'
	return true
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
