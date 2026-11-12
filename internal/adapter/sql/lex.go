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

// comment is a comment found by scan: its line and its text without its delimiters.
type comment struct {
	Line int
	Text string
}

// scanned is a file with its strings and comments blanked: same length, newlines kept. Markers and
// client commands are searched in Masked.
type scanned struct {
	Masked   string
	Comments []comment
	// Unclosed is true when a string, a quoted name or a block comment runs to the end unclosed.
	Unclosed bool
}

// scan blanks the strings and comments of src. Quote characters and dollar tags stay, so that a
// marker such as $$ is still seen; what is between them does not.
func scan(src string, lx lexical) scanned {
	s := &scanner{src: src, b: []byte(src), lx: lx}
	for i := 0; i < len(src); {
		i = s.next(i)
	}
	return scanned{Masked: string(s.b), Comments: s.comments, Unclosed: s.unclosed}
}

type scanner struct {
	src      string
	b        []byte // src with what has been found blanked
	lx       lexical
	comments []comment
	unclosed bool
	// line is the line at offset at; the scan only moves forward, so lines are counted once.
	line, at int
}

// lineOf returns the line of offset i, at or after the last offset asked for.
func (s *scanner) lineOf(i int) int {
	if s.line == 0 {
		s.line = 1
	}
	s.line += strings.Count(s.src[s.at:i], "\n")
	s.at = i
	return s.line
}

// next handles what starts at offset i and returns the offset after it.
func (s *scanner) next(i int) int {
	if end, ok := s.comment(i); ok {
		return end
	}
	if end, ok := s.literal(i); ok {
		return end
	}
	return i + 1
}

// comment handles a comment starting at i.
func (s *scanner) comment(i int) (int, bool) {
	rest := s.src[i:]
	switch {
	case strings.HasPrefix(rest, "--"):
		return s.lineComment(i, 2), true
	case s.lx.hashComments && rest[0] == '#':
		return s.lineComment(i, 1), true
	case strings.HasPrefix(rest, "/*"):
		end := blockEnd(s.src, i, s.lx.nestedBlocks)
		s.closed(end-i >= len("/**/") && strings.HasSuffix(s.src[:end], "*/"))
		s.comments = append(s.comments, comment{Line: s.lineOf(i), Text: strings.TrimSuffix(s.src[i+2:end], "*/")})
		s.blank(i, end)
		return end, true
	}
	return 0, false
}

// literal handles a string or a quoted name starting at i.
func (s *scanner) literal(i int) (int, bool) {
	rest := s.src[i:]
	c := rest[0]
	switch {
	case s.lx.tripleQuotes && isTripleQuote(rest):
		return s.enclosed(i, 3, rest[:3]), true
	case s.lx.qQuotes && isQQuote(rest):
		return s.enclosed(i, 3, string(qClosing(rest[2]))+"'"), true
	case c == '\'' || s.lx.doubleStrings && c == '"':
		end, closed := quotedEnd(s.src, i, c, s.escapes(i))
		s.closed(closed)
		if closed {
			s.blank(i+1, end-1)
		} else {
			s.blank(i+1, end)
		}
		return end, true
	case c == '"' || c == '`':
		end, closed := quotedEnd(s.src, i, c, false)
		s.closed(closed)
		return end, true
	case s.lx.dollarQuotes && c == '$':
		if tag, ok := dollarTag(rest); ok {
			return s.enclosed(i, len(tag), tag), true
		}
	}
	return 0, false
}

// escapes reports whether \ escapes a quote in the string starting at i: always in dialects with
// backslash escapes, and in PostgreSQL's E'…' strings.
func (s *scanner) escapes(i int) bool {
	if s.lx.backslash {
		return true
	}
	return s.src[i] == '\'' && i > 0 && (s.src[i-1] == 'E' || s.src[i-1] == 'e')
}

func isTripleQuote(rest string) bool {
	return strings.HasPrefix(rest, "'''") || strings.HasPrefix(rest, `"""`)
}

func isQQuote(rest string) bool {
	return len(rest) > 2 && (rest[0] == 'q' || rest[0] == 'Q') && rest[1] == '\''
}

// lineComment records and blanks the comment starting at i, whose delimiter is width bytes.
func (s *scanner) lineComment(i, width int) int {
	end := strings.IndexByte(s.src[i:], '\n')
	if end < 0 {
		end = len(s.src) - i
	}
	s.comments = append(s.comments, comment{Line: s.lineOf(i), Text: s.src[i+width : i+end]})
	s.blank(i, i+end)
	return i + end
}

// enclosed blanks the body of a string that opens with width bytes at i and closes with closer. A
// string not closed runs to the end.
func (s *scanner) enclosed(i, width int, closer string) int {
	end := strings.Index(s.src[i+width:], closer)
	if end < 0 {
		s.closed(false)
		s.blank(i+width, len(s.src))
		return len(s.src)
	}
	body := i + width + end
	s.blank(i+width, body)
	return body + len(closer)
}

// closed notes whether the string, quoted name or comment just found closes.
func (s *scanner) closed(ok bool) {
	if !ok {
		s.unclosed = true
	}
}

func (s *scanner) blank(from, to int) {
	for i := from; i < to && i < len(s.b); i++ {
		if s.b[i] != '\n' {
			s.b[i] = ' '
		}
	}
}

// qClosing is the character that closes Oracle's q'… string opened with open.
func qClosing(open byte) byte {
	if c, ok := map[byte]byte{'[': ']', '{': '}', '(': ')', '<': '>'}[open]; ok {
		return c
	}
	return open
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

// quotedEnd returns the offset after the quoted text starting at i, and whether a quote closes it:
// a doubled quote is part of it, and with backslash so is an escaped one.
func quotedEnd(src string, i int, q byte, backslash bool) (int, bool) {
	for j := i + 1; j < len(src); j++ {
		switch {
		case backslash && src[j] == '\\':
			j++
		case src[j] == q:
			if j+1 < len(src) && src[j+1] == q {
				j++
				continue
			}
			return j + 1, true
		}
	}
	return len(src), false
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
