# Rules: `tests/`

## Purpose

The `tests/` rules keep the test suite trustworthy as agents change it. They check two things:

- **How tests are written** — the habits that make a test pass without proving anything
- **What the tests guard** — so that a change can never quietly shrink it

They work on the facts from the language adapters ([adapters.md](adapters.md)) and never know a language.

## The rules

| Rule | Kind | Regression when |
|---|---|---|
| `tests/no-clock` | finding | A test reads the clock directly |
| `tests/no-sleep` | finding | A test waits by sleeping |
| `tests/skip-without-reason` | finding | A test is skipped or disabled without a reason |
| `tests/always-true` | finding | An assertion cannot fail |
| `tests/no-mock` | finding | A test of a *pure* unit uses a mock |
| `tests/purity-lost` | finding (review) | A unit that was pure in base is no longer pure in head — returns exit code `3`, never `1` |
| `tests/scope-shrunk` | measure | Something guarded in base is guarded by no test in head |
| `tests/target-unknown` | finding (review) | A new active test whose target could not be identified at all — returns exit code `3`, never `1` |
| `tests/invariant-unguarded` | finding | An enabled invariant has no active guarding test |

All are on by default and need no configuration.

## How tests are written

These four rules work on the adapters' hazard categories. Each finding's anchor is the test's name plus the normalised offending line, so moving the test does not make it new.

### `tests/no-clock`

- **Finds**: a `clock` hazard inside a test
- **Why**: the test's result depends on when it runs; it passes today and fails at midnight, at month end, or in another time zone
- **Fix**: pass the time in as a value, or inject a clock
- Products add their own clock wrappers to the hazard table, so bypassing them is caught too

### `tests/no-sleep`

- **Finds**: a `sleep` hazard inside a test
- **Why**: a sleep is either too long (slow suite) or too short (flaky test); it never waits for the actual event
- **Fix**: wait for the event itself (a channel, a callback, a condition with a deadline that fails loudly)

### `tests/skip-without-reason`

- **Finds**: a skip whose reason is missing or empty
- **Why**: a skipped test protects nothing, and without a reason nobody can decide when it should come back
- **Fix**: write the reason in the skip itself: `t.Skip("…")`, `pytest.skip(reason="…")`, `@Disabled("…")`, `#[ignore = "…"]`, or a comment on or above `it.skip`
- Skips that happen only in some cases (`if runtime.GOOS == "windows"`) need a reason too
- A skip **with** a reason is not a regression here, but the test stops counting towards the guarded scope (below)

### `tests/always-true`

- **Finds**: an `always-true` hazard — an assertion on a constant, or a value compared with itself
- **Why**: the assertion cannot fail, so the test reports success no matter what the code does
- **Fix**: assert on the behaviour under test

### `tests/no-mock`

- **Finds**: a `mock` hazard in a test whose targets are all in *pure* units
- **Why**: a pure unit has no side effects; needing a mock means side effects have leaked into it, and the mock hides that
- **Fix**: move the side effect out of the unit, and test the unit with plain values
- Which units are pure is decided by hadome on every check (below). Nothing to configure
- A test that also calls a unit that is not pure (or whose purity is `unknown`) may need the mock for that one, so it is not reported

### Pure units

A **pure unit** takes inputs and returns results without touching the outside world: no database, network, files, clock or environment. Pricing, validation and date-range arithmetic are typical. Code that is not pure is best tested with real dependencies (integration tests); pure code is best tested with plain values.

hadome decides purity from imports, on every check:

- A unit is **pure** when it imports nothing that performs input or output, and depends on no unit that is not pure
- What performs input or output is an `io` table in each adapter, like the hazard tables (Go: `net`, `os`, `database/sql`, …; Python: `socket`, `requests`, `sqlite3`, …). Configuration can add entries, never remove them. An entry is a unit (`example.com/acme/db`), or a unit followed by `/...` for it and everything under it (`example.com/acme/clients/...`). An entry may name one of the repository's own units, such as a product's database layer: everything that imports it is then not pure
- A unit whose imports cannot all be identified is `unknown`, and `unknown` is never pure
- Purity follows the repository's own imports transitively: a unit that imports a unit that is not pure is not pure either
- In Go the `time` package is not in the table, because it also holds plain values (`time.Duration`); reading the clock is caught in tests by `tests/no-clock`, not by purity

