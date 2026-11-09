package sql

import (
	"regexp"
	"strings"
)

// splitter says which client forms a dialect's files hold. Every form is found in the masked text,
// so none is seen inside a string or a comment.
type splitter struct {
	// commands match whole lines the client reads and the database never sees; they are blanked.
	commands *regexp.Regexp
	// keep matches lines that commands would match but that are SQL (SET TRANSACTION in Oracle).
	keep *regexp.Regexp
	// commandsAtStart is true when a command line is one only where a statement may start: after a
	// line ending in ; or a separator. SQL*Plus's SET is a command there, and part of an UPDATE
	// elsewhere.
	commandsAtStart bool
	// separator matches a whole line that ends one piece and starts the next (GO, /).
	separator *regexp.Regexp
	// delimiter is true for MySQL's DELIMITER, which changes what ends a statement.
	delimiter bool
	// copyData is true for psql's COPY … FROM stdin, whose data lines follow the statement up to \.
	copyData bool
}

// migrationDown marks where the part that undoes a migration starts in dbmate and goose files; up
// starts the part that applies it again.
var (
	migrationDown = regexp.MustCompile(`(?im)^\s*--\s*(migrate:down|\+goose\s+down)\b`)
	migrationUp   = regexp.MustCompile(`(?im)^\s*--\s*(migrate:up|\+goose\s+up)\b`)
	delimiterLine = regexp.MustCompile(`(?i)^\s*delimiter\s+(\S+)\s*$`)
	copyFromStdin = regexp.MustCompile(`(?i)^\s*COPY\b.*\bFROM\s+stdin\b.*;\s*$`)
)

// split blanks the client forms of src and cuts it at the separators.
func (s splitter) split(src string, lx lexical) []Statement {
	text := []byte(src)
	blankRange := func(from, to int) {
		for i := from; i < to; i++ {
			if text[i] != '\n' {
				text[i] = ' '
			}
		}
	}
	blankDownSections(src, blankRange)
	if s.copyData {
		blankCopyData(string(text), blankRange)
	}
	masked := scan(string(text), lx).Masked
	var cuts []int
	delim := ";"
	offset := 0
	atStart := true
	for _, ln := range strings.SplitAfter(masked, "\n") {
		body := strings.TrimRight(ln, "\r\n")
		end := offset + len(body)
		trimmed := strings.TrimSpace(body)
		switch {
		case s.delimiter && delimiterLine.MatchString(body):
			delim = delimiterLine.FindStringSubmatch(body)[1]
			blankRange(offset, end)
		case s.commands != nil && s.commands.MatchString(body) && (s.keep == nil || !s.keep.MatchString(body)) &&
			(!s.commandsAtStart || atStart):
			blankRange(offset, end)
		case s.separator != nil && s.separator.MatchString(body):
			blankRange(offset, end)
			cuts = append(cuts, offset)
			atStart = true
		case delim != ";":
			// A statement ended by the changed delimiter: the grammar reads ;, so the delimiter
			// becomes ; and spaces. A ; inside the body stays and is the body's own.
			for i := strings.Index(body, delim); i >= 0; {
				at := offset + i
				text[at] = ';'
				blankRange(at+1, at+len(delim))
				next := strings.Index(body[i+len(delim):], delim)
				if next < 0 {
					break
				}
				i += len(delim) + next
			}
		}
		if trimmed != "" {
			atStart = strings.HasSuffix(trimmed, ";") || (s.separator != nil && s.separator.MatchString(body)) ||
				(s.commands != nil && s.commands.MatchString(body) && atStart)
		}
		offset += len(ln)
	}
	return pieces(string(text), cuts)
}

// blankDownSections blanks the parts of dbmate and goose migrations that undo them.
func blankDownSections(src string, blank func(from, to int)) {
	for _, m := range migrationDown.FindAllStringIndex(src, -1) {
		end := len(src)
		if up := migrationUp.FindStringIndex(src[m[1]:]); up != nil {
			end = m[1] + up[0]
		}
		blank(m[0], end)
	}
}

// blankCopyData blanks the data lines after COPY … FROM stdin, up to and including the line \.
func blankCopyData(src string, blank func(from, to int)) {
	offset := 0
	copying := false
	for _, ln := range strings.SplitAfter(src, "\n") {
		body := strings.TrimRight(ln, "\r\n")
		switch {
		case copying:
			blank(offset, offset+len(body))
			copying = body != `\.`
		case copyFromStdin.MatchString(body):
			copying = true
		}
		offset += len(ln)
	}
}

// pieces cuts text at the offsets.
func pieces(text string, cuts []int) []Statement {
	var out []Statement
	start := 0
	for _, c := range append(cuts, len(text)) {
		if strings.TrimSpace(text[start:c]) != "" {
			out = append(out, Statement{Text: text[start:c], Start: start, Line: lineAt(text, start)})
		}
		start = c
	}
	return out
}
