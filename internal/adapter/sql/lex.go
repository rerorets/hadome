package sql

import "strings"

// lexical says how a dialect writes strings and comments, so that markers, client separators and
// comments are found only outside them.
type lexical struct {
	hashComments  bool // MySQL's # comments
	nestedBlocks  bool // PostgreSQL's nested /* /* */ */
	dollarQuotes  bool // $$ … $$ and $tag$ … $tag$
	doubleStrings bool // "…" is a string, not a name (MySQL, BigQuery)
	backslash     bool // \ escapes in '…' (MySQL, BigQuery)
	tripleQuotes  bool // '''…''' and """…""" (BigQuery)
	qQuotes       bool // Oracle's q'[…]'
}

// span is a comment, or a string body, found by scan.
type span struct {
	Start, End int // byte offsets, End exclusive
	Line       int
	Text       string // a comment's text without its delimiters
}

// scanned is a file with its strings and comments blanked: same length, newlines kept. Markers and
// client commands are searched in Masked.
type scanned struct {
	Masked   string
	Comments []span
}

// scan blanks the strings and comments of src. Quote characters and dollar tags stay, so that a
// marker such as $$ is still seen; what is between them does not.
func scan(src string, lx lexical) scanned {
	b := []byte(src)
	var comments []span
	line := 1
	blank := func(from, to int) {
		for i := from; i < to && i < len(b); i++ {
			if b[i] != '\n' {
				b[i] = ' '
			}
		}
	}
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == '\n':
			line++
			i++
		case c == '-' && strings.HasPrefix(src[i:], "--"), lx.hashComments && c == '#':
			end := strings.IndexByte(src[i:], '\n')
			if end < 0 {
				end = len(src) - i
			}
			start := 2
			if c == '#' {
				start = 1
			}
			comments = append(comments, span{Start: i, End: i + end, Line: line, Text: src[i+start : i+end]})
			blank(i, i+end)
			i += end
		case c == '/' && strings.HasPrefix(src[i:], "/*"):
			end := blockEnd(src, i, lx.nestedBlocks)
			comments = append(comments, span{Start: i, End: end, Line: line, Text: strings.TrimSuffix(src[i+2:end], "*/")})
			line += strings.Count(src[i:end], "\n")
			blank(i, end)
			i = end
		case lx.tripleQuotes && (strings.HasPrefix(src[i:], `'''`) || strings.HasPrefix(src[i:], `"""`)):
			q := src[i : i+3]
			end := strings.Index(src[i+3:], q)
			if end < 0 {
				end = len(src) - i - 3
			}
			body := i + 3 + end
			line += strings.Count(src[i:body], "\n")
			blank(i+3, body)
			i = min(body+3, len(src))
		case lx.qQuotes && (c == 'q' || c == 'Q') && i+2 < len(src) && src[i+1] == '\'':
			open := src[i+2]
			closing := map[byte]byte{'[': ']', '{': '}', '(': ')', '<': '>'}[open]
			if closing == 0 {
				closing = open
			}
			end := strings.Index(src[i+3:], string(closing)+"'")
			if end < 0 {
				end = len(src) - i - 3
			}
			body := i + 3 + end
			line += strings.Count(src[i:body], "\n")
			blank(i+3, body)
			i = min(body+2, len(src))
		case c == '\'' || (lx.doubleStrings && c == '"'):
			end := quotedEnd(src, i, c, lx.backslash || (c == '\'' && i > 0 && (src[i-1] == 'E' || src[i-1] == 'e')))
			line += strings.Count(src[i:end], "\n")
			blank(i+1, end-1)
			i = end
		case c == '"' || c == '`':
			end := quotedEnd(src, i, c, false)
			line += strings.Count(src[i:end], "\n")
			i = end
		case lx.dollarQuotes && c == '$':
			tag, ok := dollarTag(src[i:])
			if !ok {
				i++
				continue
			}
			end := strings.Index(src[i+len(tag):], tag)
			if end < 0 {
				end = len(src) - i - len(tag)
			}
			body := i + len(tag) + end
			line += strings.Count(src[i:body], "\n")
			blank(i+len(tag), body)
			i = min(body+len(tag), len(src))
		default:
			i++
		}
	}
	return scanned{Masked: string(b), Comments: comments}
}

// blockEnd returns the offset after the block comment starting at i.
func blockEnd(src string, i int, nested bool) int {
	depth := 0
	for j := i; j < len(src)-1; j++ {
		switch {
		case src[j] == '/' && src[j+1] == '*':
			if depth == 0 || nested {
				depth++
			}
			j++
		case src[j] == '*' && src[j+1] == '/':
			depth--
			j++
			if depth == 0 {
				return j + 1
			}
		}
	}
	return len(src)
}

// quotedEnd returns the offset after the quoted text starting at i: a doubled quote is part of it,
// and with backslash so is an escaped one.
func quotedEnd(src string, i int, q byte, backslash bool) int {
	for j := i + 1; j < len(src); j++ {
		switch {
		case backslash && src[j] == '\\':
			j++
		case src[j] == q:
			if j+1 < len(src) && src[j+1] == q {
				j++
				continue
			}
			return j + 1
		}
	}
	return len(src)
}

// dollarTag returns the tag ($$ or $name$) that starts s.
func dollarTag(s string) (string, bool) {
	for j := 1; j < len(s); j++ {
		c := s[j]
		switch {
		case c == '$':
			return s[:j+1], true
		case c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || (j > 1 && c >= '0' && c <= '9'):
		default:
			return "", false
		}
	}
	return "", false
}

// lineAt returns the 1-based line of offset in src.
func lineAt(src string, offset int) int {
	return strings.Count(src[:min(offset, len(src))], "\n") + 1
}
