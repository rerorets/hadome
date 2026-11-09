//go:build sqlgrammars

package sql

import (
	"github.com/antlr4-go/antlr/v4"

	"github.com/rerorets/hadome/internal/adapter/sql/grammar/tsql"
)

// The grammar of tsql is linked only with -tags sqlgrammars: the dialect is not enabled. Enabling it
// removes the build constraint above and adds the dialect to config.SQLDialects.

func init() {
	grammars["tsql"] = antlrGrammar{
		lexer:  func(s antlr.CharStream) antlr.Lexer { return tsql.NewTSqlLexer(s) },
		parser: func(s antlr.TokenStream) antlr.Parser { return tsql.NewTSqlParser(s) },
		entry:  func(p antlr.Parser) antlr.ParseTree { return p.(*tsql.TSqlParser).Tsql_file() },
	}
}
