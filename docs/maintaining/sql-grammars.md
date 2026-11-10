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

## Changes so that SLL reads

The SQL adapter reads with SLL first, and reads again with LL only the statements SLL could not read (see "How SQL is read" in [adapters-sql.md](../design/adapters-sql.md)). When SLL cannot tell alternatives apart, it takes the first. So a correct statement may not read with SLL. When a common form does that, the grammar is changed so that it reads with SLL.

| Dialect | Form | Change |
|---|---|---|
| MySQL | A function call (`NOW(6)`, `s.f(1)`). Statements need no `;` between them, so SLL takes it for a column name followed by the `(` of the next statement | The column alternative of `selectElement` gets the predicate `calledName`, and a name right before `(` is not read as a column |
| BigQuery | An OR inside parentheses (`a AND (b IS NULL OR c = 1)`). SLL lets the left side of the OR take every OR, and fails | `expression_maybe_parenthesized_not_a_query` gets a first alternative, `expression` with the predicate `orInParentheses` |

- **The tree does not change.** The tree hadome builds from a statement that reads (`Node`) stays what it was before the change. When an added alternative makes the tree one level deeper, `inlined` of `antlrGrammar` says so, and the conversion removes that level. The BigQuery change above does this
- **Input whose tree changes.** The MySQL change alters LL's result only for two statements without `;` between them, joined as `SELECT f ((SELECT 1))`. Before the change it read as the column `f` and the next statement in parentheses; after it, as a call of `f`. A name without parentheses (`SCHEMA`, `CURRENT_USER`) reads as a column, as before
- **Predicates.** The functions of predicates are in `@parser::members` of the `.g4`. A predicate looks only at the tokens ahead, never at the state of the grammar
- **Trial.** After a change, check these three things
  - `TestStatementsWithParenthesesReadInSLL` reads the changed forms with SLL
  - The trees built from hadome's tests and from the `.sql` of real repositories at hand are the same before and after
  - On the same repositories, the output of `hadome check` is the same

**Forms SLL does not read (not changed)**

| Dialect | Form | Why it is not changed |
|---|---|---|
| MySQL | Queries joined with `UNION` (`SELECT 1 UNION SELECT 2`) | In the grammar, `querySpecificationNointo` may hold a `UNION` too, so deciding where to split needs LL. Changing it to read with SLL changes the shape of the tree of a query joining three or more |

## When a dialect is enabled

A dialect is not enabled until it passes a trial on real code ("When a dialect is enabled" in [adapters-sql.md](../design/adapters-sql.md)).

- `internal/adapter/sql/grammar_<dialect>.go` puts each dialect's grammar into the build. The file of a dialect that is not enabled has `//go:build sqlgrammars`, so the usual build leaves it out
- To try the reader of a dialect that is not enabled, and to run its unit tests (`dialects_test.go`), add the tag: `go test -tags sqlgrammars ./internal/adapter/sql/`
- CI runs the tests and vet with the tag in a job of its own, so the readers of dialects that are not enabled are checked on every pull request too
- Enabling a dialect changes two places: remove the `//go:build` line of `grammar_<dialect>.go`, and add the dialect to `SQLDialects` in `internal/config`. The dialects in `SQLDialects` are the enabled ones
- When the two disagree, `TestTheBuildLinksTheGrammarsOfEnabledDialects` fails
