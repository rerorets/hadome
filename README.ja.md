<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/logo-dark.svg">
    <img src="docs/assets/logo-light.svg" alt="hadome" width="460">
  </picture>
</p>

<p align="center">
  変更で増えた違反と、弱くなったテストを報告するチェッカー。
</p>

<p align="center">
  <a href="LICENSE"><img alt="License: MIT" src="https://img.shields.io/badge/license-MIT-blue.svg"></a>
  <a href="https://github.com/rerorets/hadome/releases"><img alt="Release" src="https://img.shields.io/github/v/release/rerorets/hadome?include_prereleases&sort=semver"></a>
  <img alt="Languages" src="https://img.shields.io/badge/languages-Go%20%7C%20TS%20%7C%20Python%20%7C%20Rust%20%7C%20Java%20%7C%20Kotlin%20%7C%20Terraform%20%7C%20Ansible-555.svg">
</p>

<p align="center">
  <a href="README.md">English</a> · 日本語
</p>

## 概要

hadome（歯止め）は変更をmainと比べ、その変更で増えた違反と、弱くなったテストを報告します。mainにすでにある違反は報告しません。

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/compare-dark.svg">
    <img src="docs/assets/compare-light.svg" alt="mainにある3つの違反は報告せず、変更で増えた3つだけを報告して終了コード1になる例" width="760">
  </picture>
</p>

見るのは、コードの書き方（依存の向き・大きさ・コメント）と、テストの書き方です。テストは本数ではなく、テストが呼んでいる関数の範囲で比べます。テストを消して範囲が狭まれば、後退として報告します。

AIエージェントがコードの大半を書くリポジトリで使うことを想定しています。結果はJSONでも出せて、エージェントはそれを読んで直します。

Go・TypeScript・Python・Rust・Java・Kotlin・Terraform・Ansibleを、1つのバイナリで読みます。

## はじめかた

どのコマンドもリポジトリのルートで動かします。

```sh
cd path/to/your-repo
```

### 1. 入れる

使っている言語のものを選びます。どれも版を固定して入れます（[配布の設計](docs/design/distribution.ja.md)）。どの入れ方でも、中身は同じバイナリで、設定も同じ`.hadome.yml`です。

以降は`hadome …`と書きます。`npx hadome …`や`go tool hadome …`など、入れた方法の呼び方に読み替えてください。

#### TypeScript / JavaScript

```sh
npm install --save-dev --save-exact hadome   # pnpm・yarn・bun でも同じ（bun add -d -E hadome → bunx hadome check）
npx hadome check
```

#### Python

```sh
uv add --dev hadome==0.2.0                   # pip なら pip install hadome==0.2.0
uv run hadome check
```

#### Go

```sh
go get -tool github.com/rerorets/hadome/cmd/hadome
go tool hadome check
```

ソースからビルドします。Cコンパイラがあれば8言語を読み、無ければGo・Terraform・Ansibleだけを読みます。

#### Java / Kotlin（Gradle）

```kotlin
// build.gradle.kts
plugins { id("io.github.rerorets.hadome") version "0.2.0" }
```

`./gradlew hadomeCheck`で動かします。

#### Java / Kotlin（Maven）

```xml
<!-- pom.xml の <build><plugins> -->
<plugin>
  <groupId>io.github.rerorets</groupId>
  <artifactId>hadome-maven-plugin</artifactId>
  <version>0.2.0</version>
</plugin>
```

`mvn hadome:check`で動かします。

#### Rust

```sh
cargo binstall hadome@0.2.0
hadome check
```

#### それ以外（Terraform・Ansible など）

```sh
brew install rerorets/tap/hadome
hadome check
```

