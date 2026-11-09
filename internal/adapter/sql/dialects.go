package sql

import (
	"regexp"
	"strings"
)

// The dialects. Each is its grammar with the tables that say where its statements, names, calls and
// relations are, its markers, its client forms and its rules for case.

func lower(id Ident) string { return strings.ToLower(id.Text) }

// foldUnquoted folds an unquoted name with f and keeps a quoted one as written.
func foldUnquoted(f func(string) string) func(Ident) string {
	return func(id Ident) string {
		if id.Quoted {
			return id.Text
		}
		return f(id.Text)
	}
}

func asWritten(id Ident) string { return id.Text }

func re(expr string) *regexp.Regexp { return regexp.MustCompile(expr) }

func set(names ...string) map[string]bool {
	out := make(map[string]bool, len(names))
	for _, n := range names {
		out[n] = true
	}
	return out
}

// pgCast is a :: cast to one of PostgreSQL's types. SQL Server and Snowflake write :: too, for
// OBJECT::name, Type::Method() and their own casts, so only PostgreSQL's type names count.
var pgCast = re(`(?i)::\s*(text|int|int2|int4|int8|integer|bigint|smallint|numeric|decimal|real|float4|float8|` +
	`double\s+precision|boolean|bool|date|time|timestamp|timestamptz|interval|uuid|json|jsonb|bytea|varchar|` +
	`character\s+varying|regclass|regproc|regprocedure|regtype|regnamespace|name|oid|inet|cidr|money|tsvector|` +
	`tsquery|xml|citext|ltree|hstore)\b`)

var postgresqlDialect = tableDialect{
	name: "postgresql",
	lex:  lexical{nestedBlocks: true, dollarQuotes: true},
	splitter: splitter{
		commands: re(`^\s*\\`),
		copyData: true,
	},
	markers: []*regexp.Regexp{
		re(`\$[A-Za-z_]*\$`), re(`(?i)\bLANGUAGE\s+'?plpgsql\b`), re(`(?i)\bCREATE\s+EXTENSION\b`), pgCast,
	},
	fold: foldUnquoted(strings.ToLower),
	builtins: Builtins{Functions: postgresqlFunctions, Schemas: set("pg_catalog", "information_schema"),
		Prefixes: []string{"pg_"}},
	hazards: []HazardEntry{
		{Category: "clock", Call: "now"}, {Category: "clock", Call: "clock_timestamp"},
		{Category: "clock", Call: "statement_timestamp"}, {Category: "clock", Call: "transaction_timestamp"},
		{Category: "clock", Call: "timeofday"},
		{Category: "clock", Keywords: []string{"CURRENT_TIMESTAMP"}}, {Category: "clock", Keywords: []string{"CURRENT_DATE"}},
		{Category: "clock", Keywords: []string{"CURRENT_TIME"}}, {Category: "clock", Keywords: []string{"LOCALTIMESTAMP"}},
		{Category: "clock", Keywords: []string{"LOCALTIME"}},
		{Category: "sleep", Call: "pg_sleep"}, {Category: "sleep", Call: "pg_sleep_for"}, {Category: "sleep", Call: "pg_sleep_until"},
	},
	rules: rules{
		statements: []string{"stmt"},
		names:      []string{"func_name", "qualified_name", "any_name", "name"},
		params:     []string{"param_name"},
		calls:      []callRule{{"func_application", []string{"func_name"}}},
		relations: []nameRule{{"relation_expr", []string{"qualified_name"}}, {"insert_target", []string{"qualified_name"}},
			{"refreshmatviewstmt", []string{"qualified_name"}}},
		ctes: []nameRule{{"common_table_expr", []string{"name"}}},
	},
}

