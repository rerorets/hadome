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

## SLL で読めるようにする直し

SQL のアダプタは、まず SLL で読み、読めなかった文だけを LL で読み直します（[adapters-sql.ja.md](../design/adapters-sql.ja.md) の「読み方」）。SLL は、見分けられない選択肢があると最初の選択肢を選びます。そのため、正しい文でも SLL で読めないことがあります。よく書く形がそうなるときは、文法を直して SLL で読めるようにします。

| 方言 | 形 | 直し方 |
|---|---|---|
| MySQL | 関数の呼び出し（`NOW(6)`、`s.f(1)`）。文の間に `;` が要らないので、SLL は列の名前と次の文の `(` とみなす | `selectElement` の列の選択肢に述語 `calledName` を付け、名前の直後が `(` なら列として読まない |
| BigQuery | 括弧の中の OR（`a AND (b IS NULL OR c = 1)`）。SLL は OR の左側に OR をすべて読ませて失敗する | `expression_maybe_parenthesized_not_a_query` の最初に、述語 `orInParentheses` を付けた `expression` の選択肢を足す |

- **直しても木を変えない。** 読めた文から hadome が作る木（`Node`）は、元の文法で読んだときと同じにします。選択肢を足して木が1段深くなるときは、`antlrGrammar` の `inlined` に書き、変換でその段をはずします。BigQuery の上の直しがこれにあたります
- **木が変わる入力。** MySQL の直しで LL の結果が変わるのは、`;` の無い2つの文が `SELECT f ((SELECT 1))` の形でつながる入力だけです。元の文法は列の `f` と括弧の中の次の文として読み、直した文法は `f` の呼び出しとして読みます。括弧の無い名前（`SCHEMA`、`CURRENT_USER`）は、どちらの文法でも列として読みます
- **述語。** 述語の関数は、`.g4` の `@parser::members` に書きます。述語は先読みのトークンだけを見て、文法の状態は見ません
- **試し。** 直したら、次の3つを確かめます
  - `TestStatementsWithParenthesesReadInSLL` が、直した形を SLL で読むこと
  - 元の文法と直した文法で、hadome のテストと手元の実際のリポジトリの `.sql` から作る木が同じこと
  - 同じリポジトリで、`hadome check` の出力が同じこと

**SLL で読めない形（直していないもの）**

| 方言 | 形 | 直さない理由 |
|---|---|---|
| MySQL | `UNION` でつないだ問い合わせ（`SELECT 1 UNION SELECT 2`） | 文法では `querySpecificationNointo` も `UNION` を持てるので、どこで区切るかを決めるのに LL が要る。SLL で読めるように直すと、3つ以上をつないだ問い合わせの木の形が変わる |
| PostgreSQL | 2つ以上の値を括弧に並べた行（`(a.x, a.id) < (b.x, b.id)`） | 文法の `implicit_row` は `( expr_list , a_expr )` なので、最後の値の前で `expr_list` を終えるのに LL が要る。SLL で読めるように直すと、`implicit_row` の木の形が変わる |

## 方言を有効にする条件

方言は、実際のコードで試しを通るまで有効にしません（[adapters-sql.ja.md](../design/adapters-sql.ja.md) の「方言を有効にする条件」）。

- 方言ごとの文法は `internal/adapter/sql/grammar_<方言>.go` がビルドに入れます。有効でない方言のファイルには `//go:build sqlgrammars` があり、ふつうのビルドには入りません
- 有効でない方言の読み手を試すときと、その単体テスト（`dialects_test.go`）を流すときは、タグを付けます: `go test -tags sqlgrammars ./internal/adapter/sql/`
- CI は、タグを付けたテストと vet を別のジョブで流します。有効でない方言の読み手も、プルリクエストのたびに確かめられます
- 方言を有効にするときは、次の2か所を変えます。`grammar_<方言>.go` の `//go:build` の行を消すことと、`internal/config` の `SQLDialects` に方言を足すことです。`SQLDialects` に載った方言が有効な方言です
- 2か所が食い違うと、`TestTheBuildLinksTheGrammarsOfEnabledDialects` が落ちます
