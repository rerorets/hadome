# SQL の文法

SQL のアダプタは、方言ごとの公開された ANTLR の文法から作った Go のパーサで SQL を読みます。この文書は、その文法とパーサの置き場所と、作り直し方を書きます。設計は [adapters-sql.ja.md](../design/adapters-sql.ja.md) にあります。

## 置き場所

`internal/adapter/sql/grammar/` の下に、方言ごとに1つのディレクトリがあります。

| ディレクトリ | 方言 | 元の文法 | ライセンス |
|---|---|---|---|
| `postgresql` | PostgreSQL | grammars-v4 の `sql/postgresql` | MIT（PostgreSQL のライセンスの部分を含む） |
| `mysql` | MySQL、MariaDB | grammars-v4 の `sql/mysql/Positive-Technologies` | MIT |
| `sqlite` | SQLite | grammars-v4 の `sql/sqlite` | MIT |
| `tsql` | SQL Server | grammars-v4 の `sql/tsql` | MIT |
| `plsql` | Oracle | grammars-v4 の `sql/plsql` | Apache-2.0 |
| `googlesql` | BigQuery | bytebase/parser の `googlesql` | BSD-3-Clause |
| `snowflake` | Snowflake | grammars-v4 の `sql/snowflake` | MIT |

各ディレクトリの中身は次のとおりです。

- **`src/*.g4`**: ANTLR の文法です。元の文法に、Go 用の書き換え（元のリポジトリの `transformGrammar.py`）を当てたものです
- **`src/*.go.in`**: 文法の動作が呼ぶ補助のコードです。元の文法の Go 用のディレクトリにあったものです
- **`LICENSE`**: 文法のライセンスです。`THIRD_PARTY_NOTICES` に載ります
- **`*.go`**: `generate.sh` が作るコードです。どのファイルも先頭に `Code generated … DO NOT EDIT.` があり、hadome は生成されたコードとして扱います

元の文法から直したところには、`.g4` の中に `hadome:` で始まるコメントを付けます。直す理由も、そのコメントに書きます。

例: PostgreSQL の文法は、PL/pgSQL のためにトークンにした単語（`warning` など）を、SQL の名前として読めませんでした。`unreserved_keyword` にその単語を足し、`// hadome:` のコメントで理由を書いています。

## 作り直し方

`.g4` か `.go.in` を直したら、Go のコードを作り直します。

```sh
ANTLR_JAR=/path/to/antlr-4.13.1-complete.jar internal/adapter/sql/grammar/generate.sh postgresql
```

- 方言を書かないと、すべての方言を作り直します
- Java 11 以上が要ります
- ANTLR の版は、`go.mod` の `github.com/antlr4-go/antlr/v4` と同じ 4.13.1 にします。jar は https://www.antlr.org/download/ から取ります
- 作り直したら、`go test ./internal/adapter/sql/...` と、各方言の試し（後述の「方言を有効にする条件」）を流します

## 方言を有効にする条件

方言は、実際のコードで試しを通るまで有効にしません（[adapters-sql.ja.md](../design/adapters-sql.ja.md) の「方言を有効にする条件」）。

- 方言ごとの文法は `internal/adapter/sql/grammar_<方言>.go` がビルドに入れます。有効でない方言のファイルには `//go:build sqlgrammars` があり、ふつうのビルドには入りません
- 有効でない方言の読み手を試すときと、その単体テスト（`dialects_test.go`）を流すときは、タグを付けます: `go test -tags sqlgrammars ./internal/adapter/sql/`
- CI は、タグを付けたテストと vet を別のジョブで流します。有効でない方言の読み手も、プルリクエストのたびに確かめられます
- 方言を有効にするときは、次の2か所を変えます。`grammar_<方言>.go` の `//go:build` の行を消すことと、`internal/config` の `SQLDialects` に方言を足すことです。`SQLDialects` に載った方言が有効な方言です
- 2か所が食い違うと、`TestTheBuildLinksTheGrammarsOfEnabledDialects` が落ちます
