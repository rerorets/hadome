# Changelog

The changes to hadome that its users can see, per version. How to write it and how the version goes up are in [docs/maintaining/releasing.md](docs/maintaining/releasing.md).

Each version's changes fall into four groups. They differ in what happens to a user's CI after upgrading.

| Group | What goes in | After upgrading |
|---|---|---|
| Finds more | New rules, checks that look at more, changes that send more to review (exit 3) | A user's CI may turn red or stop for review |
| Fewer false findings and misses | Findings that were wrong and no longer appear, tests that were not read and now are | Fewer findings |
| Configuration | New or changed `.hadome.yml` keys | Add the setting to use it |
| Other | Output, documents, distribution | Check results do not change |

Changes up to 0.4.0 are in the GitHub releases.

https://github.com/rerorets/hadome/releases

## 0.7.0 - 2026-11-18

### Finds more

- `deps/call-not-in-diagram` also counts calls made outside functions and methods: code at the top of a module, initial values of fields, decorators, default values of parameters, initial values of a Go package's `var`s, and the like. They count as calls from the file's unit.
- Calls through a module brought in with Rust's `use crate::store::{self, Finder}` are counted too.
- A new rule, `deps/cycle`, fails an import that makes a new cycle between units. It works without a diagram. It fails only when an import of a directory pair base does not have closes a cycle, and the message gives the way back (`store → domain → store`). When the importing file is Rust, Java or Kotlin, imports between a directory and its ancestor are not followed.
- A new rule, `deps/layer`, fails an import from an inner layer to an outer one, by the layers written in `rules.deps.layers`. Until layers are written, it does nothing.

### Fewer false findings and misses

- Imports inside Python's `if TYPE_CHECKING:` count as imports of types only, so they make no dependency for `deps/`.
- In Java and Kotlin, files under a `test/` or `test@…/` that is not inside `src/` are read as tests: the Kotlin Multiplatform layouts `jvm/test/` and `nonJvm/test/`, and Amper's `test@jvm/`. They used to be read as product code.

### Configuration

- `rules.deps.cycles.allow` takes directory pairs whose cycles `deps/cycle` does not follow, written `app/ui/** <-> app/presenter/**`. Adding a pair goes to review (exit 3).
- `rules.deps.layers` takes the layers from the inside out, each a name and directory globs. Writing them the first time needs no review; any later change goes to review (exit 3).

### Other

- `hadome suggest diagram` and `hadome suggest calls` draft the dependency and call diagrams from the imports and calls as they are; configuring a draft fails none of today's code. Boxes group units `--depth` levels down (2 by default), counting only directories that branch, per language. Pairs of boxes that depend on each other both ways are listed.
- The configuration `hadome init` writes, and the block of instructions for agents, recommend drafting the diagrams and proposing them to the person when there are none.

- `deps/call-not-in-diagram` reports a call at the line where it is written. It used to report at the declaration of the calling function.
- `hadome mutate` gives the test command a temporary directory of its own, and empties it at every run. Before, every `go test` stopped at its time limit left its work directory behind in the temporary directory.

## 0.6.0 - 2026-11-17

### Finds more

- A new rule, `deps/call-not-in-diagram`, checks calls between units against a call diagram. A change adding a call the diagram has no arrow for fails. Adding an arrow, and marking a box `wiring`, count as loosening the configuration and go to review (exit 3).
- `deps/diagram-stale` checks the boxes of the call diagram too.
- A call through an interface counts as a call to the units of its implementations. Implementations are recognised by `implements` and `extends` in Java, Kotlin and TypeScript, by method sets in Go, by `impl Trait for Type` in Rust, and by base classes and `Protocol` in Python.
- Making a class of another unit in place (`new Repo()`), and calling a method on what was made, count as calls too. A value made in another module and imported is followed as its class. This covers Java, Kotlin, TypeScript and Python.
- Calls missed in real repositories are now counted: a receiver with an operator in Kotlin (`!repo.ok()`), a call from an inner class to a field of the class around it in Java and Kotlin, names a Python package's `__init__.py` imports, and in Rust, names re-exported with `pub use` and names a `use` inside a function brings in.

### Fewer false findings and misses

- A Go name is looked up among the declarations that can be built with the caller. Functions of the same name split by build conditions (`//go:build`, or a file name such as `_linux.go`) are no longer mistaken for each other.
- `hadome mutate` does not try functions in files that cannot be built with the test. Mutants in files split by cgo no longer go to review (exit 3) as untried.
- `hadome check` with `--base-dir` and `--head-dir` no longer stops on a symbolic link to a directory or a broken one. As when reading the working tree, such links are not read as files.

### Configuration

- Naming a call diagram file in `rules.deps.calls` turns on `deps/call-not-in-diagram`. Calls of a box with the class `wiring` are not checked.

### Other

