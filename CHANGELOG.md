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
