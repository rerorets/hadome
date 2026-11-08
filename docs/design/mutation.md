# Mutation

## Responsibility

Package `internal/mutation` breaks the code a change touched, a little and on purpose, and checks whether the tests notice. Broken code is called a **mutant**. When a test fails, the mutant is **caught**; when the tests pass, it **survived**. When the build that runs the tests does not compile the mutant's file, it is **not built**: no test is run for it, and it is not counted as survived.

Today's `tests/` rules only see what a test calls. A test can call code without checking its result, and then it guards nothing. Mutation asks "if the code breaks, does a test really fail?", so it measures what the tests guard more truly than now.

Does:

- Picks the functions the change touched, and asks the adapters to make mutants of them
- Copies the version's tree to disk, applies the mutants one at a time, and runs the test command from configuration
- Reports as regressions only the mutants that survive in head and that the change added

Does not:

- Guess the test command. Configuration always names it; hadome never calls `go`, `pytest`, `cargo` and the like on its own
- Call existing mutation tools (gremlins, mutmut, Stryker, PIT, cargo-mutants). This keeps the six languages that have mutators (Go, TypeScript, Python, Rust, Java, Kotlin) behaving the same, and keeps hadome from chasing how outside tools are installed and what they print
- Break code the change did not touch. Breaking the whole repository every time would make the time depend on the repository's size, not the change's
- Run inside `hadome check`. It runs the tests many times, so it is a separate command, `hadome mutate`

## The development flow

`hadome check` and `hadome mutate` differ in speed and in how deep they check. So they take different places in the flow.

| | `hadome check` | `hadome mutate` |
|---|---|---|
| Checks | How tests are written, and what they call | Whether breaking the code fails a test |
| Runs code | No | Runs the configured test command |
| Time | Seconds | Minutes: the number of mutants × the test time |
| Agent, locally | Before finishing every change | Once before finishing, when configured |
| CI | Required on every PR | Recommended as required on every PR |

```mermaid
flowchart LR
  change["An agent changes code"] --> check["hadome check (locally, every time)"]
  check --> mutate["hadome mutate (locally, once before finishing)"]
  mutate --> pr["PR"]
  pr --> cicheck["CI: hadome check"] & cimutate["CI: hadome mutate"]
  cicheck & cimutate --> merge["Merge"]
```

### Keeping it in use

hadome cannot change a user's CI settings or branch protection. Whether it is required on PRs is for the repository's owner to decide. Within that, four things keep it in use.

1. Once in, it cannot be taken out without a person's review. A change that removes or weakens the `mutation` setting asks for review as `config/loosened` (exit code `3`), like other settings. An agent cannot drop it for being slow
2. To tell agents, the `hadome agents` block says "when `mutation` is configured, run `hadome mutate` before finishing a change"
3. To show how to add it to CI, [ci.md](../ci.md) has a GitHub Actions example with `check` and `mutate` as separate jobs
4. To make starting easy, `hadome init` writes a `mutation` setting with a test command for the languages it found, as a comment. It never turns a guessed command on

### Steps for agents

`docs/for-agents.md` holds the section below.

````markdown
## Trying mutations

Only when `.hadome.yml` has `mutation`. Run it once before finishing a change, after `hadome check` exits `0`.

```sh
hadome mutate --format json
```

| Exit code | Meaning | What to do |
|---|---|---|
| `0` | Every way of breaking the code the change touched failed a test | Finish |
| `1` | Some way of breaking it goes unnoticed by the tests | Add tests as each entry's `fix` in `regressions` says, and run it again |
| `2` | The tests failed with nothing broken, or the configuration is invalid | Fix the tests first. Do not remove the `mutation` setting |
| `3` | A person must review something (mutants beyond the limit went untried, and the like) | Stop. Tell the person what was listed |

- Do not change the code to make the tests pass. `1` says the tests are not enough
- Write `gate:allow mutation/survived reason: <reason>` only after confirming the replacement does not change behaviour (an equivalent mutant). The reason says why the result stays the same
- Do not remove or weaken the `mutation` setting for being slow. Such a change gives `3`

How to confirm: `hadome mutate` exits `0`.
````