var mysqlDialect = tableDialect{
	name:            "mysql",
	renameWithoutTo: true,
	lex:             lexical{hashComments: true, doubleStrings: true, backslash: true},
	splitter: splitter{
		commands:  re(`(?i)^\s*(source\s+\S+|\\\.\s+\S+|\\[a-z]\b.*)\s*;?\s*$`),
		delimiter: true,
	},
	markers: []*regexp.Regexp{
		re(`(?im)^\s*DELIMITER\s+\S+\s*$`), re(`(?i)\bENGINE\s*=`), re("`[^`.\n]+`"), re(`(?i)\bAUTO_INCREMENT\b`),
	},
	fold: lower,
	builtins: Builtins{Functions: mysqlFunctions,
		Schemas: set("information_schema", "mysql", "performance_schema", "sys")},
	hazards: []HazardEntry{
		{Category: "clock", Call: "now"}, {Category: "clock", Call: "sysdate"}, {Category: "clock", Call: "curdate"},
		{Category: "clock", Call: "curtime"}, {Category: "clock", Call: "utc_timestamp"}, {Category: "clock", Call: "unix_timestamp"},
		{Category: "clock", Keywords: []string{"CURRENT_TIMESTAMP"}}, {Category: "clock", Keywords: []string{"CURRENT_DATE"}},
		{Category: "clock", Keywords: []string{"CURRENT_TIME"}}, {Category: "clock", Keywords: []string{"UTC_TIMESTAMP"}},
		{Category: "sleep", Call: "sleep"},
	},
	rules: rules{
		statements: []string{"sqlStatement"},
		names:      []string{"fullId", "tableName"},
		params:     []string{"functionParameter", "procedureParameter"},
		calls: []callRule{{"functionCall", []string{"fullId", "scalarFunctionName"}}, {"callStatement", []string{"fullId"}},
			{"specificFunctionCall", nil}},
		relations: []nameRule{{"tableSourceItem", []string{"tableName"}}, {"insertStatement", []string{"tableName"}},
			{"replaceStatement", []string{"tableName"}}, {"singleDeleteStatement", []string{"tableName"}},
			{"truncateTable", []string{"tableName"}}},
		ctes: []nameRule{{"commonTableExpressions", []string{"cteName"}}},
	},
}

var sqliteDialect = tableDialect{
	name: "sqlite",
	lex:  lexical{},
	splitter: splitter{
		commands: re(`^\s*\.[a-z]`),
	},
	markers: []*regexp.Regexp{
		re(`(?i)\bAUTOINCREMENT\b`), re(`(?i)\bWITHOUT\s+ROWID\b`), re(`(?im)^\s*PRAGMA\b`),
	},
	fold: lower,
	builtins: Builtins{Functions: sqliteFunctions, Schemas: set("main", "temp"),
		Prefixes: []string{"sqlite_"}},
	hazards: []HazardEntry{
		{Category: "clock", Keywords: []string{"CURRENT_TIMESTAMP"}}, {Category: "clock", Keywords: []string{"CURRENT_DATE"}},
		{Category: "clock", Keywords: []string{"CURRENT_TIME"}}, {Category: "clock", Keywords: []string{"'NOW'"}},
	},
	rules: rules{
		statements: []string{"sql_stmt"},
		names:      []string{"schema_name", "table_name", "view_name", "trigger_name", "index_name", "new_table_name"},
		calls: []callRule{{"expr_base", []string{"function_name"}}, {"expr", []string{"function_name"}},
			{"expr_recursive", []string{"function_name"}}},
		relations: []nameRule{{"table_or_subquery", []string{"schema_name", "table_name"}},
			{"qualified_table_name", []string{"schema_name", "table_name"}},
			{"insert_stmt", []string{"schema_name", "table_name"}}},
		ctes: []nameRule{{"common_table_expression", []string{"cte_table_name"}}},
	},
}

var tsqlDialect = tableDialect{
	name:           "tsql",
	alterRedefines: true,
	lex:            lexical{},
	splitter: splitter{
		commands:  re(`^\s*:[A-Za-z]`),
		separator: re(`(?i)^\s*GO(\s+\d+)?\s*;?\s*$`),
	},
	markers: []*regexp.Regexp{
		re(`(?im)^\s*GO(\s+\d+)?\s*;?\s*$`),
		re(`\[[A-Za-z_#@][\w ]*\]\s*\.|\.\s*\[[A-Za-z_][\w ]*\]`),
		re(`(?i)\b(TABLE|VIEW|PROCEDURE|PROC|FUNCTION|INTO|FROM|JOIN|UPDATE|EXEC|EXECUTE)\s+\[[A-Za-z_]`),
		re(`(?i)\bDECLARE\s+@\w+`),
		re(`(?i)\bNVARCHAR\s*\(\s*MAX\s*\)`),
	},
	fold: lower,
	builtins: Builtins{Functions: tsqlFunctions, Schemas: set("sys", "information_schema"),
		Prefixes: []string{"sp_", "xp_", "fn_"}},
	hazards: []HazardEntry{
		{Category: "clock", Call: "getdate"}, {Category: "clock", Call: "getutcdate"}, {Category: "clock", Call: "sysdatetime"},
		{Category: "clock", Call: "sysutcdatetime"}, {Category: "clock", Call: "sysdatetimeoffset"},
		{Category: "clock", Keywords: []string{"CURRENT_TIMESTAMP"}},
		{Category: "sleep", Keywords: []string{"WAITFOR", "DELAY"}}, {Category: "sleep", Keywords: []string{"WAITFOR", "TIME"}},
	},
	rules: rules{
		statements: []string{"batch_level_statement", "sql_clauses"},
		names: []string{"func_proc_name_schema", "func_proc_name_server_database_schema", "full_table_name", "simple_name",
			"table_name", "entity_name", "full_object_name"},
		params: []string{"procedure_param"},
		calls: []callRule{{"function_call", []string{"scalar_function_name"}},
			{"execute_body", []string{"func_proc_name_server_database_schema"}}},
		relations: []nameRule{{"table_source_item", []string{"full_table_name"}}, {"ddl_object", []string{"full_table_name"}}},
		ctes:      []nameRule{{"common_table_expression", []string{"id_"}}},
	},
}

