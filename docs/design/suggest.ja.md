# 不変条件の候補と入口

## 責務

この文書が扱うのは、次の2つのための部分です。

- 人が hadome を設定しなくて済むようにする
- エージェントが hadome を自分で探り当てなくて済むようにする

| 名前 | 何か |
|---|---|
| **不変条件の候補** | 不変条件を持つことが多いのに、まだ誰も書き出していないコードの場所（パッケージ `internal/suggest`） |
| **`hadome suggest`** | 候補を、`.hadome.yml` にそのまま貼れるエントリとして出す |
| **`hadome agents`** | エージェントに hadome を伝えるブロックを出す、またはファイルに書き込む |
| **`hadome init`** | 最小の `.hadome.yml` を書く |

しないこと:

- 何かを決めること。候補は情報です。終了コードを変えず、hadome が自分で設定に足すこともありません
- 候補が正しいかを判断すること。例: `CreatePost` という関数が冪等である必要はないかもしれません。決めるのは人です

## 候補

候補はテストでも生成されたものでもないコードのシンボルから見つけます。数えるのは公開されたシンボルだけです。不変条件はそれらの入口について書くものだからです。

| 種類 | シンボルが候補になる条件 |
|---|---|
| `idempotent` | 名前が `Create`、`Add`、`Insert`、`Register`、`Submit`、`Enqueue` で始まる |
| `erasable` | 名前が `Delete`、`Remove`、`Erase`、`Purge` で始まる |
| `isolated` | 名前が `Get`、`List`、`Find`、`Search`、`Read`、`Load`、`Fetch`、`Query` で始まり、範囲の引数を持つ。範囲の引数は、名前が user、tenant、owner、account、org、team、workspace、customer、project を表す引数（[rules-tests.ja.md](rules-tests.ja.md) を参照） |
| `round-trip` | 同じユニット・同じレシーバにある組の1つ目である。組は `Export`/`Import`、`Encode`/`Decode`、`Marshal`/`Unmarshal`、`Serialize`/`Deserialize`、`Save`/`Load` の後に、同じ残りの名前が続くもの。例: `ExportRows` と `ImportRows` |

- **「で始まる」**: その語が名前全体であるか、後に大文字か数字が続くことを指します。例: `Address` は `Add` で始まるとはみなしません
- **候補から外すもの**: 同じ種類の設定済みの不変条件がすでに名指ししているシンボルは、候補になりません
- **対象の書き方**: 候補の対象は設定と同じ書き方で書きます。ユニットのパスの最後の要素に、名前を続けます。例: `api.CreatePost`、`store.Store.Delete`

### 候補が出る場所

| 場所 | どの候補か | 理由 |
|---|---|---|
| `hadome check` | この変更で新しく出たもの: head では候補で、base では候補でなかったもの | チェックのほかのすべてと同じく、変更についての話だから。前からある候補を毎回繰り返さない |
| `hadome suggest` | 作業ツリーのすべて | 不変条件を探す人やエージェントは一覧全体が欲しい |

どちらも終了コードを変えません。

```mermaid
classDiagram
  class Candidate {
    +Type string
    +Targets []string
    +File string
    +Line int
    +Why string
  }
  class Engine {
    +Candidates func(Input) []Candidate
  }
  class Report {
    +Candidates []Candidate
  }
  class suggest {
    <<package>>
    +Candidates(in engine.Input) []engine.Candidate
  }
  Engine ..> Candidate : base と head
  Report --> Candidate : head で新しいもの
  suggest ..> Candidate
```

エンジンが知っているのは、関数の型だけです。`cmd/hadome` が `suggest.Candidates` を渡すので、エンジンは候補を出すロジックに依存しません。

## `hadome suggest`

作業ツリーをその設定で解析し、候補を `invariants:` のエントリとして出します。

- 各エントリには、どこにあり、なぜ候補にしたかを書いたコメントが付きます
- 受け入れるには、エントリを `.hadome.yml` に写します

## `hadome agents`