- With a call diagram, `hadome check` reports the units where method calls on values of unknown type grew. In JSON, they are in `unfollowed`. The exit code does not change.

## 0.5.2 - 2026-11-16

### Fewer false findings and misses

- In a repository with a submodule, `hadome check` against the working tree no longer stops with exit code `2`. A submodule is not read as files of this repository, as when reading a revision.

### Other

- When units lie under the directory a node's label names, `deps/diagram-stale` adds to its fix that writing `<label>/...` takes them in as one box.
- The external gate docs say that a relative path in a `run` argument is relative to the copy of the version being checked, and show a launcher that runs a gate added in the change on base as well.
- Building hadome from source needs Go 1.27. The distributed binaries are built with Go 1.27 too.

## 0.5.1 - 2026-11-14

### Fewer false findings and misses

- The query of a view or table function in a product `.sql` is read as tokens, without parsing its syntax. Syntax hadome cannot read in the query leaves the file read and the view a symbol.
- The expression of a BigQuery SQL function in a product `.sql` is read as tokens too. Syntax hadome cannot read in it leaves the file read and the function a symbol.
- `hadome mutate` matches functions sharing an ID with base by their body as well, so an unchanged function is never taken for a touched one and tried.

### Other

- npm has five platform packages. Windows on ARM64 installs `hadome-win32-x64`, which Windows 11 runs by translating the x86-64 binary. On Windows 10 on ARM64, hadome installed from npm does not run. The release archives, PyPI and `cargo binstall` ship a Windows ARM64 binary.
- `hadome mutate` runs, between the tests that call a broken function and all the tests, the tests that reach it through the units importing its unit. Verdicts do not change.
- `hadome check` analyses faster: it hashes a version's files once, copies each tree-sitter tree into Go nodes right after reading it, and reads each node kind of a test body once, among others. Results do not change. Copying the tree into Go nodes raises the maximum memory by up to about 20% on some files.
- The README opens with a demo GIF and the table of regression versus cleanup, folds the per-language installs, and compares hadome with similar tools.

## 0.5.0 - 2026-11-11

### Finds more

- An SQL adapter is added. Functions, procedures, views, tables and other objects defined in `.sql` files become symbols, and a test whose SQL strings name one is counted as guarding it. In a repository with `.sql` files, a change such as removing the last test that guards an SQL object is newly reported. The dialects read are PostgreSQL, MySQL (MariaDB) and BigQuery.
- A configuration change that alters how SQL names resolve goes to review (exit 3).
- Go mutate makes zero-value mutants for functions that return named types too.
- Go files the test build does not compile get no mutants and go to review (exit 3).
- When a mutant's test run times out, `hadome mutate` runs the clean run again to confirm the limit. If the limit grows, the mutant runs again with the longer limit, and it counts as caught only when it times out against a confirmed limit. The limit is confirmed at most twice per run. On a busy machine, a mutant that survives is no longer counted as caught, so new survivors may be reported. With `mutation.timeout` written, that value is the limit as it is.

### Fewer false findings and misses

- Go packages under `test/` and `e2e/` are test support like `testutil/`, while `tests/` stays product code.
- Mutate in every language makes no mutant that replaces a constant already zero with the zero value.
- Python test classes run the `setUp`, `setup_method` and autouse fixtures they inherit from base classes in the same file or an imported module, in Python's method resolution order.
- `size/complexity` counts each nested function on its own. A nested function over the limit is reported at its own line.
- `comments/broken-reference` does not report references to paths ignored by `.gitignore`.
- Mutants in Go files the test build does not compile are not counted as survivors.
- Go tests read:
  - the caller's type taken by a helper's interface parameter
  - the type of each variable of a call that returns several values
  - method values as calls
  - the declared type of each result of a method call
- Constant expressions that evaluate to zero, literals already zero and qualified zero constants get no zero-value mutant.
- Imports in TypeScript declaration files (`.d.ts`) are no dependency.
- Playwright tests are tests that drive the app.
- File-level mocks are not charged to tests that check purity only.
- `size/file-lines` points at the first line over the limit. An exception marker at the top of the file covers that finding.
- TypeScript tests read:
  - helper arguments bound by position
  - `describe.each` rows
  - functions returned by loaders