The short version `hadome agents` prints gains one line: "when `mutation` is configured, run `hadome mutate` before finishing a change" (item 2 above).

## Shape

```mermaid
classDiagram
  class Mutant {
    <<facts>>
    +Path string
    +Line int
    +Func string
    +Operator string
    +Original string
    +Replacement string
    +Start int
    +End int
    +Anchor string
  }
  class Mutator {
    <<interface>>
    +Mutants(v engine.Version, funcs []facts.Symbol) ([]facts.Mutant, error)
    +Unmutated(funcs []facts.Symbol) []string
  }
  class Mutate {
    +Analyzer engine.Analyzer
    +Mutator Mutator
    +Run(src engine.Source) (engine.Report, error)
  }
  class Runner {
    +Test []string
    +Sep string
    +Timeout time.Duration
  }
  class Outcome {
    <<enumeration>>
    Caught
    Survived
    Unbuilt
  }
  Mutator ..> Mutant : makes
  Mutate --> Mutator
  Mutate --> Runner
  Runner ..> Outcome : returns
```

- `Mutant` is a language-neutral fact, so it lives in `facts`. That lets the adapters make mutants without depending on `internal/mutation`
- The adapters implement `Mutator`, because only they read syntax trees. `Unmutated` lists the languages that cannot be broken yet; functions touched in them go to a person's review as untried. A mutant is a range of text and what replaces it; the adapters run no code, so their invariants (no code run, no process started) still hold
- `Runner` lives in this package. It copies the version's tree to disk, applies a mutant, runs the test command and restores the file. Like the gates (`internal/gate`), starting processes happens outside the engine
- The report's shape, exception markers and exit codes are the engine's, used as they are. After trying the mutants, `Mutate` hands the results to `engine.Check` as one rule (`mutation/survived`) that returns them per version. The rules of "How to compare" below are exactly the engine's comparison of findings
- `hadome check` does not know `mutation/survived`, so it would report that rule's exception markers as naming an unknown rule or as unused. `engine.Engine.Elsewhere` names the rules another command reports: `check` leaves `mutation/survived` to `mutate`, and `mutate` leaves every other rule to `check`. Markers of those rules are left alone

The dependency diagram gains these arrows.

```mermaid
flowchart TB
  cmd["cmd/hadome"] --> mutation["internal/mutation"]
  mutation --> engine["internal/engine"] & config["internal/config"] & facts["internal/facts"]
```

## Where to break

Only functions that are one of these:

- A function whose code the change changed (new functions included)
- A function called (`Test.Calls`) by a test the change rewrote. This catches a change that weakens only a test and leaves the code alone

## How to break

A few kinds only. Each changes one place, and most tests can notice it.

