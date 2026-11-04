# CLAUDE.md

hadome (歯止め) is a ratcheting checker: it compares a change against the main branch and fails only on regressions — new violations, and anything that shrinks what the tests guard. Start with `README.md`.

## Hard rules

1. **This is a public repository. Bring in no knowledge from any employer or private project.** No code, names, domains, paths, table names, or examples taken from private codebases. Use generic examples only (`store.Append`, `worker/queue`, "product A"). Ideas that are common knowledge are fine; specifics are not.
2. **Design before code.** Every package starts as a design document in `docs/design/` (responsibility, what it does not do, a Mermaid diagram of types and interfaces, invariants, what is deliberately left out). Stop there for the owner's review before writing code.
3. **Fail on regressions, not on history.** Every rule compares against the base branch and reports only new violations and regressions.
4. **Exceptions carry reasons.** An exception marker without a written reason has no effect. Markers in users' source code must not contain the tool's name (use the neutral `gate:` prefix) so that renaming the tool never forces users to rewrite their code.
5. **Every rule ships with a self-test.**
6. **No network, no telemetry.** hadome only reads the repository it is pointed at.

## Shape

| Layer | What it is | Where |
|---|---|---|
| Engine | Base comparison, violations, reasoned exceptions, self-test harness, output | this repository |
| Language adapters | Finding tests, reading imports, measuring size, reading docblocks — for Go, TypeScript, Python, Rust, Java, Kotlin | this repository |
| Generic rules | `tests/`, `deps/`, `size/`, `comments/` | this repository, tuned by configuration |
| Product configuration | Thresholds, dependency diagram, invariant list, enabled rules | each user's repository |
| Product-specific gates | Anything a single product needs | each user's repository, using the same interface |

Distributed as a single Go binary, through six ecosystems (`packaging/`, [docs/maintaining/releasing.md](docs/maintaining/releasing.md)).

hadome checks itself: [.hadome.yml](.hadome.yml) lists its invariants, and [docs/design/dependencies.md](docs/design/dependencies.md) is the diagram its imports must follow. A new package or import starts there.

## Writing

- Code, comments and commit messages are in English.
- Every document is bilingual: `foo.md` in English and `foo.ja.md` in Japanese next to it. **The Japanese file is the source of truth.** Write or change the Japanese first, then bring the English up to it in the same change. Links in a Japanese document point to the Japanese documents. The two files must have the same headings and code blocks (`cmd/hadome/docs_test.go` fails otherwise).
- Comments say what the code cannot: why an unusual approach was taken, invariants, and facts a reader would otherwise get wrong. No history, no progress notes, no ticket numbers.
- Tests follow the model in [docs/model.md](docs/model.md): invariants, writing rules, gate. Test observable behavior only, never copy expected values from the implementation, never read the clock or sleep, never skip silently.

## Local setup

Building, testing and enabling the hooks are in [CONTRIBUTING.md](CONTRIBUTING.md). The private denylist `.githooks/pre-commit` reads (`$HADOME_DENYLIST` or `~/.config/hadome-dev/denylist.txt`) stays outside the repository; never commit it.

<!-- hadome:begin -->
## hadome

This repository uses hadome to stop regressions in code and tests.

- Before you finish any change, run `hadome check --format json` and fix every regression it reports.
- When `.hadome.yml` has `mutation`, also run `hadome mutate --format json` before you finish, after `check` exits `0`, and add a test for every mutant it reports. Its exit codes are `check`'s.
- Exit codes: `0` done · `1` regression, fix it · `2` configuration error, fix the configuration · `3` a person must review (a new exception, a loosened configuration, code that stopped being pure, or something hadome could not identify). On `3`, stop and ask. Never change the code just to make it pass.
- When you ask about `3`, give what hadome listed (the rule and its `why`), why you made that change, and the other way you would take if it is not accepted, with what that costs. Leave the decision to the person; do not tell them it is fine.
- Never add `gate:allow` to get a check to pass. Add one only when the rule truly does not apply, with a specific reason, and tell the person.
- When a test guards a product-specific invariant, add `guards: <id>` to its doc comment; for `durable`, `erasable`, `append-only` and `fail-closed`, add `guards: <type> <target>`. `idempotent`, `round-trip` and `isolated` are recognised from the test's calls. `hadome explain invariants` lists them.
- When `hadome check` lists invariant candidates and one covers behaviour that matters, propose it to the person instead of adding it yourself.
- Before changing `.hadome.yml`, run `hadome explain <key>`. After changing it, run `hadome config check`.
<!-- hadome:end -->