[リリース](https://github.com/rerorets/hadome/releases)のアーカイブから直接入れることもできます。

### 2. AIエージェントに覚えさせる

エージェントが読むファイルに、hadomeの使い方のブロックを書き込みます。ファイルのパスは今いるディレクトリから見たものです。ルート以外で動かすと、そこに別の`CLAUDE.md`ができます。

```sh
hadome agents --write CLAUDE.md
```

- `AGENTS.md`など別のファイルも指定できます。`hadome agents --write AGENTS.md CLAUDE.md`のように並べると、それぞれに書きます。
- ファイルが無ければ作ります。2回目からは同じブロックを書き換えます。
- 日本語のブロックにするときは`--lang ja`を付けます。
- 詳しくは[エージェント向けの手引き](docs/for-agents.ja.md)にあります。

手で足すときは、次のブロックを、最初と最後の`<!-- hadome:begin -->`と`<!-- hadome:end -->`の行も含めてそのまま貼り付けます。この2行があると、あとで`hadome agents --write`が同じブロックを書き換えられます。

<details>
<summary>ブロックの中身</summary>

```markdown
<!-- hadome:begin -->
## hadome

This repository uses hadome to stop regressions in code and tests.

- Before you finish any change, run `hadome check --format json` and fix every regression it reports.
- When `.hadome.yml` has `mutation`, also run `hadome mutate --format json` before you finish, after `check` exits `0`, and add a test for every mutant it reports. Its exit codes are `check`'s.
- Exit codes: `0` done · `1` regression, fix it · `2` configuration error, fix the configuration · `3` a person must review (a new exception, a loosened configuration, code that stopped being pure, or something hadome could not identify). On `3`, stop and ask. Never change the code just to make it pass.
- When you ask about `3`, give what hadome listed (the rule and its `why`), why you made that change, and the other way you would take if it is not accepted, with what that costs. Leave the decision to the person; do not tell them it is fine.
- Never add `gate:allow` to get a check to pass. Add one only when the rule truly does not apply, with a specific reason, and tell the person.
- When a test guards a product-specific invariant, add `guards: <id>` to its doc comment; for `durable`, `erasable`, `append-only` and `fail-closed`, add `guards: <type> <target>`. `idempotent`, `round-trip` and `isolated` are recognised from the test's calls. `hadome explain invariants` lists them.
- When `hadome check` lists invariant candidates and one covers behaviour that matters, propose it to the person instead of adding it yourself.
- Before changing `.hadome.yml`, run `hadome explain <key>`. After changing it, run `hadome config check`.
<!-- hadome:end -->
```

</details>

## 出力の例

`store.Append`の唯一のテストを消し、別のテストにsleepを足し、依存の図に無いimportをした変更の結果です。

```console
$ hadome check --base main
✗ deps/not-in-diagram          import not in the diagram: store → core (no arrow store --> core)
  store/store.go:3
  why: the diagram (docs/deps.md) is how people and agents know what may depend on what; an import it does not draw makes it wrong
  fix: depend on what the diagram allows, often through an interface the importing unit declares; if the design changed, draw the arrow, which a person reviews
✗ tests/no-sleep               test waits by sleeping: time.Sleep(10 * time.Millisecond)
  worker/worker_test.go:11
  why: a sleep is either too long (a slow suite) or too short (a flaky test); it never waits for the actual event
  fix: wait for the event itself: a channel, a callback, or a condition with a deadline that fails loudly
✗ tests/scope-shrunk           guarded scope shrank: example.com/app/store.Append is no longer exercised by any active test (was: TestAppendKeepsRows)
  store/store_test.go:9
  why: the behaviour of example.com/app/store.Append can now change without any test noticing
  fix: keep or restore a test that calls example.com/app/store.Append, or remove example.com/app/store.Append itself if it is no longer needed
3 regression(s). 0 existing finding(s) unchanged (not blocking).
```

出力は既定で英語です。`--lang ja`を付けると日本語で出ます。日本語になるのは、見つけたものの文言（`message`・`why`・`fix`）、`hadome explain <ルール>`の説明、最後のまとめの行です。JSONの鍵、ルールのID、終了コードは変わりません。毎回付けずに日本語にする設定は、下の「設定と例外」にあります。

## 検査するもの

| 分類 | ルール |
|---|---|
| `tests/` | `scope-shrunk`（テストが守る範囲の縮小）・`invariant-unguarded`・`no-clock`・`no-sleep`・`always-true`・`skip-without-reason`・`no-mock`（純粋なコードのテストのモック）・`guards-nothing`（本体を呼ばないテスト）・`no-assertion`（assert の無いテスト）・`duplicate`（本体が同じテスト）・`purity-lost`と`target-unknown`（確認待ち） |
| `deps/` | `not-in-diagram`（依存の図に無いimport）・`diagram-stale`（何も指さない箱） |
| `size/` | `file-lines`・`function-lines`・`params`・`complexity`・`interface-methods` |
| `comments/` | `todo-format`（いつやるかの無いTODO）・`missing-doc`・`broken-reference`（無い文書を指すコメント） |
| `unknown/` | `unread`（確認待ち）: hadomeが読めなかったファイル・`import`（確認待ち）: 行き先が実行するまで決まらないimport |
| `mutation/` | `survived`（`hadome mutate`だけ）: テストが気づかなかった壊し方 |
| `config/` | `loosened`（確認待ち）: 自分を判定するルールを緩める変更 |

どのルールもなぜ問題かと直し方を一緒に出します。ルールの仕様は[docs/design](docs/design)にあります。

## 後退と整理

テストが3本減ったとき、それが整理なのか後退なのかは、本数だけでは分かりません。hadomeは、テストが呼んでいる関数やメソッド（対象）を割り出し、有効なテストが届く対象の集合を「守っている範囲」として比べます。

| やったこと | 本数 | 守っている範囲 | 判定 |
|---|---|---|---|
| 重複していた3本を1本にまとめた | 減る | 変わらない | 整理 |
| コードを消し、そのテストも消した | 減る | 対象ごと消えた | 整理 |
| テストだけを消した・弱くした | 減るか同じ | 狭まった | 後退 |

要らなくなった振る舞いは、テストだけでなくコードごと消すと、整理として通ります。

テストを3つの段（不変条件・記載ルール・門番）で考える背景は、[考え方](docs/model.ja.md)にあります。

## 使う場面と終了コード

| 場面 | 呼ぶもの | 返ってくるもの |
|---|---|---|
| エージェントのループの中 | `hadome check --format json` | 後退ごとに、ルール・ファイルと行・なぜ困るか・直し方 |
| pushの前 | `hadome check`（pushの前のフックとして） | 読みやすい一覧 |
| プルリクエストごとのCI | `hadome check --base origin/main` | 終了コードと、コメントに貼れる要約 |
| 変更を仕上げる前（手元） | `hadome mutate` | テストが気づかなかった壊し方の一覧 |

| 終了コード | 意味 |
|---|---|
| `0` | 後退なし |
| `1` | 後退が1つ以上ある |
| `2` | 設定の誤り。検査を信じられないので通さない |
| `3` | 後退は無いが、人の確認が要るもの（新しい例外、ルールを緩める設定など）がある |

hadomeはリポジトリを読むだけで、GitHubなどのサービスとは通信しません。要約をコメントに貼るなら、CIの側で貼ります。GitHub Actions の例は[CI で使う](docs/ci.ja.md)にあります。

## 設定と例外

設定は`.hadome.yml`に書きます。上限の数字、依存の図、不変条件などです。ファイルが無くても動きます。

`hadome init`で最小の`.hadome.yml`を作れます。すでにあるファイルは上書きしません。出力をいつも日本語にするなら、そこに`messages: ja`を書きます。`--lang`を付けたときは、`--lang`を優先します。

設定を変えたり、不変条件を提案したりするのはエージェントの役目です。エージェントは`hadome explain <キー>`で仕様を読みます（[エージェント向けの手引き](docs/for-agents.ja.md)）。人は提案を採るかどうかと、ルールを緩める変更を認めるかどうかを決めます。緩める変更には、終了コード`3`で人の確認が入ります。

### 例外の書き方

当てはまる場所に、理由と一緒に書きます。理由が無ければ効きません。

```go
// gate:allow tests/no-sleep reason: OS のファイル監視には待てるイベントが無い
time.Sleep(50 * time.Millisecond)
```

### 自分のプロダクトだけのルール

「ログの置き場にUPDATEを書かない」のようなルールは、自分のリポジトリに小さなプログラムとして書きます。言語は問いません。hadomeはそれを組み込みのルールと同じように扱い、後退だけを報告し、理由つきの例外も効かせます。書き方は[製品固有の門番](docs/external-gates.ja.md)にあります。

## ミューテーションを試す

`.hadome.yml`に`mutation`を書くと、`hadome mutate`で、変更が触れた関数を1か所ずつ壊し、テストが気づくかを試せます。どのテストも気づかなかった壊し方は、`mutation/survived`の後退として報告します。

```yaml
mutation:
  test: [go, test, ./...]
```

壊し方ごとにテストを1回動かすので、`check`よりずっと時間がかかります。手元で、変更を仕上げる前に回すことを想定しています。詳しくは[エージェント向けの手引き](docs/for-agents.ja.md)にあります。

## 名前の由来

歯止めはラチェットの爪（英語でpawl）のことです。歯車を一方向にだけ回し、逆には戻さない部品です。hadomeもmainより悪くなる向きの変更だけを止めます。

## 版と不具合の報告

0.xの間は、版の間で挙動が変わることがあります（[版の付け方](CONTRIBUTING.ja.md#版の付け方)）。不具合や要望は[Issues](https://github.com/rerorets/hadome/issues)へどうぞ。

## ライセンス

[MIT](LICENSE)
