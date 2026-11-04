# Language adapters

## Responsibility

A language adapter turns the files of one version into **facts** that rules can use without knowing any language. Rules ask "which tests read the clock?". The adapter knows that in Go this is `time.Now()`, in Python `datetime.now()`, and in Kotlin `Instant.now()`.

It does:

- Recognise which files belong to its language, and what role each file plays (source, test, generated)
- Extract comments, symbols, imports and tests as language-neutral facts
- Translate language-specific APIs into neutral **hazard categories** (clock, sleep, mock, always-true assertion)
- Extract test skips, with their reason and condition
- Say when it could not identify something, instead of guessing

It does not:

- Judge anything. Whether something is a violation is decided by the rules
- Run code, build the project, or call the language's toolchain (no `go`, `node`, `python`, `cargo`, `javac`, `kotlinc`), so that hadome stays one binary that works without them
- Parse exception markers. Adapters hand over comments; the engine finds `gate:allow` in them, with one implementation for every language

## The facts

```mermaid
classDiagram
  class Analysis {
    +Files []File
    +Comments []Comment
    +Symbols []Symbol
    +Imports []Import
    +Tests []Test
  }
  class File {
    +Path string
    +Language string
    +Role Role
    +Lines int
    +Unit string
    +UnitPath string
    +Unread bool
  }
  class Comment {
    +Path string
    +Line int
    +Text string
    +Directive bool
  }
  class Symbol {
    +ID string
    +Kind SymbolKind
    +Path string
    +Line int
    +EndLine int
    +Exported bool
    +Doc bool
    +Scoped bool
    +Params int
    +Complexity int
    +Methods int
  }
  class Import {
    +Path string
    +Line int
    +FromUnit string
    +ToUnit string
    +Internal bool
    +IO bool
  }
  class Test {
    +Name string
    +Path string
    +Line int
    +Declared map~string,[]string~
    +Calls []Call
    +Unresolved int
    +Reads int
    +Asserts int
    +AssertsUnknown bool
    +Body string
    +Hazards []Hazard
    +Skip Skip
  }
  class Call {
    +Target string
    +Times int
    +Scopes int
  }
  class Hazard {
    +Category HazardCategory
    +Path string
    +Line int
    +Evidence string
  }
  class Skip {
    +Skipped bool
    +Conditional bool
    +Reason string
    +Line int
  }
  class Language {
    <<interface>>
    +Name() string
    +Claims(path) bool
    +Analyze(v Version, c Config, paths []string) Analysis
  }
  Analysis --> File
  Analysis --> Comment
  Analysis --> Symbol
  Analysis --> Import
  Analysis --> Test
  Test --> Call
  Test --> Hazard
  Test --> Skip
  Language ..> Analysis : returns the facts of the files it claimed
```

### Files and roles

