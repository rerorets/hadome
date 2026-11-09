# SQL grammars

The SQL adapter reads SQL with Go parsers made from each dialect's public ANTLR grammar. This document says where the grammars and the parsers are, and how to regenerate them. The design is in [adapters-sql.md](../design/adapters-sql.md).

## Where they are

Under `internal/adapter/sql/grammar/` there is one directory per dialect.

| Directory | Dialect | Grammar it comes from | License |
|---|---|---|---|
| `postgresql` | PostgreSQL | `sql/postgresql` of grammars-v4 | MIT (with parts under the PostgreSQL license) |
| `mysql` | MySQL, MariaDB | `sql/mysql/Positive-Technologies` of grammars-v4 | MIT |
| `sqlite` | SQLite | `sql/sqlite` of grammars-v4 | MIT |
| `tsql` | SQL Server | `sql/tsql` of grammars-v4 | MIT |
| `plsql` | Oracle | `sql/plsql` of grammars-v4 | Apache-2.0 |
| `googlesql` | BigQuery | `googlesql` of bytebase/parser | BSD-3-Clause |
| `snowflake` | Snowflake | `sql/snowflake` of grammars-v4 | MIT |

Each directory holds the following.

- **`src/*.g4`**: the ANTLR grammars. They are the original grammars with the rewrite for Go applied (the original repository's `transformGrammar.py`)
- **`src/*.go.in`**: support code the grammar's actions call. It comes from the original grammar's Go directory
- **`LICENSE`**: the grammar's license. It goes into `THIRD_PARTY_NOTICES`
- **`*.go`**: the code `generate.sh` writes. Every file starts with `Code generated … DO NOT EDIT.`, so hadome treats it as generated code

Every change to the original grammar has a comment starting with `hadome:` in the `.g4`. The comment says why.

Example: the PostgreSQL grammar could not read the words it makes tokens for PL/pgSQL (`warning` and others) as SQL names. Those words are added to `unreserved_keyword`, with a `// hadome:` comment giving the reason.

## How to regenerate

After changing a `.g4` or a `.go.in`, regenerate the Go code.

```sh
ANTLR_JAR=/path/to/antlr-4.13.1-complete.jar internal/adapter/sql/grammar/generate.sh postgresql
```

- Without a dialect, every dialect is regenerated
- Java 11 or later is needed
- The ANTLR version is 4.13.1, the same as `github.com/antlr4-go/antlr/v4` in `go.mod`. The jar is at https://www.antlr.org/download/
- After regenerating, run `go test ./internal/adapter/sql/...` and each dialect's trial ("When a dialect is enabled" below)

## When a dialect is enabled

A dialect is not enabled until it passes a trial on real code ("When a dialect is enabled" in [adapters-sql.md](../design/adapters-sql.md)).

- `internal/adapter/sql/grammar_<dialect>.go` puts each dialect's grammar into the build. The file of a dialect that is not enabled has `//go:build sqlgrammars`, so the usual build leaves it out
- To try the reader of a dialect that is not enabled, and to run its unit tests (`dialects_test.go`), add the tag: `go test -tags sqlgrammars ./internal/adapter/sql/`
- CI runs the tests and vet with the tag in a job of its own, so the readers of dialects that are not enabled are checked on every pull request too
- Enabling a dialect changes two places: remove the `//go:build` line of `grammar_<dialect>.go`, and add the dialect to `SQLDialects` in `internal/config`. The dialects in `SQLDialects` are the enabled ones
- When the two disagree, `TestTheBuildLinksTheGrammarsOfEnabledDialects` fails
