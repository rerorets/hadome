# エージェント向けの hadome

hadome を使うリポジトリで作業するエージェント向けの手順です。どの手順もうまくいったかを確かめるコマンドで終わります。

この手順を短くまとめたものは `hadome agents` が出します。置き場所はエージェントへの指示を書いたファイルです（例: `AGENTS.md`、`CLAUDE.md`）。

## 変更を終える前に

```sh
hadome check --format json
```

| 終了コード | 意味 | すること |
|---|---|---|
| `0` | 何も悪くなっていない | 終える |
| `1` | 後退がある | `regressions` の各エントリを直し、もう1度チェックを実行する |
| `2` | 設定が不正、またはチェックを実行できなかった | 標準エラーが挙げたものを直し、もう1度チェックを実行する。挙がるのは、ファイル、行、キー、許される値、例 |
| `3` | 人の確認が要るものがある。`reviews` か `new_exceptions` に入っている | 止まる。挙がったものとその理由を人に伝える。消すためにコードを変えない |

JSON の出力には、次のものが入ります。

- **後退のエントリ**: それぞれに `rule`、`file`、`line`、`message`、`why`、`fix` が付きます。`fix` は、その後退を解消する最小の変更です。
- **日本語の出力**: `.hadome.yml` に `messages: ja` があるか、`--lang ja` を付けると、`message`、`why`、`fix` の値が日本語になります。キー、`rule`、終了コードは変わりません。
- **ルールの説明**: ルールそのものを知るには `hadome explain <rule>` を実行します。例: `hadome explain tests/no-sleep`。
- **`candidates`**: その変更で増えた、不変条件の候補が並びます。終了コードは変えません（下の「不変条件の候補」を参照）。
- **`unfollowed`**: 呼び出しの図（`rules.deps.calls`）があるとき、型の分からない値の呼び出しが増えたユニットが並びます。終了コードは変えません。型の注釈を書くと、hadome がたどれるようになります。
- **`introduced`**: `.hadome.yml` を初めて足す変更で、既定の値から変えた設定が並びます。hadome を入れる変更なので、設定を緩めても `3` にはなりません。この一覧も終了コードを変えません。人に見せる報告にはこの一覧を添えます。

落ちるのは後退だけです。基準のブランチの時点ですでに見つかっていたものは `untouched` に数え、止めません。直す必要はありません。直すなら別の変更にします。

確かめ方: `hadome check` が `0` で終わる。

## ミューテーションを試す

`.hadome.yml` に `mutation` があるときだけ実行します。`hadome check` が `0` で終わったあと、変更を終える前に1回実行します。

```sh
hadome mutate --format json
```

| 終了コード | 意味 | すること |
|---|---|---|
| `0` | 変更で触ったコードを壊すと、どれもテストが落ちた | 終える |
| `1` | テストが気づかない壊し方がある | `regressions` の各エントリの `fix` に沿ってテストを足し、もう1度実行する |
| `2` | 何も壊していない状態でテストが落ちた、または設定が不正 | 先にテストを直す。`mutation` の設定は消さない |
| `3` | 人の確認が要る。例: 上限を超えて試せなかった分がある | 止まる。挙がったものを人に伝える |

- 本体のコードを変えてテストを通すことはしません。`1` は、テストが足りないという知らせです。
- `gate:allow mutation/survived reason: <理由>` を書くのは、振る舞いを変えない置き換え（等価なミュータント）だと確かめたときだけです。理由には、なぜ結果が変わらないかを書きます。
- 遅いからといって、`mutation` の設定を消したり弱めたりしません。そうした変更は `3` になります。

確かめ方: `hadome mutate` が `0` で終わる。

## 人に確認を頼む

`3` のときは、挙がったものを人が判断します。判断に要るものを渡し、結論は渡しません。

| 書くこと | 例 |
|---|---|
| hadome が挙げたもの: ルール、ファイル、`why` | `config/loosened`: `rules.size.params` が 5 から 6 になる |
| その変更をした理由 | `judge` の引数を型にまとめるより、読みやすいと考えた |
| 受け入れられないときに取る別のやり方と、その手間 | 2つの版と事実を1つの型にまとめる。直すのは `judge` と呼び出し元の2か所 |

- 人の判断を先に決めません。例: 「問題ありません」「承認済みです」と書かない。
- 受け入れてもらう前に、その先の作業を進めません。
- 別のやり方は実際に取れるものを1つ以上書きます。思いつかないときは、そう書きます。

確かめ方: 人の返事を受けて、受け入れなら作業を続け、取り下げなら書いた別のやり方で直し、もう1度 `hadome check` を実行する。

## 例外を書く

