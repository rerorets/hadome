# CI で hadome を使う

プルリクエストごとに `hadome check` を実行する手順。hadome 自身の CI（[ci.yml](../.github/workflows/ci.yml) の `hadome` のジョブ）も同じ形で動いている。

## 終了コードの扱い

| 終了コード | ジョブ | 出すもの |
|---|---|---|
| `0` | 通す | なし。前に出した確認のコメントがあれば、確認の要るものが無くなったと書き換える |
| `1` | 落とす | 後退の一覧（ログ） |
| `2` | 落とす | 設定の誤り、または検査を実行できなかった理由（ログ） |
| `3` | 通す | 確認の要るものを、行ごとの警告、ジョブの要約、PR のコメントに出す |

`3` は後退ではなく、人が判断するものがある、という知らせである。プルリクエストでは、マージする人がその判断をする。そのため `3` ではジョブを落とさず、マージする人の目に入る場所に出す。落とすと、確認したあとでもブランチの保護を飛ばさないとマージできなくなる。

## GitHub Actions の例

`hadome` の呼び方は入れた方法に合わせて読み替える（README の「入れる」）。`go run` は使わない。`go run` はプログラムの終了コードが `0` 以外なら、どれも `1` にして返すので、`3` と `1` の区別が消える。`go tool hadome`、`npx hadome`、`uv run hadome` は終了コードをそのまま返す。

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

## 例が前提にしていること

- `fetch-depth: 0` で履歴をすべて取る。hadome は base のブランチを git から読む
- コメントはプルリクエストに1つだけ書き、実行のたびに書き換える。印は先頭の行の `<!-- hadome:review -->` で、これを目印に前のコメントを探す
- フォークから来たプルリクエストでは、GitHub が渡すトークンは読み取りだけになる。そのため、コメントの手順は飛ばす。行ごとの警告とジョブの要約は、そのときも出る
- コメントに要るのは、ジョブの `pull-requests: write` の権限だけである。ラベルやリポジトリの設定は要らない
- 行ごとの警告は確認の要るものの場所（`.hadome.yml` の行、依存の図の矢印の行、例外の印の行）を指す。その行がプルリクエストの差分に入っていれば、Files changed のその行に出る
- hadome 自身は GitHub と通信しない。コメントを書くのは、CI の手順である