| 形 | すること |
|---|---|
| `hadome agents` | ブロックを出す |
| `hadome agents --lang ja` | 日本語で出す |
| `hadome agents --write <file>...` | ブロックを、並べたファイルそれぞれに入れる |

英語のブロックは README のクイックスタートにあるものと同じです。両者が食い違うと、テストが落ちます。

### ファイルに入れる

書き先は、必ず人かエージェントが指定します。hadome は書き先を探しません。

- エージェントが読むファイルの名前は、ツールごとに違います。例: `AGENTS.md`、`CLAUDE.md`、`.github/copilot-instructions.md`
- その名前は増えたり変わったりします
- hadome がその一覧を持つと、名前が変わるたびに直し続けることになります

```sh
hadome agents --write AGENTS.md CLAUDE.md
```

| ファイルの状態 | すること |
|---|---|
| 印（`<!-- hadome:begin -->` と `<!-- hadome:end -->`）がある | 印の間を置き換える |
| 印が無い | ファイルの末尾にブロックを足す |
| ファイルが無い | ファイルを作り、ブロックだけを入れる |
| ファイルを置くディレクトリが無い | 失敗にする。打ち間違い（`.githbu/`）でディレクトリができるのを防ぐため |
| 印が片方しか無い | 失敗にする。どこまでが hadome のブロックか分からないため |

- **パス**: 相対でも絶対でもかまいません。相対パスはコマンドを実行したディレクトリから見た場所を指します
- **書く前の確認**: 並べたファイルを、書く前にすべて確かめます。1つでも失敗にあたるものがあれば、どのファイルも書き換えません。問題をすべて出して、終了コード `2` を返します。半分だけ書き換わった状態を残さないためです
- **書けたとき**: 書いたファイルを1行ずつ出して、終了コード `0` を返します

## `hadome init`

`.hadome.yml` を書きます。中身は次のとおりです。既存のファイルは上書きしません。

- `schema: 1`
- 見つけた言語のコメント
- `hadome suggest` と `hadome explain` を指すコメント

**言語の見つけ方**: ファイルの拡張子で見つけます。Ansible の YAML は拡張子では見分けられません。そのため、Ansible のアダプタが受け持つ配置にファイルがあれば、Ansible を見つけたとします。例: `roles/…`、`playbooks/…`、ルートの `site.yml` と `playbook.yml`。受け持つかどうかはアダプタの決め方をそのまま使って判断します。

**`mutation` の設定**: これもコメントにして書きます（[mutation.ja.md](mutation.ja.md)）。

- 見つけた言語ごとに、よく使うテストのコマンドを1行ずつ並べます
- 利用者は1つを残し、そのコメントを外して使います
- 推測したコマンドなので、有効にはしません

| 言語 | テストのコマンド |
|---|---|
| Go | `[go, test, ./...]` |
| TypeScript | `[npm, test]` |
| Python | `[python, -m, pytest]` |
| Rust | `[cargo, test]` |
| Java、Kotlin | `[./gradlew, test]` |
| Terraform、Ansible | 書かない。壊す関数が無い |

hadome はこのファイルが無くても動きます。`init` は近道で、必須ではありません。

## 不変条件

1. 候補は終了コードを変えない
2. `hadome agents --write` を2回実行しても、ファイルは1回目の後と同じまま
3. `hadome agents --write` が失敗したときは、どのファイルも書き換わっていない
4. `hadome init` はファイルを上書きしない

## 意図して入れなかったもの

- **コードの振る舞いから出す候補**: 例: 何を書くか、どの表に触るか。事実が持っているのは、名前と引数だけです
- **候補を受け入れる対話的なモード**: 出力はそのまま有効な設定で、受け入れるのは貼るだけで済みます
- **`hadome agents` の書き先を探すこと**: 理由は「ファイルに入れる」にあります
- **`hadome agents` の書き先を対話で聞くこと**: `hadome agents` をいちばんよく実行するのはエージェントで、エージェントは問いに答えられません。CI でも止まってしまいます