例外の印を書くのは、ルールがその行に本当に当てはまらないときだけです。チェックを通すためには書きません。

```go
time.Sleep(d) // gate:allow tests/no-sleep reason: the OS file watcher offers no event to wait on
```

- 印はその行に効きます。印だけの行なら、次の行に効きます。
- `reason:` には、なぜここでルールが当てはまらないかを具体的に書きます。理由の無い印は効きません。
- 新しい印はどれもチェックを `3` にするので、人の目に入ります。なぜ足したかを人に伝えます。

確かめ方: `hadome check` がその印を `new_exceptions` に挙げ、その行で見つかったものをもう挙げない。

## 不変条件を足す

不変条件は、製品が守る約束です。足すのは、人が求めたときか、提案して了承を得たあとだけです。

1. 型の意味を読む: `hadome explain invariants`、続けて `hadome explain <type>`
2. `.hadome.yml` にエントリを足す:

   ```yaml
   invariants:
     - type: idempotent        # 組み込みの型とその対象
       target: api.CreatePost
     - id: billing-1           # 製品固有。合う組み込みの型が無いときだけ
       says: "Totals on the screen and in the API match"
   ```

   対象はユニットのパスの最後の要素と名前で書きます。コードから呼ぶときと同じ書き方です。例: `api.CreatePost`、`store.Store.Delete`。
3. `hadome config check` を実行する
4. それを守るテストがあることを確かめる（次の節）。守るテストの無い新しい不変条件は、後退になる

確かめ方: `hadome config check` が `0` で終わり、`hadome check` が `tests/invariant-unguarded` を報告しない。

## テストに不変条件を守らせる

| 型 | テストがすること |
|---|---|
| `idempotent` | 対象を2回以上呼ぶ。ループの中の呼び出しは2回と数える |
| `round-trip` | 組になった対象をすべて呼ぶ |
| `isolated` | 対象を、異なる2人の利用者かテナントで呼ぶ。具体的には、名前が user、tenant、owner、account、org、team、workspace、customer、project を表す引数に、異なる2つの値を渡す |
| `durable`、`erasable`、`append-only`、`fail-closed` | 対象を呼び、テストの doc コメントに `guards: <type> <target>` と書く |
| 製品固有（`id`） | テストの doc コメントに `guards: <id>` と書く |

```go
// guards: durable store.Save
func TestSavedRowsSurviveRestart(t *testing.T) {
```

- **書く場所**: どの言語でも、テストの直前のコメントに書きます。Python では docstring でもかまいません。
- **`guards:` の値**: メッセージが不変条件を呼ぶとおりに書きます。`<type> <target>`、対象が複数なら `<type> <a>,<b>`、製品固有なら `id` です。
- **スキップしたテスト**: 何も守りません。
- **ヘルパー関数**: テストファイルのヘルパー関数を通した呼び出しも数えます。

この形を満たしていても、テストが不変条件を確かめているとは限りません。不変条件が破れたら落ちるテストを書きます。

確かめ方: `hadome check` がそれについて `tests/invariant-unguarded` を報告しない。

## 大きさの上限に当たったら

`size/` の後退は製品コードのファイル、関数、インターフェースのどれかが上限を超えたか、base で既に上限を超えていたものがさらに大きくなったことを指します。

すでにある切れ目で分けます。

- 段階ごとに関数を分ける
- 型ごとにファイルを分ける
- 呼び出し側ごとにインターフェースを分ける

`.hadome.yml` の上限は上げません。全員に対してルールを緩めることになるので、`3` になります。

確かめ方: `hadome check` が `size/` の後退を報告しない。

## コメント

- `TODO` にはいつやるか、または誰がやるかを、`.hadome.yml` が決めた形で書きます。既定の形は `TODO(<when or who>): <what>` です（`hadome explain rules.comments.todo` で確かめられます）。`FIXME` と `XXX` は受け付けません。
- 製品コードの公開された関数、メソッド、型にはすべて、その名前で始まる doc コメントを付けます。
- コメントで挙げた Markdown ファイルは、実在していなければなりません。リポジトリのルートからなら `docs/design/x.md`、コメントのあるファイルからなら `./x.md` と書きます。

確かめ方: `hadome check` が `comments/` の後退を報告しない。

## 依存

`.hadome.yml` が依存の図を名指ししているとき（`rules.deps.diagram`）、リポジトリのユニット間の import は、図の矢印に沿っていなければなりません。