| Role | Meaning | How it is decided |
|---|---|---|
| `source` | Product code | Default |
| `test` | Test code | The language's conventions (below), then configuration overrides |
| `generated` | Generated code; size and comment rules skip it | Standard headers (e.g. Go's `Code generated … DO NOT EDIT.`), then configuration globs |
| `other` | A language hadome does not know, or not code | Still listed, so file-level rules can see it |

Test file conventions:

| Language | Test files |
|---|---|
| Go | `*_test.go` |
| TypeScript | `*.test.ts(x)`, `*.spec.ts(x)`, files under `__tests__/` |
| Python | `test_*.py`, `*_test.py` |
| Rust | `#[cfg(test)]` modules in any file, and files under `tests/` |
| Java | Files under `src/test/`, and classes with `@Test` methods |
| Kotlin | Files under `src/test/`, and classes with `@Test` methods |

### Units (for dependency rules)

A **unit** is what a dependency diagram draws a box for. Each language has its natural unit; configuration can group them.

| Language | Unit |
|---|---|
| Go | Package (directory) |
| TypeScript | Directory, after resolving `tsconfig` path aliases |
| Python | Package (directory with modules) |
| Rust | Module path within a crate |
| Java / Kotlin | Package |

`Internal` is true when the import points inside the repository. `IO` is true when the imported unit performs input or output, according to the adapter's `io` table and the entries configuration adds (`rules.tests.io`). Only imports of non-test files are listed: they are what the unit itself depends on.

### Hazard categories

Rules about how tests are written work on categories, not API names. Each adapter maps its language's APIs to them.

| Category | Go | Python | TypeScript | Java / Kotlin | Rust |
|---|---|---|---|---|---|
| `clock` | `time.Now`, `time.Since` | `datetime.now`, `time.time` | `Date.now`, `new Date()` | `Instant.now`, `LocalDateTime.now`, `System.currentTimeMillis` | `SystemTime::now`, `Instant::now` |
| `sleep` | `time.Sleep` | `time.sleep`, `asyncio.sleep` | `setTimeout` used to wait | `Thread.sleep`, `delay` | `thread::sleep` |
| `mock` | imports of mocking libraries | `unittest.mock`, `pytest-mock` | `jest.mock`, `vi.mock`, `mock.module` | Mockito, MockK | `mockall` |
| `always-true` | `if true`, comparing a value with itself | `assert True` | `expect(true).toBe(true)` | `assertTrue(true)` | `assert!(true)` |

Skips (`t.Skip`, `pytest.skip`, `@Disabled`, `#[ignore]` and so on) are not a hazard category. They are extracted as `Skip`, with their reason and condition. `tests/skip-without-reason` fails only skips without a reason.

These lists are examples, not the full tables. The full tables live in each adapter, and configuration can add entries (a product's own clock wrapper that tests must not bypass, a mocking library hadome does not know). Configuration cannot remove built-in entries.

### Assertions

For `tests/no-assertion` ([rules-tests.md](rules-tests.md)), the adapter counts each test's calls that check something (assertions) into `Test.Asserts`, including those made through helpers in test files.

| Language | Counted as an assertion (examples) |
|---|---|
| Go | the testing handle's `Error`, `Errorf`, `Fatal`, `Fatalf`, `Fail`, `FailNow`; testify's `assert` and `require` |
| Python | the `assert` statement, `self.assert…`, `pytest.raises`, `pytest.fail` |
| TypeScript | `expect(…)`, `assert` and `assert.…` |
| Java / Kotlin | JUnit's `assert…` and `fail`, AssertJ's and Hamcrest's `assertThat`, Mockito's `verify`, kotest's `should…` |
| Rust | `assert!`, `assert_eq!`, `assert_ne!`, `panic!`; a test marked `#[should_panic]` |

As with hazard categories, these are examples; each adapter holds the full table. Configuration can add entries under `rules.tests.assertions`, but never remove built-in ones.

When a test passes its testing handle (Go's `*testing.T`, Python's `self` and so on) to a function the adapter cannot read, it may check there, so the adapter sets `Test.AssertsUnknown`.

### Reading product values

For `tests/guards-nothing`, the adapter stores in `Test.Reads` how many product values a test reads without calling them, including through helpers in test files. When a value changes, the test's result can change too, so a test that calls no function may still check product code (for example, a test that checks an embedded document matches the README).

| Language | What counts as a product value (examples) |
|---|---|
| Go | Package variables and constants (`store.Limit`, or `limit` in the same package), and functions passed as values without being called (`sort.Slice(rows, store.Less)`) |
| Python | A name imported from product code and used in an expression without being called (comparing with `MAX_ROWS`, passing `normalize` to `map`, `isinstance(x, Store)`) |
| TypeScript | A name imported from product code and used in an expression without being called |
| Java / Kotlin | Static fields and constants of a class (`Store.LIMIT`), method references (`Store::normalize`), Kotlin top-level `val`s |
| Rust | Product `const`s and `static`s, and functions passed as values without being called. A product name inside a macro's arguments also counts (the parser does not read macro arguments as expressions, so a call there cannot be told from a read) |

Names in type positions (type annotations, TypeScript's `import type`, generic type arguments) do not count. Types do not run, so a test whose only mention of product code is in types runs no product code.

### The body's fingerprint

For `tests/duplicate`, the adapter makes a fingerprint of the sequence of tokens in each test's body and stores it in `Test.Body`. The test's name, comments and whitespace are not part of it. Literals are, with their values, so tests that differ in even one input have different fingerprints.

### What a test guards

People never classify tests. What a test guards is mostly worked out by machine, and the little that is declared is written by whoever writes the test (usually an agent).

| Piece | Who provides it | How |
|---|---|---|
| Target (the entry point the test exercises) | Inferred by the adapter | The public functions and methods the test body calls in non-test code, each as a `Call` with how many times it is called and with how many distinct scopes (the argument where the user or tenant goes; [rules-tests.md](rules-tests.md) defines both). When the call target cannot be identified (a method on a value whose type is unknown, a variable holding a function, a function of a package that could not be read), it is `unknown`. A declaration overrides the inference only when it is wrong |
| Viewpoint | Not declared | Hard to infer and costly to write. The guarded scope is the set of inferred targets until mutation analysis replaces it |
| Invariant link | Mostly inferred from the shape of the test's calls. The test's author writes one line only for product-specific invariants and for types without a shape | `guards: <invariant>` in the doc comment |

```go
// guards: durable store.Save
func TestSavedRowsSurviveRestart(t *testing.T) {
```

The adapter only collects `key: value` lines (a lower-case key, a colon, a value) from the doc comment into `Declared`, one entry per line, with whitespace normalised. The adapter does not know which keys exist. What a key means is decided by the rule that reads it ([rules-tests.md](rules-tests.md) for `guards`).

### Identified or not

What decides whether a fact can block is whether the adapter identified it, not how it was read.

| State | Example | Treatment |
|---|---|---|
| **Identified** | `Thread.sleep(` in code (not in a string or comment) · `@Disabled` without a reason · a 450-line file | Can block, in every language |
| **Unidentified** (`unknown`) | A dynamic import whose destination cannot be resolved · an unusual construct where a function's end cannot be found | Never passed silently. Reported as "could not identify" |

`unknown` is compared with base too: unknowns already in base pass, and a change that adds unknowns returns exit code `3` (needs review). An unknown never fails the check, so a guess never fails a build. An unknown is never passed silently either, so when unidentified facts increase, a person checks them.

Each fact also records how it was read. This is for diagnostics only and does not change whether the fact blocks.

| Read by | Meaning |
|---|---|
| `go/parser` | The Go standard library's parser |
| `tree-sitter` | A tree-sitter grammar |

## How each language is read

| Language | How it is read |
|---|---|
| Go | The standard library's parser (`go/parser`, `go/ast`). No C dependency |
| TypeScript, Python, Rust, Java, Kotlin | tree-sitter grammars |

Full parsers are used for every language because what users get (accurate function spans, test boundaries, annotations and imports, and few `unknown` facts) was put ahead of how easy hadome is to build. The extra work this brings (a C dependency for tree-sitter, and builds per operating system and architecture) is taken on by the release process, not by users. See [distribution.md](distribution.md).

`unknown` still exists: some things cannot be identified even with a parser (the contents of Rust macros, dynamic imports).

## Invariants

1. Adapters never execute code, start a process, or touch the network
2. The same file content always yields the same facts
3. Text inside a string literal or a comment is never reported as code (no hazard, import or symbol comes from it)
4. A file no adapter claims is still listed with role `other`
5. Configuration can add hazard entries but never remove built-in ones
6. A test body's fingerprint does not change when the name, comments or whitespace change. It changes when any literal changes

## Deliberately left out

- Resolving every module system completely (dynamic imports, Python path manipulation). Unresolvable imports are reported as `unknown`
- Type information (what a call resolves to across files). Rules work on syntax only
- Mutation analysis
- Languages beyond the six

## Decisions

1. Whether a fact can block depends on whether it was identified, not on how it was read. Unidentified facts are reported; a change that adds them returns exit code `3`
2. Go is read with the standard library's parser; the other five languages with tree-sitter. No shared tokenizer
3. Targets are inferred, viewpoints are not declared, and only tests that guard a product-specific invariant or one of the types without a shape carry a declaration (`guards:`)
4. Configuration can add hazard entries but never remove built-in ones. A place where a built-in entry truly does not apply gets a reasoned exception, which goes to a person for review (exit code `3`). If configuration could remove them, editing configuration alone could switch a rule off without any review by a person
