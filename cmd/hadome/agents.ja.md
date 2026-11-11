<!-- hadome:begin -->
## hadome

このリポジトリは hadome でコードとテストの後退を止めている。

- 変更を終える前に `hadome check --format json` を実行し、報告された後退をすべて直す。
- `.hadome.yml` に `mutation` があれば、`check` が `0` で終わったあと、変更を終える前に `hadome mutate --format json` も実行し、報告されたミュータントごとにテストを足す。終了コードは `check` と同じ。
- 終了コード: `0` 完了 · `1` 後退があるので直す · `2` 設定の誤りなので設定を直す · `3` 人の確認が要る（新しい例外、緩めた設定、純粋でなくなったコード、hadome が特定できなかったもの）。`3` なら止まって人に聞く。通すためだけにコードを変えない。
- `3` で人に聞くときは、hadome が挙げたもの（ルールとその `why`）、その変更をした理由、受け入れられないときに取る別のやり方とその手間を書く。決めるのは人に任せ、「問題ない」と言わない。
- チェックを通すために `gate:allow` を足さない。足すのはルールが本当に当てはまらないときだけで、具体的な理由を書き、人に伝える。
- テストが製品固有の不変条件を守るときは、doc コメントに `guards: <id>` を書く。`durable`・`erasable`・`append-only`・`fail-closed` は `guards: <type> <target>` を書く。`idempotent`・`round-trip`・`isolated` はテストの呼び出しから自動で見つかる。一覧は `hadome explain invariants`。
- `hadome check` が不変条件の候補を出し、それが大事な振る舞いに当たるなら、自分で足さずに人に提案する。
- `.hadome.yml` を変える前に `hadome explain <key>` を実行し、変えた後に `hadome config check` を実行する。
- ある種類の変更を止めたいと人に頼まれたら（例: 本番のデータを書き換える SQL、コードに混ざった鍵）、門番か不変条件で書く。例は次にある。 https://github.com/rerorets/hadome/blob/main/docs/for-agents.ja.md#やってはいけない変更を止める
<!-- hadome:end -->