var oracleDialect = tableDialect{
	name: "oracle",
	lex:  lexical{qQuotes: true},
	splitter: splitter{
		commands: re(`(?i)^\s*(@@?\S+|(SET|PROMPT|REM|REMARK|SPOOL|WHENEVER|EXIT|QUIT|DEFINE|UNDEFINE|COLUMN|TTITLE|BTITLE|` +
			`BREAK|COMPUTE|CONNECT|CONN|DISCONNECT|SHOW|PAUSE|ACCEPT|HOST|VARIABLE|PRINT|CLEAR|EXEC|EXECUTE|SQLBLANKLINES)\b.*)$`),
		keep:            re(`(?i)^\s*(SET\s+(TRANSACTION|ROLE|CONSTRAINTS?)\b|EXEC(UTE)?\s+IMMEDIATE\b)`),
		commandsAtStart: true,
		separator:       re(`^\s*/\s*$`),
	},
	markers: []*regexp.Regexp{
		re(`(?i)\bCREATE\s+(OR\s+REPLACE\s+)?((NON)?EDITIONABLE\s+)?PACKAGE\b`), re(`(?m)^\s*/\s*$`),
		re(`(?i)\bVARCHAR2\b`), re(`(?i)\bIS\s+BEGIN\b`),
	},
	fold: foldUnquoted(strings.ToUpper),
	builtins: Builtins{Functions: oracleFunctions, Schemas: set("SYS", "SYSTEM", "PUBLIC"),
		Prefixes: []string{"DBMS_", "UTL_", "APEX_", "OWA_", "HTP", "SYS_", "V$", "ALL_", "USER_", "DBA_"}},
	hazards: []HazardEntry{
		{Category: "clock", Keywords: []string{"SYSDATE"}}, {Category: "clock", Keywords: []string{"SYSTIMESTAMP"}},
		{Category: "clock", Keywords: []string{"CURRENT_TIMESTAMP"}}, {Category: "clock", Keywords: []string{"CURRENT_DATE"}},
		{Category: "clock", Keywords: []string{"LOCALTIMESTAMP"}},
		{Category: "sleep", Call: "DBMS_LOCK.SLEEP"}, {Category: "sleep", Call: "DBMS_SESSION.SLEEP"},
	},
	rules: rules{
		statements: []string{"sql_unit", "plsql_unit"},
		names: []string{"function_name", "procedure_name", "package_name", "tableview_name", "trigger_name", "sequence_name",
			"schema_object_name", "materialized_view_name", "schema_name", "table_name", "id_expression"},
		params:          []string{"parameter"},
		packageRoutines: []nameRule{{"function_spec", []string{"identifier"}}, {"procedure_spec", []string{"identifier"}}},
		calls:           []callRule{{"call_statement", []string{"routine_name"}}},
		relations:       []nameRule{{"dml_table_expression_clause", []string{"tableview_name"}}},
		ctes:            []nameRule{{"subquery_factoring_clause", []string{"query_name"}}},
		callName:        oracleCall,
	},
}

// oracleCall reads a call written as an expression: pkg.f(1) is a general_element whose last part
// has arguments. A name without arguments is a column, or a call that cannot be told from one.
func oracleCall(n *Node) (*Node, []string, bool) {
	if n.Rule != "general_element" || n.Parent != nil && n.Parent.Rule == "general_element" {
		return nil, nil, false
	}
	parts := n.All(func(m *Node) bool { return m.Rule == "general_element_part" })
	if len(parts) == 0 {
		return nil, nil, false
	}
	last := parts[len(parts)-1]
	argsNode := last.Child("function_argument")
	if argsNode == nil {
		return nil, nil, false
	}
	name := &Node{Rule: "name", Start: n.Start, Line: n.Line}
	for _, p := range parts {
		for _, c := range p.Children {
			if c.Rule == "function_argument" {
				continue
			}
			if len(name.Children) > 0 {
				name.Children = append(name.Children, &Node{Text: ".", Token: "PERIOD"})
			}
			name.Children = append(name.Children, c)
		}
	}
	return name, arguments(argsNode, &Node{End: argsNode.Start}), true
}

