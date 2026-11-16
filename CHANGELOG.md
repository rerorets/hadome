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

## 0.6.0 - 2026-11-17

### Finds more

- A new rule, `deps/call-not-in-diagram`, checks calls between units against a call diagram. A change adding a call the diagram has no arrow for fails. Adding an arrow, and marking a box `wiring`, count as loosening the configuration and go to review (exit 3). (#172)
- `deps/diagram-stale` checks the boxes of the call diagram too. (#172)
- A call through an interface counts as a call to the units of its implementations. Implementations are recognised by `implements` and `extends` in Java, Kotlin and TypeScript, by method sets in Go, by `impl Trait for Type` in Rust, and by base classes and `Protocol` in Python. (#173, #174, #175)
- Making a class of another unit in place (`new Repo()`), and calling a method on what was made, count as calls too. A value made in another module and imported is followed as its class. This covers Java, Kotlin, TypeScript and Python. (#177, #180)
- Calls missed in real repositories are now counted: a receiver with an operator in Kotlin (`!repo.ok()`), a call from an inner class to a field of the class around it in Java and Kotlin, names a Python package's `__init__.py` imports, and in Rust, names re-exported with `pub use` and names a `use` inside a function brings in. (#179, #180, #188)

### Fewer false findings and misses

- A Go name is looked up among the declarations that can be built with the caller. Functions of the same name split by build conditions (`//go:build`, or a file name such as `_linux.go`) are no longer mistaken for each other. (#185)
- `hadome mutate` does not try functions in files that cannot be built with the test. Mutants in files split by cgo no longer go to review (exit 3) as untried. (#185)
- `hadome check` with `--base-dir` and `--head-dir` no longer stops on a symbolic link to a directory or a broken one. As when reading the working tree, such links are not read as files. (#189)

### Configuration

- Naming a call diagram file in `rules.deps.calls` turns on `deps/call-not-in-diagram`. Calls of a box with the class `wiring` are not checked. (#172)

### Other

- With a call diagram, `hadome check` reports the units where method calls on values of unknown type grew. In JSON, they are in `unfollowed`. The exit code does not change. (#178)

## 0.5.2 - 2026-11-16

### Fewer false findings and misses

- In a repository with a submodule, `hadome check` against the working tree no longer stops with exit code `2`. A submodule is not read as files of this repository, as when reading a revision. (#168)

### Other

- When units lie under the directory a node's label names, `deps/diagram-stale` adds to its fix that writing `<label>/...` takes them in as one box. (#164)
- The external gate docs say that a relative path in a `run` argument is relative to the copy of the version being checked, and show a launcher that runs a gate added in the change on base as well. (#163)
- Building hadome from source needs Go 1.27. The distributed binaries are built with Go 1.27 too. (#166)

## 0.5.1 - 2026-11-14

### Fewer false findings and misses

- The query of a view or table function in a product `.sql` is read as tokens, without parsing its syntax. Syntax hadome cannot read in the query leaves the file read and the view a symbol. (#151)
- The expression of a BigQuery SQL function in a product `.sql` is read as tokens too. Syntax hadome cannot read in it leaves the file read and the function a symbol. (#154)
- `hadome mutate` matches functions sharing an ID with base by their body as well, so an unchanged function is never taken for a touched one and tried. (#149)

### Other

- npm has five platform packages. Windows on ARM64 installs `hadome-win32-x64`, which Windows 11 runs by translating the x86-64 binary. On Windows 10 on ARM64, hadome installed from npm does not run. The release archives, PyPI and `cargo binstall` ship a Windows ARM64 binary. (#156)
- `hadome mutate` runs, between the tests that call a broken function and all the tests, the tests that reach it through the units importing its unit. Verdicts do not change. (#144)
- `hadome check` analyses faster: it hashes a version's files once, copies each tree-sitter tree into Go nodes right after reading it, and reads each node kind of a test body once, among others. Results do not change. Copying the tree into Go nodes raises the maximum memory by up to about 20% on some files. (#143, #145, #150)
- The README opens with a demo GIF and the table of regression versus cleanup, folds the per-language installs, and compares hadome with similar tools. (#146, #148)

## 0.5.0 - 2026-11-11

### Finds more

- An SQL adapter is added. Functions, procedures, views, tables and other objects defined in `.sql` files become symbols, and a test whose SQL strings name one is counted as guarding it. In a repository with `.sql` files, a change such as removing the last test that guards an SQL object is newly reported. The dialects read are PostgreSQL, MySQL (MariaDB) and BigQuery. (#120)
- A configuration change that alters how SQL names resolve goes to review (exit 3). (#120)
- Go mutate makes zero-value mutants for functions that return named types too. (#92)
- Go files the test build does not compile get no mutants and go to review (exit 3). (#97)
- When a mutant's test run times out, `hadome mutate` runs the clean run again to confirm the limit. If the limit grows, the mutant runs again with the longer limit, and it counts as caught only when it times out against a confirmed limit. The limit is confirmed at most twice per run. On a busy machine, a mutant that survives is no longer counted as caught, so new survivors may be reported. With `mutation.timeout` written, that value is the limit as it is. (#129)

### Fewer false findings and misses

- Go packages under `test/` and `e2e/` are test support like `testutil/`, while `tests/` stays product code. (#89)
- Mutate in every language makes no mutant that replaces a constant already zero with the zero value. (#92)
- Python test classes run the `setUp`, `setup_method` and autouse fixtures they inherit from base classes in the same file or an imported module, in Python's method resolution order. (#93)
- `size/complexity` counts each nested function on its own. A nested function over the limit is reported at its own line. (#95)
- `comments/broken-reference` does not report references to paths ignored by `.gitignore`. (#96)
- Mutants in Go files the test build does not compile are not counted as survivors. (#97)
- Go tests read: (#98, #117)
  - the caller's type taken by a helper's interface parameter
  - the type of each variable of a call that returns several values
  - method values as calls
  - the declared type of each result of a method call
- Constant expressions that evaluate to zero, literals already zero and qualified zero constants get no zero-value mutant. (#98, #99, #103)
- Imports in TypeScript declaration files (`.d.ts`) are no dependency. (#99)
- Playwright tests are tests that drive the app. (#99)
- File-level mocks are not charged to tests that check purity only. (#99)
- `size/file-lines` points at the first line over the limit. An exception marker at the top of the file covers that finding. (#100, #112)
- TypeScript tests read: (#102)
  - helper arguments bound by position
  - `describe.each` rows
  - functions returned by loaders
- Methods of values a TypeScript test builds itself are not counted as unresolved calls. Helpers in support files yield no tests. (#102)
- Kotlin tests walk top-level helpers. Spies are taken to call the real product code. (#103)
- Tests that drive the app are not asked for `no-mock`. (#103)
- Built-in calls on local strings and lists in JVM tests are not counted as unresolved. (#103)
- In Python, subclasses of `TestCase` are test classes, the values class autouse fixtures put on `self` are read, and an import alias binds only the alias. (#109)
- In Python, only the tests of test support code itself are spared `guards-nothing`, and setup is part of a test's body for duplicates. (#109)
- Java Mockito `@InjectMocks` fields hold the real product, the inputs of parameterised test classes enter the duplicate fingerprint, and Java helpers are followed at every call. (#114)
- Method calls on values of external concrete types such as Go's `*sql.DB` are not counted as unresolved. (#116)
- Test annotations that only give a name stay out of the duplicate fingerprint. (#116)
- A reference like `README/CLAUDE.md` reads as either file. (#116)
- A Go package's `TestMain` setup is counted as guarded by that package's tests. (#118)
- Rust `#[test_case]` and `#[test_matrix]` functions are tests with their cases as inputs. (#117)
- Idempotent candidates, in every language, are only those with I/O reached from a retryable entry point. (#118)
- In Ansible, a file that cannot be read as YAML is unread on its own. A Molecule scenario whose `verify.yml` cannot be read as YAML gives no test. (#127)

### Configuration

- `sql.databases` gives each database a name, a dialect, file globs and a default schema. Without it, every `.sql` file in the repository is read as one database. (#120)
- `rules.tests.sql-sinks` adds calls that take SQL, per language. (#120)

### Other

- `hadome check` analyses base and head in one call, and head does not read again a language, a file or an SQL catalog whose content is the same. The smaller a PR's change, the faster it is. Results do not change. (#126)
- `hadome check` analyses faster and uses less memory: it collects tests' strings only when SQL reads them, walks a JVM test class's setup once per class, and reads MySQL and BigQuery SQL with the fast prediction, among others. Results do not change. (#123, #124, #125)
- `hadome mutate` runs Go mutants against their tests' packages first, and against every test only when they survive. Outcomes do not change. (#110)
- `hadome mutate` sets each stage's time limit from that stage's own run without mutants. (#110)
- `hadome mutate` uses a fixed working directory per repository and refuses a second `mutate` in the same repository. (#110)
- `hadome mutate` stops the whole process tree of a test run on timeout, cancellation and exit, so no test binary of a mutant that never stops is left running. (#119)
- Symbols that share a short name are shown by their shortest distinct path. (#118)
- The agents guide (`docs/for-agents.md`) shows how to stop the changes a team forbids with gates and invariants, and what hadome cannot stop. The instructions `hadome agents` prints point to these examples. (#131)
- npm packages are staged through trusted publishing and approved by a maintainer with 2FA. (#115)
- The hadome binary is larger by the SQL grammars it holds. (#120)
- Release notes are written in the repository's `CHANGELOG.ja.md` and `CHANGELOG.md`. (#106, #107)
- Actions used by the release workflow are pinned to commit SHAs. (#90)

## 0.4.3 - 2026-11-07

### Finds more

- The IO counted when deciding purity includes global IO APIs and more IO libraries and mocks. When purity is lost, `purity-lost` names the cause. (#75)
- TypeScript and Python count missing local imports as unresolved. (#76)
- Hazards in Kotlin and Java test setup (reading the clock, sleeping and so on) are found. (#77)
- Directories whose names start with `_` are read. (#78)
- An import into a directory hadome does not read goes to review (exit 3). (#83)
- The clock and sleep a Python setup uses are counted for each test. (#83)

### Fewer false findings and misses

- Purity is decided per import cycle. Type-only imports are not counted. (#75)
- TypeScript and Python imports resolve through nested projects, `tsconfig` `extends` and workspace packages. Tests are followed into test support files. (#76)
- Kotlin and Java tests read inherited and nested test classes, typed lambda calls, MockK and REST Assured checks, calls on enum constants, and KDoc above a split annotation. (#77)
- Lines are counted as an editor counts them. (#78)
- TypeScript tests are read in their curried and Playwright forms. The setup, hooks and fake timers around them run too. (#79)
- Tests in different files are copies only when they have the same targets. (#80)
- Overrides need no doc comment. (#80)
- A reference by bare file name is found anywhere in the repository. (#80)
- Go tests reach methods through the declared types of package variables, literals, helper results, range elements and fields. Their own package's unexported methods are targets too. (#81)
- Go test helper packages are test support, and tests are followed into them. (#82)
- `vi.fn` passed as a value is no mock. (#83)
- Go helpers and function literals read their parameters by declared type in their own scope. Test package variables are typed from any test file of the package. (#84)
- Kotlin declarations after several annotations with arguments are read. Kotlin tests read string-template constants and value fields inherited from another file. (#85)
- Go mutate makes no mutant that replaces a constant already zero with the zero value. (#86)

### Configuration

- Framework entry points can be let off doc comments. (#82)
- Test support paths listed in `files.product` are product code. (#82)

### Other

- Messages, help, and the output of `init` and `mutate` say what a reader needs. `suggest` reads camelCase and snake_case. (#78, #80)

## 0.4.2 - 2026-11-07

### Other

- The Japanese documents state definitions plainly and agree with the code and CI. (#74)

## 0.4.1 - 2026-11-06

### Fewer false findings and misses

- TypeScript tests reach the code through object-held helpers and variables set up outside the test. (#67)
- Python tests reach the code through pytest fixtures, `setUp` and lambda helpers. (#68)
- Rust paths starting with the crate's own name are the repository's, as integration tests write them. (#69)
- Go tests reach the code through their file's package-level variables. (#70)
- Go tests that run a testify suite reach the code through its methods. (#71)
- Kotlin tests use their companion object's fields and helpers as the class's own. (#72)
