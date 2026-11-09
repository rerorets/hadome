//go:build sqlgrammars

package sql

import (
	"github.com/antlr4-go/antlr/v4"

	"github.com/rerorets/hadome/internal/adapter/sql/grammar/sqlite"
)

// The grammar of sqlite is linked only with -tags sqlgrammars: the dialect is not enabled. Enabling it
// removes the build constraint above and adds the dialect to config.SQLDialects.

func init() {
	grammars["sqlite"] = antlrGrammar{
		lexer:  func(s antlr.CharStream) antlr.Lexer { return sqlite.NewSQLiteLexer(s) },
		parser: func(s antlr.TokenStream) antlr.Parser { return sqlite.NewSQLiteParser(s) },
		entry:  func(p antlr.Parser) antlr.ParseTree { return p.(*sqlite.SQLiteParser).Parse() },
	}
}
