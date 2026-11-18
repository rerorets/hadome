# Running hadome from an agent's hook

These steps run `hadome check` automatically every time an agent tries to finish its work. The instructions `hadome agents --write` writes are not always enough: an agent can finish without running the check. In a Claude Code or Codex Stop hook, the agent keeps working and does not finish while a regression remains.

## Exit codes

| Exit code of `hadome check` | The hook | What the agent does |
|---|---|---|
| `0` | lets it finish | finishes |
| `1` | stops it and hands it the output as the reason | fixes the regressions and tries to finish again |
| `2` | stops it and hands it the output as the reason | fixes the configuration and tries to finish again |
| `3` | stops it once and tells it to ask the person | tells the person what was listed and finishes |

On `3` the hook stops the agent only once.

- `3` is not a regression; it says there is something for a person to decide. Making the agent keep working would have it change the code to make the items go away.
- In a Stop that follows a continuation by the hook, `stop_hook_active` in the input is `true`. Then the hook lets the agent finish.
- `3` stays until the person decides. When the agent tries to finish after the person's next message, the hook stops it once again.

## The script

Claude Code and Codex use the same script. Put it in the repository as `.claude/hooks/hadome-stop.sh` and make it executable.

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

Call `hadome` the way you installed it (for example `npx hadome`, `go tool hadome`, `uv run hadome`). Do not use `go run`: it turns every non-zero exit code into `1`, so `3` and `1` can no longer be told apart.

## Claude Code

Add this to `.claude/settings.json`. A Stop hook needs no `matcher`.

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

- When the hook exits `2`, Claude receives its standard error as the reason and keeps working.
- After Stop hooks have stopped Claude eight times in a row, Claude Code lets the next stop through. The count starts again each time Claude calls a tool.
- When a person interrupts (Ctrl+C), the Stop hook does not run.

## Codex

Add this to `.codex/hooks.json`. Codex has no variable for the project root, so the path is built from the git root.

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

- Before the first use, and after changing the hook, trust it with `/hooks` in Codex. A hook that is not trusted does not run.
- Codex's documentation states no limit on how many times in a row a Stop hook may stop it. If it keeps going without being able to fix a regression, a person stops it.

## Checking it

Delete the only test of a function and run the script by hand.

```sh
echo '{"stop_hook_active": false}' | .claude/hooks/hadome-stop.sh; echo "exit $?"
```

It lists `tests/scope-shrunk` and ends with `exit 2`; with the test restored it prints nothing and ends with `exit 0`.
