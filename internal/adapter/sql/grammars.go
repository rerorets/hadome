package sql

import (
	"github.com/antlr4-go/antlr/v4"

	"github.com/rerorets/hadome/internal/adapter/sql/grammar/googlesql"
	"github.com/rerorets/hadome/internal/adapter/sql/grammar/mysql"
	"github.com/rerorets/hadome/internal/adapter/sql/grammar/plsql"
	"github.com/rerorets/hadome/internal/adapter/sql/grammar/postgresql"
	"github.com/rerorets/hadome/internal/adapter/sql/grammar/snowflake"
	"github.com/rerorets/hadome/internal/adapter/sql/grammar/sqlite"
	"github.com/rerorets/hadome/internal/adapter/sql/grammar/tsql"
)

// The grammars of the dialects, each generated from a public ANTLR grammar (grammar/generate.sh).

var postgresqlGrammar = antlrGrammar{
	lexer:  func(s antlr.CharStream) antlr.Lexer { return postgresql.NewPostgreSQLLexer(s) },
	parser: func(s antlr.TokenStream) antlr.Parser { return postgresql.NewPostgreSQLParser(s) },
	entry:  func(p antlr.Parser) antlr.ParseTree { return p.(*postgresql.PostgreSQLParser).Root() },
}

// mysqlGrammar reads MySQL and MariaDB.
var mysqlGrammar = antlrGrammar{
	lexer:  func(s antlr.CharStream) antlr.Lexer { return mysql.NewMySqlLexer(s) },
	parser: func(s antlr.TokenStream) antlr.Parser { return mysql.NewMySqlParser(s) },
	entry:  func(p antlr.Parser) antlr.ParseTree { return p.(*mysql.MySqlParser).Root() },
}

var sqliteGrammar = antlrGrammar{
	lexer:  func(s antlr.CharStream) antlr.Lexer { return sqlite.NewSQLiteLexer(s) },
	parser: func(s antlr.TokenStream) antlr.Parser { return sqlite.NewSQLiteParser(s) },
	entry:  func(p antlr.Parser) antlr.ParseTree { return p.(*sqlite.SQLiteParser).Parse() },
}

var tsqlGrammar = antlrGrammar{
	lexer:  func(s antlr.CharStream) antlr.Lexer { return tsql.NewTSqlLexer(s) },
	parser: func(s antlr.TokenStream) antlr.Parser { return tsql.NewTSqlParser(s) },
	entry:  func(p antlr.Parser) antlr.ParseTree { return p.(*tsql.TSqlParser).Tsql_file() },
}

var oracleGrammar = antlrGrammar{
	lexer:  func(s antlr.CharStream) antlr.Lexer { return plsql.NewPlSqlLexer(s) },
	parser: func(s antlr.TokenStream) antlr.Parser { return plsql.NewPlSqlParser(s) },
	entry:  func(p antlr.Parser) antlr.ParseTree { return p.(*plsql.PlSqlParser).Sql_script() },
}

var bigqueryGrammar = antlrGrammar{
	lexer:  func(s antlr.CharStream) antlr.Lexer { return googlesql.NewGoogleSQLLexer(s) },
	parser: func(s antlr.TokenStream) antlr.Parser { return googlesql.NewGoogleSQLParser(s) },
	entry:  func(p antlr.Parser) antlr.ParseTree { return p.(*googlesql.GoogleSQLParser).Root() },
}

var snowflakeGrammar = antlrGrammar{
	lexer:  func(s antlr.CharStream) antlr.Lexer { return snowflake.NewSnowflakeLexer(s) },
	parser: func(s antlr.TokenStream) antlr.Parser { return snowflake.NewSnowflakeParser(s) },
	entry:  func(p antlr.Parser) antlr.ParseTree { return p.(*snowflake.SnowflakeParser).Snowflake_file() },
}