- `deps/not-in-diagram` が出たら、図が許すものに依存するようにコードを変えます。たいていは、import する側のユニットが宣言するインターフェースを通します。
- チェックを通すために、図に新しい矢印を描きません。設計を変えることになるので `3` になります。
- 呼び出しの図（`rules.deps.calls`）があるときは、ユニット間の呼び出しも、その図の矢印に沿っていなければなりません。`deps/call-not-in-diagram` が出たら、図が許す層を通して呼ぶようにコードを変えます。クラス `wiring` もチェックを通すためには付けません。付けると `3` になります。
- 図が無くても、ユニットの間に新しい循環を作る import は `deps/cycle` で落ちます。出たら、どちらの向きが正しいかを決め、逆向きの import を消します。`rules.deps.cycles.allow` に組を足してチェックを通すことはしません。足すと `3` になります。
- `.hadome.yml` に層（`rules.deps.layers`）があるときは、内側の層から外側の層への import が `deps/layer` で落ちます。出たら、内側の層がインターフェースを宣言し、外側の層がそれを実装する形に変えます。層を書き換えてチェックを通すことはしません。書き換えると `3` になります。
- 図が無いときは、`hadome suggest diagram` と `hadome suggest calls` で叩き台を作り、人に提案します。叩き台は、いまの import と呼び出しをそのまま描いたものです。設定しても、いまのコードは落ちません。箱のまとめ方（`--depth`）や、つなぎ込みの箱に付ける `wiring` は、人と決めます。
- [設計のパターンを守る](patterns.ja.md)は、リポジトリがすでに使っている設計を、図・層・許す組に書き写すときの参考です。パターンに合わせてコードを組み替えることは勧めません。
  - 読むのは、人に頼まれたときと、リポジトリの文書（README、設計書、AGENTS.md など）に、その設計で作っていると書いてあるときです。
  - 最初に図を提案するとき（`.hadome.yml` に `rules.deps.diagram` も `rules.deps.layers` も無いとき）だけ、何かの設計に沿っているか、沿わせたいかを軽く聞きます。叩き台から見えた形があれば、「こう見えました」と添えます。沿っていない、または決めていないという答えなら、それ以上は勧めません。
  - 書き写す案を出すときは、叩き台で、いまその形に反している矢印を数えて添えます。いまある違反は落ちないので、その数は「これ以上は増やさない」線になります。
  - パターンに合わせるためにディレクトリや import を変える提案は、人に頼まれたときだけにします。

確かめ方: `hadome check` が `deps/` の後退を報告しない。

## 不変条件の候補

`hadome check` は `candidates` に、その変更で増えた、不変条件を持つことが多い場所を挙げます。挙がるのは、作る、消す、書き出す・読み込む、利用者ごとに読む、のどれかをする場所です。大事な振る舞いに当たるものがあれば、人に提案します。自分では足しません。

`hadome suggest` は、リポジトリのすべての候補を、そのまま貼れる設定として出します。

## 製品のことを hadome に教える

設定で、hadome の表にエントリを足せます。組み込みのエントリは消せません。

- 前に足したエントリを消すと、ルールが緩むので `3` になります。
- assert とみなす関数（`rules.tests.assertions`）だけは逆で、足すと `3` になります。

| hadome に… | `.hadome.yml` に足すもの |
|---|---|
| 製品独自の時計のラッパーをテストで `time.Now` と同じに扱わせる | `rules.tests.hazards.clock: [example.com/acme/clock.Now]` |
| ヘルパーを sleep として扱わせる | `rules.tests.hazards.sleep: [example.com/acme/testutil.Wait]` |
| モックのライブラリを見分けさせる | `rules.tests.hazards.mock: [example.com/acme/fakes]` |
| ユニットを入出力をするものとして扱わせる | `rules.tests.io: [example.com/acme/db]`。末尾に `/...` を付けると、その下のすべてを含む |

足す前に、キーの説明を読みます。例: `hadome explain rules.tests.io`。

確かめ方: `hadome config check` が `0` で終わる。

## 製品固有のゲートを足す

製品固有のゲートは、その製品だけが持つルールを検査するコマンドです。例: 「ログの置き場に UPDATE を書かない」。リポジトリに置く小さなコマンドで、言語は問いません。やり取りの決まりは [external-gates.ja.md](external-gates.ja.md) にあります。

1. コマンドを書く。コマンドがすることは次のとおり
   - 標準入力から、JSON の要求を1つ読む
   - 作業ディレクトリをルートとする版だけを見る
   - 見つかったもの（measure のゲートなら項目）を JSON で出す。見つかったものにはそれぞれ、`file` と、行番号を含まない `anchor` が要る
   - `--self-test` を付けて呼ばれたら、自分の事例を実行して答える
2. 宣言する:

   ```yaml
   gates:
     - id: acme/log-append-only
       kind: finding
       run: [scripts/gates/log-append-only]   # リポジトリのルートから
       paths: ["internal/store/log/**"]       # 任意
   ```

3. `hadome config check`、続けて `hadome self-test` を実行する

