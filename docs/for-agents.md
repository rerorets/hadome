# hadome for agents

Exact procedures for agents working in a repository that uses hadome. Each one ends with the command that proves it worked. The short version belongs in the file your instructions come from; `hadome agents` prints it.

## Before finishing any change

```sh
hadome check --format json
```

| Exit code | Meaning | What you do |
|---|---|---|
| `0` | Nothing got worse | Finish |
| `1` | A regression | Fix each entry of `regressions`, then run the check again |
| `2` | The configuration is invalid, or the check could not run | Fix what standard error names (file, line, key, what is allowed, an example), then run the check again |
| `3` | A person must review something: an entry of `reviews`, or of `new_exceptions` | Stop. Tell the person what is listed and why. Do not change code to make it go away |

Each regression carries `rule`, `file`, `line`, `message`, `why` and `fix`. `fix` is the smallest change that resolves it. To learn about the rule itself, run `hadome explain <rule>` (for example `hadome explain tests/no-sleep`). `candidates` lists invariant candidates the change adds; they never change the exit code (see "Invariant candidates" below).

Only regressions fail. A finding that was already on the base branch is counted in `untouched` and never blocks; you do not need to fix it, and fixing it is a separate change.

Proved by: `hadome check` exits `0`.

## Asking a person to review

On `3`, a person judges what was listed. Give them what they need to judge, not the conclusion.

| Write | Example |
|---|---|
| What hadome listed: the rule, the file, the `why` | `config/loosened`: `rules.size.params` goes from 5 to 6 |
| Why you made that change | it reads better than grouping `judge`'s parameters into a type |
| The other way you would take if it is not accepted, and what it costs | group the two versions and their facts into one type; two places change, `judge` and its caller |

- Do not decide for the person, as in "this is fine" or "this was approved"
- Do not go on with the work that depends on it before they answer
- Write at least one other way you can actually take. If you see none, say so

Proved by: on the person's answer, you go on if it is accepted; if not, you make the other change you wrote and `hadome check` runs again.

## Writing an exception

Only when a rule truly does not apply to that line. Never to make a check pass.

```go
time.Sleep(d) // gate:allow tests/no-sleep reason: the OS file watcher offers no event to wait on
```

- The marker applies to its own line, or to the next line when it stands alone
- `reason:` must say why the rule does not apply here, specifically. A marker without a reason has no effect
- Every new marker makes the check return `3`, so a person sees it. Tell the person why you added it

Proved by: `hadome check` lists the marker under `new_exceptions` and no longer lists the finding.

## Adding an invariant

An invariant is a promise the product keeps. Add one only when a person asked for it, or after proposing it and getting a yes.

1. Read the type's meaning: `hadome explain invariants`, then `hadome explain <type>`
2. Add the entry to `.hadome.yml`:

   ```yaml
   invariants:
     - type: idempotent        # a built-in type and its target
       target: api.CreatePost
     - id: billing-1           # product-specific, only when no built-in type fits
       says: "Totals on the screen and in the API match"
   ```

   A target is the unit's last path element and the name, the way code calls it: `api.CreatePost`, `store.Store.Delete`.
3. Run `hadome config check`
4. Make sure a test guards it (next section). A new invariant with no guarding test is a regression

Proved by: `hadome config check` exits `0`, and `hadome check` reports no `tests/invariant-unguarded`.

## Making a test guard an invariant

| Type | What the test must do |
|---|---|
| `idempotent` | Call the target two or more times (a call in a loop counts as two) |
| `round-trip` | Call every target of the pair |
| `isolated` | Call the target with two different users or tenants: two different arguments for the parameter whose name says user, tenant, owner, account, org, team, workspace, customer or project |
| `durable`, `erasable`, `append-only`, `fail-closed` | Call the target, and say `guards: <type> <target>` in the test's doc comment |
| Product-specific (`id`) | Say `guards: <id>` in the test's doc comment |

```go
// guards: durable store.Save
func TestSavedRowsSurviveRestart(t *testing.T) {
```

