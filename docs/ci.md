# Using hadome in CI

How to run `hadome check` on every pull request. hadome's own CI (the `hadome` job of [ci.yml](../.github/workflows/ci.yml)) runs the same way.

## What each exit code does

| Exit code | Job | What it shows |
|---|---|---|
| `0` | passes | Nothing. A review comment from an earlier run is rewritten to say nothing needs review now |
| `1` | fails | The regressions (in the log) |
| `2` | fails | The configuration error, or why the check could not run (in the log) |
| `3` | passes | What needs review, as a warning on each line, in the job summary, and in a pull request comment |

`3` is not a regression: it says something is for a person to decide. On a pull request, the person who merges it decides. So `3` does not fail the job, and what needs review is put where that person sees it. Failing the job would leave a reviewed pull request mergeable only by bypassing branch protection.

## A GitHub Actions example

Call `hadome` the way you installed it (README, "Install"). Do not use `go run`: it returns `1` for any non-zero exit of the program, which loses the difference between `3` and `1`. `go tool hadome`, `npx hadome` and `uv run hadome` return the exit code as it is.

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

## Trying mutations too

Once `.hadome.yml` has `mutation`, run `hadome mutate` as a job of its own, apart from `check`. `mutate` runs the tests once per mutant, so it takes far longer than `check`, and a separate job shows `check`'s result first. Add this job under `jobs:` in the example above.

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

- Exit codes are handled as for `check`: `1` and `2` fail the job, and `3` passes it
- A surviving mutant shows as an error annotation on the line it broke. What needs a person's review (mutants beyond the limit, and the like) shows as a warning annotation
- Whatever the test command (`mutation.test`) needs is set up by the job's steps. The example installs only Go
- `mutate`'s results are not written to the PR comment. The one comment is the `check` job's, so that no two jobs rewrite it

## What the example assumes

- `fetch-depth: 0` fetches the whole history: hadome reads the base branch from git
- There is one comment per pull request, rewritten on every run. Its first line, `<!-- hadome:review -->`, is how the earlier comment is found
- A pull request from a fork gets a read-only token from GitHub, so the comment step is skipped. The line warnings and the job summary are still shown
- The comment needs only the job's `pull-requests: write` permission: no label and no repository setting
- A line warning points at where the item is (the line in `.hadome.yml`, the arrow in the dependency diagram, the exception marker). When that line is in the pull request's diff, it shows on that line in Files changed
- hadome itself never talks to GitHub. Writing the comment is a step of the CI