次のゲートは、チェックを終了コード `2` で止めます。

- 失敗したゲート
- 決まりどおりの JSON 以外を出したゲート
- `timeout` を過ぎても動いているゲート

あとからゲートを変えたり消したりすると、`3` になります。

ゲートを足した変更では、`hadome check` はゲートを実行せず、`config/gate-not-run` を挙げて `3` で終わります。まだ誰も確かめていないコマンドを、変更の中の設定だけで動かさないためです。人に確認を頼み、変更が基準のブランチに入ると、以降のチェックでゲートが実行されます。`run` を変えたときも同じです。

確かめ方: `hadome self-test` がゲートの `id` を挙げ、`hadome check` が `config/gate-not-run` でそのゲートを挙げる。

## やってはいけない変更を止める

チームで決めた「やってはいけない変更」のうち、hadome で止められるものと、その書き方です。hadome が止めるのは、base より悪くなった変更だけです。そのため、「ここに触れたら人が見る」という決まりは、「ここを守るテストを減らさない」という決まりに置き換えて書きます。

| 止めたいこと | 書き方 | 止まり方 |
|---|---|---|
| 本番のデータを書き換える SQL（`UPDATE`、`DELETE`） | ゲート（`kind: finding`） | 増えると `1` |
| 鍵、トークン、パスワードの混入 | ゲート（`kind: finding`） | 増えると `1` |
| 外部の AI の API を呼ぶ箇所の追加 | ゲート（`kind: finding`） | 増えると `1` |
| 書いた記録を書き換える変更 | 組み込みの不変条件 `append-only` | 守るテストが無くなると `1` |
| 壊れた資格情報を通してしまう認証・認可の変更 | 組み込みの不変条件 `fail-closed` | 守るテストが無くなると `1` |
| 課金の約束を破る変更 | 製品固有の不変条件 | 守るテストが無くなると `1` |
| 上のどれかを緩める変更（ゲートや不変条件を消す、上限を上げる） | 書かなくても止まる | `3` |

```yaml
gates:
  - id: acme/no-data-rewrite
    kind: finding
    run: [scripts/gates/no-data-rewrite]
    paths: ["db/scripts/**"]
    options:
      forbidden: [UPDATE, DELETE]
  - id: acme/no-secrets
    kind: finding
    run: [scripts/gates/no-secrets]
  - id: acme/ai-calls
    kind: finding
    run: [scripts/gates/ai-calls]
    paths: ["src/**"]

invariants:
  - type: append-only
    target: audit.Append
  - type: fail-closed
    target: auth.Authorize
  - id: billing-1
    says: "An order is charged at most once"
```

```go
// guards: fail-closed auth.Authorize
func TestExpiredTokenIsRejected(t *testing.T) {
```

ゲートを書くときの要点です。

- **見えたものを全部返す。** ゲートは1つの版だけを見て、見つけたものをすべて返します。base と比べるのは hadome です。そのため、すでにある `UPDATE` は止まらず、増えたものだけが止まります。
- **`anchor` は行番号を含めない。** 囲んでいる関数の名前や、空白をそろえた文を使います。行が動いても同じ違反だと分かるようにするためです。
- **鍵そのものを出さない。** 鍵のゲートは `message` と `anchor` に鍵の値を書きません。出力は CI のログに残るからです。`anchor` には、たとえば鍵の種類と変数の名前を使います。
- **決まりごとにゲートを分ける。** そうすると、`gate:allow <id> reason: …` の例外も決まりごとに書けます。

次のものは hadome では止められません。

| 止めたいこと | 止められない理由 |
|---|---|
| 公開された脆弱性情報（CVE など）に載ったパッケージ | 照合に通信が要る。hadome もゲートも通信しない |
| 触れただけで人のレビューに回す決まり（スキーマ、インフラ、環境変数） | hadome は変更の場所ではなく、base より悪くなったかで判断する。パスごとに必須のレビューを決める仕組みを使う |
| 新しい外部の通信先 | 依存の図（`rules.deps`）は、リポジトリの中のユニットの間の import だけを数える |
| 呼び出しの回数や費用の見積もり | 実際の回数や料金は、実行したときの量や料金表で決まり、ツリーからは分からない |

確かめ方: `hadome config check` と `hadome self-test` が `0` で終わり、`hadome check` が足したゲートを `config/gate-not-run` で挙げ、`tests/invariant-unguarded` を報告しない。

## ほかのエージェントに伝える

```sh
hadome agents --write AGENTS.md CLAUDE.md   # 読ませたいファイルを並べる。もう1度実行するとブロックをその場で更新する
```

確かめ方: 2回実行すると、2回目ではファイルが変わらない。
