# CI で hadome を使う

プルリクエストごとに、CI で `hadome check` を実行する手順です。hadome 自身の CI も同じ形で動いています。例: [ci.yml](../.github/workflows/ci.yml) の `hadome` のジョブ。

## 終了コードの扱い

| 終了コード | ジョブ | 出すもの |
|---|---|---|
| `0` | 通す | なし。前に出した確認のコメントがあるときは、確認の要るものが無くなったと書き換える |
| `1` | 落とす | 後退の一覧（ログ） |
| `2` | 落とす | 設定の誤り、またはチェックを実行できなかった理由（ログ） |
| `3` | 通す | 確認の要るものを、行ごとの警告、ジョブの要約、PR のコメントに出す |

`3` ではジョブを落とさず、マージする人の目に入る場所に出します。

- `3` は後退ではありません。人が判断するものがある、という知らせです。
- プルリクエストでは、その判断をするのはマージする人です。
- `3` でジョブを落とすと、人が確認したあとでも、ブランチの保護を飛ばさないとマージできなくなります。

## GitHub Actions の例

`hadome` の呼び方は入れた方法に合わせて読み替えます。入れ方は README の「入れる」にあります。

- **使えるもの**: `go tool hadome`、`npx hadome`、`uv run hadome`。どれも終了コードをそのまま返します。
- **使わないもの**: `go run`。プログラムの終了コードが `0` 以外なら、どれも `1` にして返すので、`3` と `1` の区別が消えます。

```yaml
name: hadome

on:
  pull_request:

jobs:
  hadome:
    runs-on: ubuntu-24.04
    permissions:
      contents: read
      pull-requests: write # the comment that lists what a person must review
    steps:
      - uses: actions/checkout@v5
        with:
          fetch-depth: 0 # hadome reads the base branch from git
      - uses: actions/setup-go@v6
        with:
          go-version-file: go.mod
      - name: Check with hadome
        id: check
        env:
          BASE: origin/${{ github.base_ref }}
        run: |
          report="$RUNNER_TEMP/hadome.json"
          go tool hadome check --base "$BASE" || true
          code=0
          go tool hadome check --base "$BASE" --format json > "$report" || code=$?
          echo "code=$code" >> "$GITHUB_OUTPUT"
          if [ "$code" -ne 3 ]; then exit 0; fi
          jq -r '
            def esc: tostring | gsub("%"; "%25") | gsub("\r"; "%0D") | gsub("\n"; "%0A");
            (.reviews[] | "::warning file=\(.file),line=\([.line, 1] | max),title=\(.rule)::\((.message + " — " + .why) | esc)"),
            (.new_exceptions[] | "::warning file=\(.file),line=\([.line, 1] | max),title=new exception for \(.rule)::\(("reason: " + .reason) | esc)")
          ' "$report"
          {
            echo "<!-- hadome:review -->"
            echo "## hadome: a person must review"
            echo
            echo "Nothing regressed. Whoever merges this pull request reviews the items below; each is also marked on its line in Files changed."
            echo
            echo "| Rule | Where | What | Why |"
            echo "|---|---|---|---|"
            jq -r '
              def cell: tostring | gsub("\\|"; "\\|") | gsub("\n"; " ");
              def where: if .line > 0 then "\(.file):\(.line)" else .file end;
              (.reviews[] | "| `\(.rule)` | `\(where)` | \(.message | cell) | \(.why | cell) |"),
              (.new_exceptions[] | "| new exception for `\(.rule)` | `\(where)` | reason: \(.reason | cell) | an exception needs a person to agree with its reason |")
            ' "$report"
          } > "$RUNNER_TEMP/review.md"
          cat "$RUNNER_TEMP/review.md" >> "$GITHUB_STEP_SUMMARY"
      - name: Comment what needs review
        if: github.event.pull_request.head.repo.full_name == github.repository
        env:
          GH_TOKEN: ${{ github.token }}
          CODE: ${{ steps.check.outputs.code }}
          PR: ${{ github.event.pull_request.number }}
        run: |
          id=$(gh api "repos/$GITHUB_REPOSITORY/issues/$PR/comments" --paginate \
            -q '.[] | select(.body | startswith("<!-- hadome:review -->")) | .id' | head -n 1)
          case "$CODE" in
            3) body="$RUNNER_TEMP/review.md" ;;
            0) [ -n "$id" ] || exit 0
               body="$RUNNER_TEMP/resolved.md"
               printf '%s\n\n%s\n' "<!-- hadome:review -->" "## hadome: nothing needs a person's review now" > "$body" ;;
            *) exit 0 ;;
          esac
          if [ -n "$id" ]; then
            gh api -X PATCH "repos/$GITHUB_REPOSITORY/issues/comments/$id" -F "body=@$body" > /dev/null
          else
            gh api "repos/$GITHUB_REPOSITORY/issues/$PR/comments" -F "body=@$body" > /dev/null
          fi
      - name: Fail on a regression or a check that could not run
        if: steps.check.outputs.code != '0' && steps.check.outputs.code != '3'
        run: exit ${{ steps.check.outputs.code }}
```

