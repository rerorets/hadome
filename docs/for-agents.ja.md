# エージェント向けの hadome

hadome を使うリポジトリで作業するエージェントのための、正確な手順。どの手順もうまくいったことを示すコマンドで終わる。短い版は、あなたへの指示が書かれたファイルに置く。`hadome agents` がそれを出す。

## 変更を終える前に

```sh
hadome check --format json
```

| 終了コード | 意味 | すること |
|---|---|---|
| `0` | 何も悪くなっていない | 終える |
| `1` | 後退がある | `regressions` の各エントリを直し、もう1度チェックを実行する |
| `2` | 設定が不正、またはチェックを実行できなかった | 標準エラーが挙げたもの（ファイル、行、キー、許される値、例）を直し、もう1度チェックを実行する |
| `3` | 人の確認が要るものがある: `reviews` か `new_exceptions` のエントリ | 止まる。挙がったものとその理由を人に伝える。消すためにコードを変えない |

後退にはそれぞれ `rule`、`file`、`line`、`message`、`why`、`fix` が付く。`fix` はそれを解消する最小の変更である。`.hadome.yml` に `messages: ja` があるか、`--lang ja` を付けたときは、`message`、`why`、`fix` の値が日本語になる。鍵、`rule`、終了コードは変わらない。ルールそのものを知るには `hadome explain <rule>`（例: `hadome explain tests/no-sleep`）を実行する。`candidates` はその変更で増えた不変条件の候補を挙げる。候補は終了コードを変えない（下の「不変条件の候補」を参照）。

落ちるのは後退だけである。基準のブランチにすでにあった見つかったものは `untouched` に数え、止めない。直す必要はなく、直すなら別の変更にする。

確かめ方: `hadome check` が `0` で終わる。

## ミューテーションを試す

`.hadome.yml` に `mutation` があるときだけ。`hadome check` が `0` で終わったあと、変更を終える前に1回実行する。

```sh
hadome mutate --format json
```

| 終了コード | 意味 | すること |
|---|---|---|
| `0` | 変更で触ったコードを壊すと、どれもテストが落ちた | 終える |
| `1` | テストが気づかない壊し方がある | `regressions` の各エントリの `fix` に沿ってテストを足し、もう1度実行する |
| `2` | 何も壊していない状態でテストが落ちた、または設定が不正 | 先にテストを直す。`mutation` の設定は消さない |
| `3` | 人の確認が要る（上限を超えて試せなかった分がある、など） | 止まる。挙がったものを人に伝える |

- 本体を変えてテストを通さない。`1` は、テストが足りないという知らせである
- 振る舞いを変えない置き換え（等価なミュータント）だと確かめたときだけ、`gate:allow mutation/survived reason: <理由>` を書く。理由には、なぜ結果が変わらないかを書く
- 遅いからと `mutation` の設定を消したり弱めたりしない。そうした変更は `3` になる

確かめ方: `hadome mutate` が `0` で終わる。

## 人に確認を頼む

`3` のとき、人は挙がったものを自分で判断する。判断に要るものを渡し、結論は渡さない。

| 書くこと | 例 |
|---|---|
| hadome が挙げたもの: ルール、ファイル、`why` | `config/loosened`: `rules.size.params` が 5 から 6 になる |
| その変更をした理由 | `judge` の引数を型にまとめるより、読みやすいと考えた |
| 受け入れられないときに取る別のやり方と、その手間 | 2つの版と事実を1つの型にまとめる。直すのは `judge` と呼び出し元の2か所 |

- 「問題ありません」「承認済みです」のように、人の判断を先に決めない
- 受け入れてもらう前に、その先の作業を進めない
- 別のやり方は、実際に取れるものを1つ以上書く。思いつかないときは、そう書く

確かめ方: 人の返事を受けて、受け入れなら作業を続け、取り下げなら書いた別のやり方で直し、もう1度 `hadome check` を実行する。

## 例外を書く

ルールがその行に本当に当てはまらないときだけ。チェックを通すためには書かない。

```go
time.Sleep(d) // gate:allow tests/no-sleep reason: the OS file watcher offers no event to wait on
```

- 印はその行に効く。印だけの行なら次の行に効く
- `reason:` には、なぜここでルールが当てはまらないかを具体的に書く。理由の無い印は効かない
- 新しい印はどれもチェックを `3` にするので、人の目に入る。なぜ足したかを人に伝える

確かめ方: `hadome check` がその印を `new_exceptions` に挙げ、見つかったものをもう挙げない。

## 不変条件を足す

不変条件は製品が守る約束である。足すのは人が求めたとき、または提案して了承を得た後だけ。

1. 型の意味を読む: `hadome explain invariants`、続けて `hadome explain <type>`
2. `.hadome.yml` にエントリを足す:

   ```yaml
   invariants:
     - type: idempotent        # 組み込みの型とその対象
       target: api.CreatePost
     - id: billing-1           # 製品固有。合う組み込みの型が無いときだけ
       says: "Totals on the screen and in the API match"
   ```

   対象はユニットのパスの最後の要素と名前で、コードから呼ぶときの書き方にする: `api.CreatePost`、`store.Store.Delete`。
3. `hadome config check` を実行する
4. それを守るテストがあることを確かめる（次の節）。守るテストの無い新しい不変条件は後退になる

確かめ方: `hadome config check` が `0` で終わり、`hadome check` が `tests/invariant-unguarded` を報告しない。

## テストに不変条件を守らせる