### `tests/purity-lost`

- **Items**: the pure units of a version, a measure rule whose items need review when they disappear
- **Finds**: a unit that was pure in base and is not pure in head
- **Why**: side effects have entered code that used to be pure. Sometimes that is intended, often it is a design slip
- **Treatment**: returns exit code `3` (a person reviews it), never `1`
- **Legitimate removal**: the unit no longer exists in head
- The `io` table comes from head's configuration for both versions, so adding an entry never asks for a review of units that were already importing it

## What the tests guard

### The guarded scope

The guarded scope is the set of **targets** that at least one active test exercises. A target is a public function or method in non-test code that a test calls, as inferred by the adapter.

A test counts only when it is active: skipped tests (with or without a reason) do not guard anything.

### `tests/scope-shrunk`

- **Items**: one per guarded target, keyed by the target's unit and name
- **Regression**: a target guarded in base is not guarded in head
- **Legitimate removal**: the target itself no longer exists in head (the code was removed or renamed). hadome verifies this; nobody needs to declare it
- **Cleanup passes**: merging duplicate tests, deleting tests that guard nothing new, moving or renaming tests — the set of guarded targets is unchanged
- **Unidentified**: a test whose target cannot be identified at all is reported by `tests/target-unknown` and returns exit code `3`. Tests that reach the code through helper functions in test files are followed into the helpers, so helpers do not make a target unidentifiable

When mutation analysis arrives, the items become "breakages the tests catch", and this rule gets sharper without changing its name or its meaning.

### Which tests guard an invariant

People and agents rarely need to say which test guards which invariant. hadome finds them.

| Invariant | How its guarding tests are found |
|---|---|
| A built-in type | **Automatically, by shape.** A test guards the invariant when its calls fit the type (table below). No declaration |
| A product-specific invariant (`id` and `says`) | **By a declaration.** The test's doc comment carries `guards: <id>`. Shape cannot reveal what a sentence means, so this is the only place a declaration is needed |

| Built-in type | A test guards it when, for every target, it |
|---|---|
| `idempotent` | Calls the target two or more times |
| `round-trip` | Calls the target (so a test calling both operations of the pair guards it) |
| `isolated` | Calls the target with at least two distinct scopes (below) |
| `durable`, `erasable`, `append-only`, `fail-closed` | Calls the target, and carries `guards: <type> <target>` (no reliable shape from syntax alone yet, so these still need a declaration) |

Only active tests count: a test that is always skipped guards nothing; one that skips only in some cases still guards.

The adapter reports, for each target a test calls, **how many times** and **with how many distinct scopes**:

- **Times** counts call sites, including those in helpers the test calls. A call site inside a loop counts twice, because it runs more than once (table-driven tests)
- The **scope** of a call is the argument passed to the target's scope parameter: the first parameter whose name contains `user`, `tenant`, `owner`, `account`, `org`, `team`, `workspace`, `customer` or `project` (case-insensitive), or else the first parameter that is not a request context. Distinct scopes are counted by the normalised source text of that argument

A configured target names a symbol by its unit and name, the way code calls it (`store.Append`, `store.Store.Len`). It matches every symbol whose ID ends with it after a path separator, so `api.CreatePost` matches `example.com/app/api.CreatePost`. A target that matches no symbol is reported as unguarded, and the message says so.

A declaration is one line of the test's doc comment, `guards: <invariant>`, where `<invariant>` is written the way `hadome explain` and the messages name it: the `id` of a product-specific invariant, or `<type> <target>` (`<type> <a>,<b>` for several targets).

A test that only calls the target twice by coincidence is counted as a guard. That is the price of needing no declaration; mutation analysis later replaces shape with "does the test actually fail when the invariant is broken".

### `tests/invariant-unguarded`