## ミューテーションも試す

`.hadome.yml` に `mutation` を書いたら、`hadome mutate` を `check` とは別のジョブにします。`mutate` はミュータントごとにテストを実行するので、`check` よりずっと長くかかります。別のジョブにすると、`check` の結果を先に見られます。

上の例の `jobs:` に、次のジョブを足します。

```yaml
  mutate:
    runs-on: ubuntu-24.04
    permissions:
      contents: read
    steps:
      - uses: actions/checkout@v5
        with:
          fetch-depth: 0
      - uses: actions/setup-go@v6 # and whatever else the test command needs
        with:
          go-version-file: go.mod
      - name: Try mutations with hadome
        env:
          BASE: origin/${{ github.base_ref }}
        run: |
          report="$RUNNER_TEMP/mutate.json"
          code=0
          go tool hadome mutate --base "$BASE" --format json > "$report" || code=$?
          jq -r '
            def esc: tostring | gsub("%"; "%25") | gsub("\r"; "%0D") | gsub("\n"; "%0A");
            (.regressions[] | "::error file=\(.file),line=\([.line, 1] | max),title=\(.rule)::\((.message + " — " + .fix) | esc)"),
            (.reviews[] | "::warning file=\(.file),line=\([.line, 1] | max),title=\(.rule)::\((.message + " — " + .why) | esc)"),
            (.new_exceptions[] | "::warning file=\(.file),line=\([.line, 1] | max),title=new exception for \(.rule)::\(("reason: " + .reason) | esc)")
          ' "$report" 2>/dev/null || true
          case "$code" in 0|3) exit 0 ;; *) exit "$code" ;; esac
```

- **終了コード**: 扱いは `check` と同じです。`1` と `2` でジョブを落とし、`3` では通します。
- **注釈**: 生き残ったミュータントは、壊した行へのエラーの注釈として出ます。人の確認が要るものは、警告の注釈として出ます。例: 上限を超えて試せなかった分。
- **テストに要るもの**: テストのコマンド（`mutation.test`）が要るものは、ジョブの手順で用意します。例では Go だけを入れています。
- **PR のコメント**: `mutate` の結果は書きません。コメントは `check` のジョブが書く1つだけにして、書き換えがぶつからないようにします。

## 例が前提にしていること

- **履歴**: `fetch-depth: 0` で履歴をすべて取ります。hadome は base のブランチを git から読むためです。
- **コメントの数**: プルリクエストに1つだけ書き、実行のたびに書き換えます。前のコメントは先頭の行の `<!-- hadome:review -->` で探します。
- **フォークからのプルリクエスト**: GitHub が渡すトークンが読み取りだけになるので、コメントの手順は飛ばします。行ごとの警告とジョブの要約は、そのときも出ます。
- **権限**: コメントに要るのは、ジョブの `pull-requests: write` の権限だけです。ラベルやリポジトリの設定は要りません。
- **行ごとの警告**: 確認の要るものの場所を指します。例: `.hadome.yml` の行、依存の図の矢印の行、例外の印の行。その行がプルリクエストの差分に入っていれば、Files changed のその行に出ます。
- **通信**: hadome 自身は GitHub と通信しません。コメントを書くのは CI の手順です。

## push の前のフック

GitHub Actions のような CI を使わないリポジトリでは、push の前のフックで `hadome check` を実行します。

```sh
#!/bin/sh
# .githooks/pre-push; enable it with: git config core.hooksPath .githooks
hadome check --head HEAD
code=$?
if [ "$code" -eq 3 ] && [ "${HADOME_REVIEWED:-}" = "1" ]; then
  exit 0
fi
if [ "$code" -eq 3 ]; then
  echo "hadome: something needs a person's review; once checked, push with HADOME_REVIEWED=1 git push" >&2
fi
exit "$code"
```

| 終了コード | push |
|---|---|
| `0` | 通す |
| `1`・`2` | 止める |
| `3` | 止める。確かめた人が `HADOME_REVIEWED=1 git push` で通す |

- **`3` の扱い**: CI と逆で、push を止めます。CI では、マージする人が判断する画面がプルリクエストにあるので、`3` を通してその画面に出します。push の前のフックにはその画面が無いので、push を止めて、push する本人が確かめます。
- **`--head HEAD`**: 付けます。head の既定は作業ツリーです。付けないと、コミットしていない変更まで検査するので、push する中身とずれます。
- **push するブランチ**: 例は今のブランチ（`HEAD`）を push する前提です。別のブランチを名指しで push すると、検査する中身と push する中身が合いません。
