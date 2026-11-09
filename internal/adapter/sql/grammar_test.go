package sql

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/antlr4-go/antlr/v4"
)

// viewpoint: behaviour
// target: sql.antlrGrammar.Parse
// A parsed piece's tokens have their grammar's names, their byte offsets in the file (the piece's
// start added, a character of several bytes counted in bytes) and their lines in the file; EOF has
// no text and no width. An empty piece reads.
func TestParsedTokensArePlacedInTheFile(t *testing.T) {
	n, err := grammars["postgresql"].Parse("SELECT 'é' AS a;\nSELECT 2;", 100, 10)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tk := range n.Tokens() {
		got = append(got, fmt.Sprintf("%q %s %d-%d line %d", tk.Text, tk.Token, tk.Start, tk.End, tk.Line))
	}
	want := []string{
		`"SELECT" SELECT 100-106 line 10`, `"'é'" StringConstant 107-111 line 10`, `"AS" AS 112-114 line 10`,
		`"a" Identifier 115-116 line 10`, `";" SEMI 116-117 line 10`, `"SELECT" SELECT 118-124 line 11`,
		`"2" Integral 125-126 line 11`, `";" SEMI 126-127 line 11`, `"" EOF 127-127 line 11`,
	}
	if !slices.Equal(got, want) {
		t.Errorf("tokens\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if _, err := grammars["postgresql"].Parse("", 0, 1); err != nil {
		t.Errorf("an empty piece: %v", err)
	}
}

// viewpoint: failure
// target: sql.antlrGrammar.Parse
// A piece that does not read is an error that says where, from the piece's first line when that is
// not the first line of the file, with the parser's message or else the token it stopped at. A
// character the lexer has no token for is an error too, though the parser reads the rest. Nothing is
// printed: neither the lexer nor the parser writes to standard error or standard output.
func TestSyntaxErrorsSayWhere(t *testing.T) {
	cases := []struct {
		dialect, text string
		line          int
		want          string
	}{
		{"postgresql", ")", 1, "line 1:0: extraneous input ')' expecting <EOF>"},
		{"postgresql", "SELECT FROM (", 1, `line 1:12: unexpected "("`},
		{"postgresql", "SELECT FROM (", 5, `from line 5: line 1:12: unexpected "("`},
		{"postgresql", "SELECT 1 ` ", 1, `line 1:9: unexpected "` + "`" + `"`},
		{"bigquery", "SELECT 1 ¤", 1, "line 1:9: token recognition error at: '¤'"},
	}
	for _, tc := range cases {
		var err error
		printed := captureOutput(t, func() { _, err = grammars[tc.dialect].Parse(tc.text, 0, tc.line) })
		if err == nil || err.Error() != tc.want {
			t.Errorf("Parse(%q) error %v, want %s", tc.text, err, tc.want)
		}
		if printed != "" {
			t.Errorf("Parse(%q) printed %q", tc.text, printed)
		}
	}
}

// viewpoint: failure
// target: sql.runEntry
// A generated parser that panics is a syntax error at the token it stopped at, not a crash.
func TestAParserThatPanicsIsASyntaxError(t *testing.T) {
	g := grammars["postgresql"].(antlrGrammar)
	p := g.parser(antlr.NewCommonTokenStream(g.lexer(antlr.NewInputStream("SELECT 1")), antlr.TokenDefaultChannel))
	tree, err := runEntry(func(antlr.Parser) antlr.ParseTree { panic("an action failed") }, p)
	if tree != nil || err == nil || err.Error() != `line 1:0: syntax error at "SELECT"` {
		t.Errorf("tree %v, error %v", tree, err)
	}
}

// captureOutput returns what f writes to standard output and standard error.
func captureOutput(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, stderr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = w, w
	defer func() { os.Stdout, os.Stderr = stdout, stderr }()
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	f()
	os.Stdout, os.Stderr = stdout, stderr
	w.Close()
	return <-done
}
