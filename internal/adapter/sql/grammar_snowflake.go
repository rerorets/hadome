//go:build sqlgrammars

package sql

import (
	"github.com/antlr4-go/antlr/v4"

	"github.com/rerorets/hadome/internal/adapter/sql/grammar/snowflake"
)

// The grammar of snowflake is linked only with -tags sqlgrammars: the dialect is not enabled. Enabling it
// removes the build constraint above and adds the dialect to config.SQLDialects.

func init() {
	grammars["snowflake"] = antlrGrammar{
		lexer:  func(s antlr.CharStream) antlr.Lexer { return snowflake.NewSnowflakeLexer(s) },
		parser: func(s antlr.TokenStream) antlr.Parser { return snowflake.NewSnowflakeParser(s) },
		entry:  func(p antlr.Parser) antlr.ParseTree { return p.(*snowflake.SnowflakeParser).Snowflake_file() },
	}
}
