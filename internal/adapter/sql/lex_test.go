package sql

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// viewpoint: behaviour
// target: sql.scan
// scan blanks what is inside strings and comments, keeping the length, the newlines, the quotes and
// the dollar tags, by the dialect's rules for writing them; it returns each comment's line and text.
// Something that is not closed runs to the end of the file. In the expected text, · is a blank.
func TestScanBlanksStringsAndComments(t *testing.T) {
	pg := lexical{nestedBlocks: true, dollarQuotes: true}
	my := lexical{hashComments: true, doubleStrings: true, backslash: true}
	bq := lexical{hashComments: true, doubleStrings: true, backslash: true, tripleQuotes: true}
	ora := lexical{qQuotes: true}
	cases := []struct {
		name     string
		lx       lexical
		src      string
		masked   string
		comments []string
	}{
		{"a line comment", pg, "a--x\nb", "a···\nb", []string{"1:x"}},
		{"a line comment at the end", pg, "a--xy", "a····", []string{"1:xy"}},
		{"a nested block comment", pg, "/*a/*b*/c*/d", "···········d", []string{"1:a/*b*/c"}},
		{"a block comment that does not nest", lexical{}, "/*a/*b*/c*/d", "········c*/d", []string{"1:a/*b"}},
		{"a block comment not closed", pg, "x/*ab", "x····", []string{"1:ab"}},
		{"a hash comment", my, "a#x\nb", "a··\nb", []string{"1:x"}},
		{"a hash outside MySQL", pg, "a#x\nb", "a#x\nb", nil},
		{"the lines of comments", pg, "a\n--x\n/*y\n*/--z", "a\n···\n···\n·····", []string{"2:x", "3:y\n", "4:z"}},
		{"a doubled quote", pg, "'ab''c'd", "'·····'d", nil},
		{"an escape in E''", pg, `E'a\'b'c`, "E'····'c", nil},
		{"an escape in e''", pg, `e'a\'b'c`, "e'····'c", nil},
		{"no escape in ''", pg, `x'a\'b`, "x'··'b", nil},
		{"an escape where backslashes escape", my, `'a\'b'c`, "'····'c", nil},
		{"a string not closed", pg, "'abc", "'···", nil},
		{"a double-quoted string", my, `"a'b"c`, `"···"c`, nil},
		{"a double-quoted name", pg, `"a'b"c`, `"a'b"c`, nil},
		{"a backquoted name", my, "`a'b`c", "`a'b`c", nil},
		{"a comment inside a string", pg, "'--x'", "'···'", nil},
		{"triple quotes", bq, `'''a'b'''c`, `'''···'''c`, nil},
		{"triple double quotes", bq, `"""a"b"""c`, `"""···"""c`, nil},
		{"triple quotes not closed", bq, `'''ab`, `'''··`, nil},
		{"q-quotes with brackets", ora, "q'[a']b]'c", "q'[····]'c", nil},
		{"Q-quotes with angle brackets", ora, "Q'<a>'x", "Q'<·>'x", nil},
		{"q-quotes with another character", ora, "q'!a!'x", "q'!·!'x", nil},
		{"q-quotes outside Oracle", pg, "q'[a']b", "q'··']b", nil},
		{"dollar quotes", pg, "$$a'b$$c", "$$···$$c", nil},
		{"a dollar tag", pg, "$A_b$x$A_b$c", "$A_b$·$A_b$c", nil},
		{"a dollar tag with a digit", pg, "$a1$x$a1$", "$a1$·$a1$", nil},
		{"a parameter is not a tag", pg, "$1'a'", "$1'·'", nil},
		{"a tag not closed is not a tag", pg, "$ab'x'", "$ab'·'", nil},
		{"dollar quotes not closed", pg, "$$abc", "$$···", nil},
		{"a tag cut by the end", pg, "SELECT $ab", "SELECT $ab", nil},
		{"a quote inside a line comment", pg, "-- it's\nx'a'", "·······\nx'·'", []string{"1: it's"}},
		{"a quote inside a hash comment", my, "# it's\nx'a'", "······\nx'·'", []string{"1: it's"}},
		{"a backslash in a quoted name", pg, `"x\"'y'`, `"x\"'·'`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := scan(tc.src, tc.lx)
			if want := strings.ReplaceAll(tc.masked, "·", " "); got.Masked != want {
				t.Errorf("masked %q, want %q", got.Masked, want)
			}
			var comments []string
			for _, c := range got.Comments {
				comments = append(comments, fmt.Sprintf("%d:%s", c.Line, c.Text))
			}
			if !slices.Equal(comments, tc.comments) {
				t.Errorf("comments %q, want %q", comments, tc.comments)
			}
		})
	}
}