var bigqueryDialect = tableDialect{
	name: "bigquery",
	lex:  lexical{hashComments: true, doubleStrings: true, backslash: true, tripleQuotes: true},
	markers: []*regexp.Regexp{
		re("`[^`.\n]+\\.[^`\n]+`"), re(`(?i)\bCREATE\s+(OR\s+REPLACE\s+)?(TEMP(ORARY)?\s+)?TABLE\s+FUNCTION\b`),
		re(`(?i)\bLANGUAGE\s+js\b`), re(`(?i)\bOPTIONS\s*\(`),
	},
	fold:     asWritten,
	builtins: Builtins{Functions: bigqueryFunctions, Schemas: set("INFORMATION_SCHEMA")},
	hazards: []HazardEntry{
		{Category: "clock", Call: "CURRENT_TIMESTAMP"}, {Category: "clock", Call: "CURRENT_DATE"},
		{Category: "clock", Call: "CURRENT_DATETIME"}, {Category: "clock", Call: "CURRENT_TIME"},
		{Category: "clock", Keywords: []string{"CURRENT_TIMESTAMP"}}, {Category: "clock", Keywords: []string{"CURRENT_DATE"}},
	},
	rules: rules{
		statements: []string{"unterminated_sql_statement", "unterminated_script_statement"},
		blocks:     []string{"begin_end_block", "statement_list"},
		names:      []string{"path_expression", "maybe_dashed_path_expression"},
		params:     []string{"function_parameter", "procedure_parameter"},
		calls: []callRule{{"function_call_expression_with_clauses", []string{"function_name_from_keyword", "path_expression"}},
			{"tvf_prefix_no_args", []string{"path_expression"}}},
		relations: []nameRule{{"table_path_expression", []string{"table_path_expression_base", "maybe_dashed_path_expression"}}},
		ctes:      []nameRule{{"with_clause_entry", []string{"identifier"}}},
	},
}

var snowflakeDialect = tableDialect{
	name: "snowflake",
	lex:  lexical{dollarQuotes: true},
	splitter: splitter{
		commands: re(`^\s*!\w`),
	},
	markers: []*regexp.Regexp{
		re(`(?i)\bCREATE\s+(OR\s+REPLACE\s+)?TASK\b`), re(`(?i)\bCREATE\s+(OR\s+REPLACE\s+)?STREAM\b`),
		re(`(?i)\bCREATE\s+(OR\s+REPLACE\s+)?((TEMP|TEMPORARY)\s+)?STAGE\b`), re(`(?i)\bLANGUAGE\s+JAVASCRIPT\b`),
		re(`(?i)\bRETURNS\s+VARIANT\b`),
	},
	fold: foldUnquoted(strings.ToUpper),
	builtins: Builtins{Functions: snowflakeFunctions, Schemas: set("INFORMATION_SCHEMA", "SNOWFLAKE", "ACCOUNT_USAGE"),
		Prefixes: []string{"SYSTEM$"}},
	hazards: []HazardEntry{
		{Category: "clock", Call: "CURRENT_TIMESTAMP"}, {Category: "clock", Call: "SYSDATE"}, {Category: "clock", Call: "GETDATE"},
		{Category: "clock", Keywords: []string{"CURRENT_TIMESTAMP"}}, {Category: "clock", Keywords: []string{"CURRENT_DATE"}},
		{Category: "sleep", Call: "SYSTEM$WAIT"},
	},
	rules: rules{
		statements: []string{"sql_command"},
		names:      []string{"object_name"},
		params:     []string{"arg_decl"},
		calls: []callRule{{"function_call", []string{"object_name"}}, {"call", []string{"object_name"}},
			{"execute_task", []string{"object_name"}}},
		relations: []nameRule{{"object_ref", []string{"object_name"}}, {"insert_statement", []string{"object_name"}},
			{"update_statement", []string{"object_name"}}, {"delete_statement", []string{"object_name"}},
			{"merge_statement", []string{"object_name"}}, {"truncate_table", []string{"object_name"}}},
		ctes: []nameRule{{"common_table_expression", []string{"id_"}}},
	},
}