- **Finds**: an invariant enabled in configuration with no active test that guards it
- **Why**: the invariant is written down but nothing enforces it
- **Fix**: write a test that checks the invariant. For built-in types with a shape, that is all; for the others and for product-specific invariants, add `guards:` to its doc comment
- The anchor is the invariant, so the finding is matched across versions however tests move
- Because it is compared with base, enabling a new invariant without a test is a regression; an invariant that was already unguarded in base does not block
- **Losing a guard is the same finding.** When the last guarding test is deleted, skipped, stops fitting the shape or loses its `guards:` line, the invariant becomes unguarded in head while it was guarded in base: a new finding, so a regression. Moving the guard to another test changes nothing, because the invariant stays guarded
- **Removing the invariant itself** from configuration is not this rule's business: it is `config/loosened` and returns exit code `3`, because dropping a promise is a decision for a person

A separate measure rule over (invariant, guarding test) pairs would fire in exactly the same situations, so there is none.

## Invariant candidates

`hadome check` lists the invariant candidates a change adds — places that usually carry an invariant (operations that create things → `idempotent`, export/import pairs → `round-trip`, deletes → `erasable`, reads scoped by user or tenant → `isolated`). They are information only: they never change the exit code. `hadome suggest` prints all of them. How candidates are found is in [suggest.md](suggest.md).

The agent instruction block tells agents to propose a candidate to a person when it covers behaviour that matters, so candidates reach people without anyone running a command.

## Messages

Every finding carries `why` and `fix` written for an agent: what went wrong, why it matters, and the smallest change that resolves it. The text above is the starting point for those messages.

## Self-test cases

Each rule ships with at least these cases.

| Rule | Cases |
|---|---|
| `tests/no-clock` | a new clock read fails · an existing one moved within the file passes · a clock read inside a string passes |
| `tests/no-sleep` | a new sleep fails · a sleep in non-test code is not this rule's business |
| `tests/skip-without-reason` | a new bare skip fails · a skip with a reason passes · an empty reason fails |
| `tests/always-true` | `assertTrue(true)` fails · comparing a value with itself fails · comparing two different values passes |
| `tests/no-mock` | a mock in a test of a pure unit fails · the same mock in a test of a non-pure unit passes · a unit made impure by a configured `io` entry passes · a unit importing a non-pure unit of the repository passes |
| `tests/purity-lost` | a pure unit gaining a database import returns `3` · a unit that was never pure does not · a pure unit becoming impure through a unit it imports returns `3` · deleting a pure unit passes |
| `tests/scope-shrunk` | deleting the only test of a target fails · merging two tests of one target passes · deleting a target and its test passes · renaming a test passes |
| `tests/invariant-unguarded` | a new invariant without a test fails · an old unguarded invariant does not block · deleting the only guard fails · moving the guard to another test passes · skipping the only guard fails · removing the invariant returns `3` (`config/loosened`) |
| `tests/invariant-unguarded` (shape) | an `idempotent` invariant whose only test calls the target once is unguarded · a test calling it twice guards it · `round-trip` needs both operations · `isolated` needs two scopes · a product-specific invariant is guarded only by `guards:` |

## Invariants

1. Moving, renaming or merging tests without changing what they guard never fails these rules
2. A test that is always skipped never counts as guarding anything. A test that skips itself only in some cases (on one platform, under a flag) still guards; its skip still needs a reason
3. No rule fails a change because of something already present in base
4. Anything these rules cannot identify is reported, never silently passed

## Deliberately left out

- Checking that a test is *correct*. Only mutation analysis gets close, and it comes later
- `tests/no-network` (reserved): calling real external services from tests. Added when the adapters can identify such calls
- Test speed (belongs to a separate rule group with its own design)

## Decisions

1. Purity is decided by hadome from imports on every check; `tests/no-mock` needs no configuration. A unit that stops being pure returns exit code `3`. Invariant candidates appear in `hadome check` output as information
2. Guarding tests of built-in invariant types with a shape are found automatically; `guards:` is needed only for product-specific invariants and for types without a shape. There is no separate `tests/guard-shape` rule
3. `tests/no-network` is reserved: tests must not talk to real external services. It is added when adapters can identify such calls
4. Removing an invariant from configuration returns exit code `3`, once, in the change that removes it. After that change reaches the base branch, the invariant is simply absent and later changes return `0`. The same holds for every exit-code-`3` situation
