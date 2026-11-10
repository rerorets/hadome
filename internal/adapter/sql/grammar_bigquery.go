package sql

import (
	"github.com/antlr4-go/antlr/v4"

	"github.com/rerorets/hadome/internal/adapter/sql/grammar/googlesql"
)

// The grammar of bigquery is linked into every build: the dialect is enabled.

func init() {
	grammars["bigquery"] = antlrGrammar{
		lexer:  func(s antlr.CharStream) antlr.Lexer { return googlesql.NewGoogleSQLLexer(s) },
		parser: func(s antlr.TokenStream) antlr.Parser { return googlesql.NewGoogleSQLParser(s) },
		entry:  func(p antlr.Parser) antlr.ParseTree { return p.(*googlesql.GoogleSQLParser).Root() },
		// An OR inside parentheses is read as one expression (GoogleSQLParser.g4).
		inlined: map[string]string{"expression_maybe_parenthesized_not_a_query": "expression"},
	}
}