| 型 | テストがすること |
|---|---|
| `idempotent` | 対象を2回以上呼ぶ（ループの中の呼び出しは2回と数える） |
| `round-trip` | 組の対象をすべて呼ぶ |
| `isolated` | 対象を2人の異なる利用者かテナントで呼ぶ: 名前が user、tenant、owner、account、org、team、workspace、customer、project を表す引数に、異なる2つの値を渡す |
| `durable`、`erasable`、`append-only`、`fail-closed` | 対象を呼び、テストの doc コメントに `guards: <type> <target>` と書く |
| 製品固有（`id`） | テストの doc コメントに `guards: <id>` と書く |

```go
// guards: durable store.Save
func TestSavedRowsSurviveRestart(t *testing.T) {
```

どの言語でも、テストの直前のコメントに書く（Python では docstring でもよい）。`guards:` の後の値は、メッセージが不変条件を呼ぶとおりに書く: `<type> <target>`、対象が複数なら `<type> <a>,<b>`、または `id`。スキップしたテストは何も守らない。

テストファイルのヘルパー関数を通した呼び出しも数える。この形を満たしていても、テストが不変条件を確かめているとは限らない。不変条件が破れたら落ちるテストを書くこと。

確かめ方: `hadome check` がそれについて `tests/invariant-unguarded` を報告しない。

## 大きさの上限に当たったら

`size/` の後退は、製品コードのファイル、関数、インターフェースが上限を超えたか、既存のものが大きくなったことを指す。すでにある切れ目で分ける: 段階ごとに関数、型ごとにファイル、呼び出し側ごとにインターフェース。`.hadome.yml` の上限を上げない。全員に対してルールを緩めるので `3` になる。

確かめ方: `hadome check` が `size/` の後退を報告しない。

## コメント

- `TODO` には、いつ、または誰がやるかを `.hadome.yml` が決めた形で書く（`hadome explain rules.comments.todo`。既定は `TODO(<when or who>): <what>`）。`FIXME` と `XXX` は受け付けない
- 製品コードの公開された関数、メソッド、型にはすべて、その名前で始まる doc コメントを付ける
- コメントで挙げた Markdown ファイルは実在しなければならない: リポジトリのルートからなら `docs/design/x.md`、コメントのあるファイルからなら `./x.md` と書く

確かめ方: `hadome check` が `comments/` の後退を報告しない。

## 依存

`.hadome.yml` が図を名指ししているとき（`rules.deps.diagram`）、リポジトリのユニット間の import は図の矢印に沿わなければならない。`deps/not-in-diagram` が出たら、図が許すものに依存するようコードを変える。たいていは import する側のユニットが宣言するインターフェースを通す。チェックを通すために新しい矢印を描かない。設計を変えることになるので `3` になる。

確かめ方: `hadome check` が `deps/` の後退を報告しない。

## 不変条件の候補

`hadome check` は `candidates` に、その変更で増えた、不変条件を持つことが多い場所を挙げる: 作る、消す、書き出す・読み込む、利用者ごとに読むもの。大事な振る舞いに当たるものがあれば、人に提案する。自分では足さない。

`hadome suggest` は、リポジトリのすべての候補をそのまま貼れる設定として出す。

## 製品のことを hadome に教える

設定は hadome の表にエントリを足せるが、組み込みのエントリは消せない。前に足したエントリを消すと、ルールが緩むので `3` になる。ただし assert とみなす関数（`rules.tests.assertions`）は逆で、足すと `3` になる。

| hadome に… | `.hadome.yml` に足すもの |
|---|---|
| 製品独自の時計のラッパーをテストで `time.Now` と同じに扱わせる | `rules.tests.hazards.clock: [example.com/acme/clock.Now]` |
| ヘルパーを sleep として扱わせる | `rules.tests.hazards.sleep: [example.com/acme/testutil.Wait]` |
| モックのライブラリを見分けさせる | `rules.tests.hazards.mock: [example.com/acme/fakes]` |
| ユニットを入出力をするものとして扱わせる | `rules.tests.io: [example.com/acme/db]`（末尾の `/...` はその下のすべてを含む） |

先にキーを読む: `hadome explain rules.tests.io`。

確かめ方: `hadome config check` が `0` で終わる。

## 製品固有の門番を足す

その製品だけが持つルール（「ログの置き場に UPDATE を書かない」）は、リポジトリに置く小さなコマンドで、言語は問わない。約束事は [external-gates.ja.md](external-gates.ja.md) にある。

1. コマンドを書く。標準入力から JSON の要求を1つ読み、作業ディレクトリをルートとする版だけを見て、見つかったもの（または項目）を JSON で出す。見つかったものにはそれぞれ `file` と、行番号を含まない `anchor` が要る。`--self-test` には自分の事例を実行して答える
2. 宣言する:

   ```yaml
   gates:
     - id: acme/log-append-only
       kind: finding
       run: [scripts/gates/log-append-only]   # リポジトリのルートから
       paths: ["internal/store/log/**"]       # 任意
   ```

3. `hadome config check`、続けて `hadome self-test` を実行する

失敗した門番、約束事の JSON 以外を出した門番、`timeout` を過ぎても動いている門番は、終了コード `2` でチェックを止める。後から門番を変える・消すと `3` になる。

確かめ方: `hadome self-test` が門番の `id` を挙げ、`hadome check` がそれを実行する。

## ほかのエージェントに伝える

```sh
hadome agents --write AGENTS.md CLAUDE.md   # 読ませたいファイルを並べる。もう1度実行するとブロックをその場で更新する
```

確かめ方: 2回実行すると、2回目ではファイルが変わらない。