- Methods of values a TypeScript test builds itself are not counted as unresolved calls. Helpers in support files yield no tests.
- Kotlin tests walk top-level helpers. Spies are taken to call the real product code.
- Tests that drive the app are not asked for `no-mock`.
- Built-in calls on local strings and lists in JVM tests are not counted as unresolved.
- In Python, subclasses of `TestCase` are test classes, the values class autouse fixtures put on `self` are read, and an import alias binds only the alias.
- In Python, only the tests of test support code itself are spared `guards-nothing`, and setup is part of a test's body for duplicates.
- Java Mockito `@InjectMocks` fields hold the real product, the inputs of parameterised test classes enter the duplicate fingerprint, and Java helpers are followed at every call.
- Method calls on values of external concrete types such as Go's `*sql.DB` are not counted as unresolved.
- Test annotations that only give a name stay out of the duplicate fingerprint.
- A reference like `README/CLAUDE.md` reads as either file.
- A Go package's `TestMain` setup is counted as guarded by that package's tests.
- Rust `#[test_case]` and `#[test_matrix]` functions are tests with their cases as inputs.
- Idempotent candidates, in every language, are only those with I/O reached from a retryable entry point.
- In Ansible, a file that cannot be read as YAML is unread on its own. A Molecule scenario whose `verify.yml` cannot be read as YAML gives no test.

### Configuration

- `sql.databases` gives each database a name, a dialect, file globs and a default schema. Without it, every `.sql` file in the repository is read as one database.
- `rules.tests.sql-sinks` adds calls that take SQL, per language.

### Other

- `hadome check` analyses base and head in one call, and head does not read again a language, a file or an SQL catalog whose content is the same. The smaller a PR's change, the faster it is. Results do not change.
- `hadome check` analyses faster and uses less memory: it collects tests' strings only when SQL reads them, walks a JVM test class's setup once per class, and reads MySQL and BigQuery SQL with the fast prediction, among others. Results do not change.
- `hadome mutate` runs Go mutants against their tests' packages first, and against every test only when they survive. Outcomes do not change.
- `hadome mutate` sets each stage's time limit from that stage's own run without mutants.
- `hadome mutate` uses a fixed working directory per repository and refuses a second `mutate` in the same repository.
- `hadome mutate` stops the whole process tree of a test run on timeout, cancellation and exit, so no test binary of a mutant that never stops is left running.
- Symbols that share a short name are shown by their shortest distinct path.
- The agents guide (`docs/for-agents.md`) shows how to stop the changes a team forbids with gates and invariants, and what hadome cannot stop. The instructions `hadome agents` prints point to these examples.
- npm packages are staged through trusted publishing and approved by a maintainer with 2FA.
- The hadome binary is larger by the SQL grammars it holds.
- Release notes are written in the repository's `CHANGELOG.ja.md` and `CHANGELOG.md`.
- Actions used by the release workflow are pinned to commit SHAs.

## 0.4.3 - 2026-11-07

### Finds more

- The IO counted when deciding purity includes global IO APIs and more IO libraries and mocks. When purity is lost, `purity-lost` names the cause.
- TypeScript and Python count missing local imports as unresolved.
- Hazards in Kotlin and Java test setup (reading the clock, sleeping and so on) are found.
- Directories whose names start with `_` are read.
- An import into a directory hadome does not read goes to review (exit 3).
- The clock and sleep a Python setup uses are counted for each test.

### Fewer false findings and misses

- Purity is decided per import cycle. Type-only imports are not counted.
- TypeScript and Python imports resolve through nested projects, `tsconfig` `extends` and workspace packages. Tests are followed into test support files.
- Kotlin and Java tests read inherited and nested test classes, typed lambda calls, MockK and REST Assured checks, calls on enum constants, and KDoc above a split annotation.
- Lines are counted as an editor counts them.
- TypeScript tests are read in their curried and Playwright forms. The setup, hooks and fake timers around them run too.
- Tests in different files are copies only when they have the same targets.
- Overrides need no doc comment.
- A reference by bare file name is found anywhere in the repository.
- Go tests reach methods through the declared types of package variables, literals, helper results, range elements and fields. Their own package's unexported methods are targets too.
- Go test helper packages are test support, and tests are followed into them.
- `vi.fn` passed as a value is no mock.
- Go helpers and function literals read their parameters by declared type in their own scope. Test package variables are typed from any test file of the package.
- Kotlin declarations after several annotations with arguments are read. Kotlin tests read string-template constants and value fields inherited from another file.
- Go mutate makes no mutant that replaces a constant already zero with the zero value.

### Configuration

- Framework entry points can be let off doc comments.
- Test support paths listed in `files.product` are product code.

### Other

- Messages, help, and the output of `init` and `mutate` say what a reader needs. `suggest` reads camelCase and snake_case.

## 0.4.2 - 2026-11-07

### Other

- The Japanese documents state definitions plainly and agree with the code and CI.

## 0.4.1 - 2026-11-06

### Fewer false findings and misses

- TypeScript tests reach the code through object-held helpers and variables set up outside the test.
- Python tests reach the code through pytest fixtures, `setUp` and lambda helpers.
- Rust paths starting with the crate's own name are the repository's, as integration tests write them.
- Go tests reach the code through their file's package-level variables.
- Go tests that run a testify suite reach the code through its methods.
- Kotlin tests use their companion object's fields and helpers as the class's own.
