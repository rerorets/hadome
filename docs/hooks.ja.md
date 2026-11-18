# エージェントのフックで hadome を使う

エージェントが作業を終えようとするたびに、`hadome check` を自動で実行する手順です。`hadome agents --write` で書いた指示だけでは、エージェントがチェックを実行しないまま終えることがあります。Claude Code と Codex の Stop フックに入れると、後退が残っているあいだは終えずに直し続けます。

## 終了コードの扱い

| `hadome check` の終了コード | フック | エージェントがすること |
|---|---|---|
| `0` | 終えさせる | 終える |
| `1` | 止めて、出力を理由として渡す | 後退を直し、もう1度終えようとする |
| `2` | 止めて、出力を理由として渡す | 設定を直し、もう1度終えようとする |
| `3` | 1回だけ止めて、人に聞くように伝える | 挙がったものを人に伝えて終える |

`3` で止めるのは1回だけです。

- `3` は後退ではなく、人が判断するものがある、という知らせです。直し続けさせると、消すためにコードを変えることになります。
- フックに続けさせられたあとの Stop では、入力の `stop_hook_active` が `true` になります。そのときは終えさせます。
- `3` は人が判断するまで残ります。人が次の指示を出したあとでエージェントが終えようとすると、また1回止めます。

## スクリプト

Claude Code と Codex で同じスクリプトを使います。リポジトリに `.claude/hooks/hadome-stop.sh` として置き、実行できるようにします。

```sh
#!/bin/sh
# Runs hadome check when the agent tries to finish. Exit 2 keeps the agent
# working and hands it stderr as the reason; exit 0 lets it finish.
input=$(cat)
cd "$(git rev-parse --show-toplevel)" || exit 0

out=$(hadome check 2>&1)
code=$?

case $code in
  0) exit 0 ;;
  1|2)
    printf '%s\n\nhadome check exited %s. Fix what it lists, run it again, then finish.\n' "$out" "$code" >&2
    exit 2 ;;
  3)
    # Ask once: after the hook has continued the turn, let the agent stop and wait for the person.
    case $input in *'"stop_hook_active":true'* | *'"stop_hook_active": true'*) exit 0 ;; esac
    printf '%s\n\nhadome check exited 3: a person must review the items above. Do not change the code to make them go away. Stop and ask the person, giving each item, why you made the change, and the other way you would take.\n' "$out" >&2
    exit 2 ;;
  *)
    printf 'hadome could not run (exit %s):\n%s\n' "$code" "$out" >&2
    exit 1 ;;
esac
```

```sh
chmod +x .claude/hooks/hadome-stop.sh
```

`hadome` の呼び方は入れた方法に合わせて読み替えます（例: `npx hadome`、`go tool hadome`、`uv run hadome`）。`go run` は使いません。終了コードを `1` にまとめるので、`3` と `1` の区別が消えます。

## Claude Code の設定

`.claude/settings.json` に足します。Stop フックに `matcher` は要りません。

```json
{
  "hooks": {
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "\"$CLAUDE_PROJECT_DIR\"/.claude/hooks/hadome-stop.sh",
            "timeout": 120
          }
        ]
      }
    ]
  }
}
```

- フックが `2` で終わると、Claude は標準エラーの中身を理由として受け取り、作業を続けます。
- Claude Code は、Stop フックが続けて8回止めると、次は止めずに終えます。Claude がツールを呼ぶたびに数え直します。
- 人が中断（Ctrl+C）したときは、Stop フックは動きません。

## Codex の設定

`.codex/hooks.json` に足します。Codex にはプロジェクトのルートを指す変数が無いので、git のルートからパスを作ります。

```json
{
  "hooks": {
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "\"$(git rev-parse --show-toplevel)/.claude/hooks/hadome-stop.sh\"",
            "timeout": 120
          }
        ]
      }
    ]
  }
}
```

- 初めて使う前と、フックを変えたあとは、Codex の `/hooks` でフックを信頼します。信頼していないフックは動きません。
- Codex の文書には、Stop フックが続けて止める回数の上限が書かれていません。後退を直せないまま回り続けたら、人が止めます。

## 確かめ方

ある関数の唯一のテストを消した状態で、スクリプトを手で実行します。

```sh
echo '{"stop_hook_active": false}' | .claude/hooks/hadome-stop.sh; echo "exit $?"
```

`tests/scope-shrunk` の一覧が出て `exit 2` で終わり、テストを戻すと何も出ずに `exit 0` で終わる。
