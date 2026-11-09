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
	w := &splitWork{text: []byte(src), delim: ";", atStart: true}
	blankDownSections(src, w.blank)
	if s.copyData {
		blankCopyData(string(w.text), w.blank)
	}
	offset := 0
	for _, ln := range strings.SplitAfter(scan(string(w.text), lx).Masked, "\n") {
		body := strings.TrimRight(ln, "\r\n")
		s.line(w, body, offset)
		if trimmed := strings.TrimSpace(body); trimmed != "" {
			w.atStart = strings.HasSuffix(trimmed, ";") || s.isSeparator(body) || s.isCommand(body) && w.atStart
		}
		offset += len(ln)
	}
	return pieces(string(w.text), w.cuts)
}

// splitWork is the state of one split: the text being blanked, where it is cut, the delimiter in
// force, and whether the next line may start a statement.
type splitWork struct {
	text    []byte
	cuts    []int
	delim   string
	atStart bool
}

func (w *splitWork) blank(from, to int) {
	for i := from; i < to; i++ {
		if w.text[i] != '\n' {
			w.text[i] = ' '
		}
	}
}

// line handles one line of the masked text, which starts at offset.
func (s splitter) line(w *splitWork, body string, offset int) {
	end := offset + len(body)
	switch {
	case s.delimiter && delimiterLine.MatchString(body):
		w.delim = delimiterLine.FindStringSubmatch(body)[1]
		w.blank(offset, end)
	case s.isCommand(body) && (!s.commandsAtStart || w.atStart):
		w.blank(offset, end)
	case s.isSeparator(body):
		w.blank(offset, end)
		w.cuts = append(w.cuts, offset)
	case w.delim != ";":
		// A statement ended by the changed delimiter: the grammar reads ;, so the delimiter becomes ;
		// and spaces. A ; inside the body stays and is the body's own.
		for i := 0; ; {
			next := strings.Index(body[i:], w.delim)
			if next < 0 {
				break
			}
			at := offset + i + next
			w.text[at] = ';'
			w.blank(at+1, at+len(w.delim))
			i += next + len(w.delim)
		}
	}
}

func (s splitter) isCommand(body string) bool {
	return s.commands != nil && s.commands.MatchString(body) && (s.keep == nil || !s.keep.MatchString(body))
}

func (s splitter) isSeparator(body string) bool {
	return s.separator != nil && s.separator.MatchString(body)
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