| Kind | Example |
|---|---|
| Flip a comparison | `<` → `>=`, `==` → `!=` |
| Negate a boolean | `ok` → `!ok`, `true` → `false` |
| Swap arithmetic | `+` → `-`, `*` → `/` |
| Return the zero value | `return total` → `return 0` (each language's zero value) |
| Remove a call statement | remove the line `store.Append(row)` |

A break that gives back the same value is not made a mutant. It changes nothing, so no test can catch it, and it would be reported as a false survivor.

- `return 0` is not broken by returning the zero value
- A return of a constant whose value is the zero value is not broken either. For example, with `ExitOK = 0`, `return engine.ExitOK` is not broken
- In Go, the constant's value is read, from syntax only, from its declaration in the same package or in a package of the same module. Only a literal, `false`, `iota` at the start of its block, and conversions of these are read
- In TypeScript, Python, Rust, Java and Kotlin, only declarations in the same file are read ([adapters-treesitter.md](adapters-treesitter.md), "Mutants")
- A constant whose value cannot be read (one from another module, a value written as an expression, or one declared with different values in two files) is broken as before

Go's zero value comes from the declared result type.

| Result type | Zero value |
|---|---|
| Number types (`int`, `float64`, `byte`, and so on) | `0` |
| `string` | `""` |
| `bool` | `false` |
| Pointers, slices, maps, channels, functions, interfaces (`error` and `any` included) | `nil` |
| A named type (`type Code int`, and so on) | The zero value of its underlying type. When that is a struct, the type as written followed by `{}`, such as `status.Code{}` |

- The underlying type of a named type is read, from syntax only, from its declaration in the same package or in a package of the same module. For example, when package `status` has `type Code int`, `return code` in `func Parse(s string) status.Code` becomes `return 0`
- These functions get no mutant of this kind:
  - functions with more than one result
  - functions whose result type is a fixed-size array, a struct type itself (`struct{ … }`), a type parameter, or a type with type arguments (`Set[int]`)
  - functions returning a type whose underlying type cannot be followed (a type of another module, or one declared in two files)

A mutant's anchor is the function's name, the kind, and the normalised original line, with no line number. When a change moves the function, the mutant still matches.

After each mutant it tries, it writes one progress line to standard error: which mutant it is, the file and line, the function, the kind of break, and whether a test noticed. The language is the report's: `--lang`, else `messages` in head's configuration. Example: `mutate 1/2 calc/calc.go:5 calc.Sign, flip a comparison: caught`

## Files outside the build

Breaking a file that the build running the tests does not compile makes the tests run none of its code. The tests pass as a matter of course, and whether the code is guarded is unknown. For example, a file with `//go:build !cgo` is not compiled by `go test` with cgo on. Reporting its mutants as survivors would make regressions that no added test could remove.

So the mutants of such a file are set apart as **not built**.

- **No test is run for them.** Their progress line says `not built`
- **They are counted neither as survived nor as caught.** They are never a regression (exit code `1`)
- **They go to a person's review (exit code `3`).** One review of `mutation/survived` per file names the file and how many mutants were not tried. Untried mutants are not passed silently (invariant 5)
- **They do not count toward `max-mutants`.** The limit is on the mutants whose tests are really run

### What Go decides

Whether a Go file is in the build is decided from syntax only. No command such as `go list` is run.

| What is read | Example |
|---|---|
| The `//go:build` line | `//go:build !cgo`, `//go:build ignore`, `//go:build linux && amd64` |
| `_GOOS` and `_GOARCH` in the file name | `poll_windows.go`, `asm_arm64_test.go` |
| A file name starting with `_` or `.` | `_scratch.go` |

These are the rules of `MatchFile` in `go/build`. The environment matched against is the one hadome runs in.

- GOOS, GOARCH and cgo follow `go/build`'s default (`go/build.Default`). cgo follows `CGO_ENABLED`, and without it, whether a C compiler is found
- When `mutation.test` has an element `CGO_ENABLED=0`, `GOOS=…` or `GOARCH=…`, that is used. For example, `[env, CGO_ENABLED=0, go, test, ./...]`
- When `mutation.test` has `-tags`, those tags are taken as satisfied. For example, `[go, test, -tags, integration, ./...]` or `-tags=integration`

## How to compare

Like the other rules, only what the change added is a regression.

1. In head, run every mutant in the places to break. A caught one is done. When the configuration has `{tests}`, narrow the tests run (see "Narrowing the tests run" below)
2. For each surviving mutant, look for a mutant with the same anchor in base
3. If base has none (a new function, a new line or a rewritten line), it is a regression: adding code means adding a test that guards it
4. If base has one, run it in base too. If base caught it, it is a regression (the tests got weaker). If it survived in base as well, it is an old gap and is not reported

Base runs only what survived in head, so most of the time is one run of head.

### Narrowing the tests run

The number of mutants depends on the size of the change, not on the number of tests. But trying each mutant runs the whole test command, so in a repository with many tests each run is long. For example, if the whole suite takes 5 minutes, 15 mutants take 75 minutes.

So hadome can run only the tests that call the broken function. Which test calls which function is known from the `Test.Calls` the adapters read.

1. Configuration writes `{tests}` into `test` (for example `[go, test, ./..., -run, "{tests}"]`). How test names are passed differs between languages, so hadome does not guess it
2. For each mutant, the names of the tests that call the broken function go into `{tests}`. The names go in as they are, joined by `tests-separator` (default `|`)
3. A mutant that survives the narrowed run is run again with `{tests}` taken out (all the tests). Only one that survives that too counts as survived
4. A mutant with no test found calling the broken function runs with all the tests from the start

Step 3 is there because `Test.Calls` can miss calls. If a test that calls the function indirectly is left out, the run with all the tests catches it, so no false survivor reaches the report. Only survivors run with all the tests, so most of the time is in the narrowed runs. With no `{tests}` in the configuration, every run uses all the tests.

The command with `{tests}` taken out drops the element holding `{tests}`, and the element before it when that one is a flag (starts with `-`). In the example above it becomes `[go, test, ./...]`.

A regression is reported as `mutation/survived`.

```json
{
  "rule": "mutation/survived",
  "file": "store/store.go",
  "line": 21,
  "message": "store.Append: changing `<` to `>=` does not fail any test",
  "why": "the code can do the opposite of what it does now and the tests still pass",
  "fix": "add a test that checks the result of this comparison"
}
```

A replacement may not change behaviour at all (an **equivalent mutant**), such as a loop where `i < n` and `i != n` give the same result. Such a mutant gets a reasoned exception marker (`gate:allow mutation/survived reason: <reason>`).

## Configuration

```yaml
mutation:
  test: [go, test, ./...]   # required; run in the root of the version's copy for each mutant
  timeout: 2m               # optional; the limit of one test run. Default: three times one run before breaking anything
  max-mutants: 100          # optional; how many mutants one run tries at most. Default: 100
```

The default `max-mutants` of 100 is about 17 minutes at most with a 10-second test run.

An example that narrows the tests:

```yaml
mutation:
  test: [go, test, ./..., -run, "^({tests})$"]   # {tests} gets the names of the tests that call the broken function
  tests-separator: "|"                          # optional; what joins the names. Default: "|"
```

- Before breaking anything, the test command runs once in head. If it fails, nothing is broken and the exit code is `2`: with tests that already fail, whether a mutant was caught cannot be told
- A timeout counts as caught: a mutant that made the tests stop finishing did not let them pass
- Removing `mutation`, lowering `max-mutants` or changing `test` loosens the rules and asks for a person's review. Adding or taking out `{tests}` is changing `test`. So is changing `timeout` or `tests-separator`. `max-mutants` uses the direction where a smaller number is looser (`LooserWhenLower`)
- Adding `mutation` when base has none does not loosen anything, because base did not run `mutate` at all (`OmittedIsOff`)

## Invariants

1. When the tests fail on a tree with nothing broken, `hadome mutate` gives no result and returns exit code `2`
2. The repository's working tree is never changed. Only a version copied to a temporary directory is broken
3. The same pair of versions and configuration always make the same mutants in the same order
4. A mutant that already survived in base never fails a change
5. Mutants that could not be tried (beyond the limit, in a file outside the build, and functions touched in a language without a mutator) are reported, never passed silently, with exit code `3` (a person's review). A mutant not tried can be called neither caught nor survived
6. A mutant that survived only a narrowed run is never reported. Only one that also survives all the tests is

## Deliberately left out

- Running mutants in parallel. One at a time keeps tests from flaking over what the test command shares (ports, temporary files)
- Caching results. A different pair of versions gives different results, so results can rarely be reused
- A mode that breaks the whole repository. Its time would depend on the repository's size, not the change's
- Reusing each language's builds (why existing tools are fast). Making six languages behave the same comes first
- Running the tests with instrumentation to measure which lines each test passes through (how PIT and Stryker narrow). It needs to get inside each language's test runner, which goes against not calling outside tools. `Test.Calls`, read without running code, narrows instead
- Telling which files are outside the build in languages other than Go. TypeScript's `exclude` in `tsconfig.json`, Rust's `#[cfg(...)]` and Python's branches at run time differ widely by language. Only Go is told apart today; mutants of other languages run their tests as before
- Asking `go list` which files are in the build. Starting a command depends on module downloads and caches. What syntax cannot tell (a `mutation.test` that calls a shell script that changes the environment, and the like) is not told apart
- Trying files outside the build in another environment (running again with `CGO_ENABLED=0`, and the like). The test command in configuration decides the environment the tests run in; hadome does not guess it
- Agent hooks (settings that run `hadome mutate` when an agent finishes its work). They would depend on a particular agent product. Agents are told through the `hadome agents` block, and CI makes it required
