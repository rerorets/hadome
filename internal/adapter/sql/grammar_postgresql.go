package sql

import (
	"github.com/antlr4-go/antlr/v4"

	"github.com/rerorets/hadome/internal/adapter/sql/grammar/postgresql"
)

// The grammar of postgresql is linked into every build: the dialect is enabled.

func init() {
	grammars["postgresql"] = antlrGrammar{
		lexer:  func(s antlr.CharStream) antlr.Lexer { return postgresql.NewPostgreSQLLexer(s) },
		parser: func(s antlr.TokenStream) antlr.Parser { return postgresql.NewPostgreSQLParser(s) },
		entry:  func(p antlr.Parser) antlr.ParseTree { return p.(*postgresql.PostgreSQLParser).Root() },
	}
}
