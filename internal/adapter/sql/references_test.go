package sql

import (
	"slices"
	"strings"
	"testing"
)

// tokenNode is a node holding one token per word of text, the last one EOF when text ends in <EOF>.
func tokenNode(text string) *Node {
	n := &Node{Rule: "call"}
	at := 0
	for _, w := range strings.Fields(text) {
		tok := &Node{Text: w, Token: "WORD", Start: at, End: at + len(w), Parent: n}
		if w == "<EOF>" {
			tok.Text, tok.Token, tok.End = "", "EOF", at
		}
		n.Children = append(n.Children, tok)
		at += len(w) + 1
	}
	return n
}

// viewpoint: behaviour
// target: sql.arguments
// A call's arguments are the tokens after its name, inside its parentheses or, without them, up to
// the end of the statement, split at the commas outside parentheses and brackets.
func TestArgumentsOfACall(t *testing.T) {
	cases := []struct {
		text string
		want []string
	}{
		{"f ( a , g ( b , c ) , [ d , e ] ) x", []string{"a", "g ( b , c )", "[ d , e ]"}},
		{"p a , b ; c", []string{"a", "b"}},
		{"p a , b <EOF>", []string{"a", "b"}},
		{"p a , b", []string{"a", "b"}},
		{"f ( )", nil},
		{"f", nil},
	}
	for _, tc := range cases {
		call := tokenNode(tc.text)
		if got := arguments(call, call.Children[0]); !slices.Equal(got, tc.want) {
			t.Errorf("arguments of %q = %q, want %q", tc.text, got, tc.want)
		}
	}
}
