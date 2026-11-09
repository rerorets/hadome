package sql

import (
	"github.com/antlr4-go/antlr/v4"

	"github.com/rerorets/hadome/internal/adapter/sql/grammar/mysql"
)

// The grammar of mysql is linked into every build: the dialect is enabled.

func init() {
	grammars["mysql"] = antlrGrammar{
		lexer:  func(s antlr.CharStream) antlr.Lexer { return mysql.NewMySqlLexer(s) },
		parser: func(s antlr.TokenStream) antlr.Parser { return mysql.NewMySqlParser(s) },
		entry:  func(p antlr.Parser) antlr.ParseTree { return p.(*mysql.MySqlParser).Root() },
	}
}
