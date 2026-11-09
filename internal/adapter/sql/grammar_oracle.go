//go:build sqlgrammars

package sql

import (
	"github.com/antlr4-go/antlr/v4"

	"github.com/rerorets/hadome/internal/adapter/sql/grammar/plsql"
)

// The grammar of oracle is linked only with -tags sqlgrammars: the dialect is not enabled. Enabling it
// removes the build constraint above and adds the dialect to config.SQLDialects.

func init() {
	grammars["oracle"] = antlrGrammar{
		lexer:  func(s antlr.CharStream) antlr.Lexer { return plsql.NewPlSqlLexer(s) },
		parser: func(s antlr.TokenStream) antlr.Parser { return plsql.NewPlSqlParser(s) },
		entry:  func(p antlr.Parser) antlr.ParseTree { return p.(*plsql.PlSqlParser).Sql_script() },
	}
}