In every language, write it in the comment right before the test (in Python, the docstring also works). The value after `guards:` is written exactly as the messages name the invariant: `<type> <target>`, `<type> <a>,<b>` for several targets, or the `id`. A skipped test guards nothing.

Calls made through helper functions in test files count. A test that has this shape does not necessarily check the invariant. Write a test that would fail if the invariant were broken.

Proved by: `hadome check` reports no `tests/invariant-unguarded` for it.

## Hitting a size limit

A `size/` regression means a file, function or interface of product code is over its limit, or an existing one got bigger. Split it along a line it already has: a function per step, a file per type, an interface per caller. Do not raise the limit in `.hadome.yml`: that returns `3`, because it loosens the rule for everyone.

Proved by: `hadome check` reports no `size/` regression.

## Comments

- A `TODO` says when or by whom, in the form `.hadome.yml` sets (`hadome explain rules.comments.todo`; by default `TODO(<when or who>): <what>`). `FIXME` and `XXX` are not accepted
- Every exported function, method and type of product code has a doc comment that starts with its name
- A Markdown file named in a comment must exist: write `docs/design/x.md` from the repository root, or `./x.md` from the commented file

Proved by: `hadome check` reports no `comments/` regression.

## Dependencies

When `.hadome.yml` names a diagram (`rules.deps.diagram`), an import between the repository's units must follow an arrow of it. On `deps/not-in-diagram`, change the code to depend on what the diagram allows, usually through an interface the importing unit declares. Do not draw a new arrow to make the check pass: that returns `3`, because it changes the design.

Proved by: `hadome check` reports no `deps/` regression.

## Invariant candidates

`hadome check` lists, under `candidates`, places the change adds that usually carry an invariant: things that create, delete, export and import, or read per user. When one covers behaviour that matters, propose it to the person. Do not add it yourself.

`hadome suggest` prints every candidate in the repository as configuration ready to paste.

## Teaching hadome about the product

Configuration can add entries to hadome's tables, never remove built-in ones. Removing an entry you added earlier returns `3`, because it loosens the rules. The functions that count as assertions (`rules.tests.assertions`) are the other way round: adding one returns `3`.

| To make hadome… | Add to `.hadome.yml` |
|---|---|
| treat the product's own clock wrapper like `time.Now` in tests | `rules.tests.hazards.clock: [example.com/acme/clock.Now]` |
| treat a helper as a sleep | `rules.tests.hazards.sleep: [example.com/acme/testutil.Wait]` |
| recognise a mocking library | `rules.tests.hazards.mock: [example.com/acme/fakes]` |
| treat a unit as performing input or output | `rules.tests.io: [example.com/acme/db]` (`/...` at the end includes everything under it) |

Read the key first: `hadome explain rules.tests.io`.

Proved by: `hadome config check` exits `0`.

## Adding a product-specific gate

A rule only this product has ("never write UPDATE in the log store") is a small command in the repository, in any language. The protocol is in [external-gates.md](external-gates.md).

1. Write the command. It reads one JSON request on standard input, looks only at the version whose root is its working directory, and prints findings (or items) as JSON. Each finding needs `file` and an `anchor` without a line number. It must answer `--self-test` by running its own cases
2. Declare it:

   ```yaml
   gates:
     - id: acme/log-append-only
       kind: finding
       run: [scripts/gates/log-append-only]   # from the repository root
       paths: ["internal/store/log/**"]       # optional
   ```

3. Run `hadome config check`, then `hadome self-test`

A gate that fails, prints anything but the protocol's JSON, or runs past its `timeout` stops the check with exit code `2`. Changing or removing a gate later returns `3`.

Proved by: `hadome self-test` lists the gate's `id`, and `hadome check` runs it.

## Telling other agents

```sh
hadome agents --write AGENTS.md CLAUDE.md   # list the files to put it in; running it again updates the block in place
```

Proved by: running it twice leaves the file unchanged the second time.
