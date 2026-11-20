# Product-specific gates

## Responsibility

Package `internal/gate` turns the `gates` of a configuration into rules the engine runs like any built-in rule. Each gate is an external command in the product's repository; the protocol it speaks is in [external-gates.md](../external-gates.md).

It does:

- Build one rule per configured gate, with the gate's `id` as the rule ID, so markers, matching across versions and the report work unchanged
- Run the command once per version, with the version's tree on disk as the working directory, and read its JSON answer
- For measure gates, ask the command which missing items were removed legitimately, once per check
- Run the command's `--self-test` before `hadome check` uses the gate, and for `hadome self-test`
- In `hadome check`, run only the gates base declares with the same command ([Which gates may run](#which-gates-may-run))

It does not:

- Compare versions. The engine does, exactly as for built-in rules
- Interpret findings. Whatever the command reports is the gate's verdict
- Fetch anything. Commands are files in the repository or programs on `PATH`

## Shape

```mermaid
classDiagram
  class Engine {
    +ConfigRules func(c config.Config) ([]Rule, error)
  }
  class Version {
    +Label string
    +FS fs.FS
    +Root string
  }
  class Input {
    +Facts *facts.Analysis
    +Config config.Config
    +Version Version
  }
  class SelfTester {
    <<interface>>
    +SelfTest() error
  }
  class findingGate {
    +ID() string
    +Find(in Input) ([]Finding, error)
    +SelfTest() error
  }
  class measureGate {
    +ID() string
    +Measure(in Input) ([]Item, error)
    +RemovedLegitimately(it Item, head Input) (bool, error)
    +SelfTest() error
  }
  class gate {
    <<package>>
    +Rules(c config.Config, workdir string, diag io.Writer) ([]engine.Rule, io.Closer, error)
  }
  Engine ..> gate : cmd passes gate.Rules
  findingGate ..|> SelfTester
  measureGate ..|> SelfTester
  gate ..> findingGate
  gate ..> measureGate
  Input --> Version
```

- The engine calls `ConfigRules` once per check with head's configuration, passing only the head gates that base declares with the same command, and runs the rules it returns on both versions next to the built-in ones. A gate's settings (`paths`, `options`, `timeout`) come from head like every other setting ([configuration.md](configuration.md))
- `Input` carries the `Version`, so a rule that needs files on disk can reach them. Built-in rules never use it
- `Version.Root` is the version's directory on disk when the version is one as it stands (a directory given with `--base-dir` or `--head-dir`). When it is empty — a revision from history, or the working tree, which a check copies into memory so that it sees one consistent snapshot — the gate package writes the version's files into a temporary directory once per check and removes it afterwards. Gates always see a real directory
- The engine stays free of process and file handling: running commands is this package's business, and `cmd/hadome` connects the two. `Rules` returns a closer that removes the copies; `cmd/hadome` closes it when the check is over
- A rule that implements `SelfTester` is tested by calling it, instead of by cases. `hadome self-test` loads the working tree's configuration to build the gate rules

## Running a command

| Step | Detail |
|---|---|
| Command | `run[0]` containing a `/` is a path relative to the directory hadome runs in (the repository's working tree), like shared configuration layers; otherwise it is looked up on `PATH`. So a command keeps its executable bit, which copies of a version do not |
| Working directory | The version's root |
| Input | The request of external-gates.md on standard input. `files` are the version's files matching `paths` (all files when `paths` is omitted), sorted |
| Output | Standard output is the JSON answer; standard error is passed to the user |
| Time | `timeout` (default 60s). A command still running then is stopped |
| Failure | A non-zero exit, invalid JSON, an unknown `schema`, or a timeout stops the check with exit code `2`: a check that could not run never passes |

`paths` are globs over slash-separated paths: `*` matches within one path element, `**` matches any number of elements.

## Configuration

| Key | Meaning |
|---|---|
| `id`, `kind`, `run`, `timeout` | As in the configuration of [external-gates.md](../external-gates.md) |
| `paths` | Globs of the files passed in. Omitted: every file |
| `options` | Any value; passed to the command untouched |

Gates are matched across versions by `id`. Any change to an existing gate needs review, not only removing it: a changed `run`, `paths` or `options` can make a gate more permissive in ways hadome cannot judge. The specification marks the list with `LooserWhenChanged` for this.

## Which gates may run

A gate runs a command. If the configuration in a change were enough to run a new command, a pull request that adds one line to `.hadome.yml` could run any command in CI before a person looked at it. So `hadome check` decides from base's configuration whether a command may run: base is what a person reviewed and merged.

| Head's gate | What `hadome check` does |
|---|---|
| Base has the same `id` with exactly the same `run` | Runs its self-test and runs it on both versions. It runs even when `paths`, `options` or `timeout` changed (the change itself goes to review as `config/loosened`) |
| Base has no gate with that `id` (the change adds it) | Does not run it, not even its self-test. Reports it for review as `config/gate-not-run` (exit code `3`) |
| Base has the same `id` with a different `run` | The same |
| Base has no `.hadome.yml` (a change that introduces hadome) | The same. Every gate is added |

- **What is compared**: only `run` decides what is executed. How the command is found (the handling of `run[0]`) and the working directory do not depend on the configuration. `paths`, `options` and `timeout` are values given to the command; they do not change the command itself
- **`config/gate-not-run`**: its `why` says the gate was not run, and why. Its `fix` says that once a person has reviewed the command, the gate runs in the checks after the change is on the base branch. Like `config/loosened` it is the engine's own report, so no exception marker silences it
- **Markers for a gate not run**: `gate:allow` markers for that gate are reported neither as markers for an unknown rule nor as markers that suppress nothing, so that a change adding a gate can write its exceptions in the same change
- **Why `3` even in a change that introduces hadome**: `config/loosened` is not raised in a change that introduces hadome ([configuration.md](configuration.md)), because there is nothing to loosen. A gate is not about loosening but about running a command nobody has reviewed yet, so it is held for review in an introducing change too. The first configuration the setup steps write has no gates, so following the steps never ends in `3`
- **`hadome self-test`**: runs the self-test of every gate in the working tree's configuration. It is the command for checking your own change locally, and the user runs it themselves. After adding a gate, check it works with `hadome self-test`

What this does not prevent:

- **The contents of the command files base runs**: `run[0]` is found in the working tree, so if the change rewrites the script of a gate base has, the rewritten script runs. This is the same trust as any other script in the repository (external-gates.md, "Trust")
- **Shared layers inside the repository**: shared layers (`extends`) are read from the working tree for both versions ([configuration.md](configuration.md)). Shared layers are meant to live outside the repository; with one inside it, a gate a change adds to that layer is taken as one base's configuration also has

## Invariants

1. A gate's findings are compared across versions exactly like a built-in rule's
2. A gate that cannot run, or answers something that is not the protocol, never lets a check pass (exit code `2`)
3. Removing or changing a gate needs a person's review (exit code `3`)
4. The engine itself never starts a process
5. `hadome check` runs only the gates base declares with the same `id` and the same `run`. Any other gate is not run, not even its self-test, and goes to a person's review (exit code `3`)

## Deliberately left out

- Running gates in parallel. One command at a time keeps diagnostics readable. The protocol does not stand in the way of running them in parallel
- Sandboxing. A gate runs with the permissions of any script in the repository (external-gates.md, "Trust")
