# Rules: `comments/`

## Purpose

Comments are read by the next person and the next agent as facts about the code. Three ways of writing make them disagree with the code or go missing: notes to do something "later" that never say when, public code with no explanation, and pointers to documents that no longer exist. The `comments/` rules stop a change from adding any of them.

## The rules

| Rule | Kind | Regression when |
|---|---|---|
| `comments/todo-format` | finding | A comment line starting with `TODO`, `FIXME` or `XXX` does not have the configured form |
| `comments/missing-doc` | finding | An exported function, method or type of product code has no doc comment; a value (a Terraform `variable` or `output`, an Ansible role's default) has no description |
| `comments/broken-reference` | finding | A comment names a Markdown file that does not exist in the version |

All are on by default. Generated files are never checked.

### `comments/todo-format`

- **Finds**: a comment line that begins (after the comment marker and spaces) with `TODO`, `FIXME` or `XXX`, and does not match `rules.comments.todo`
- **Default form**: `TODO(<when or who>): <what>` — the regular expression `^TODO\([^)]+\): \S`. `FIXME` and `XXX` never match it, so they are always findings: one word for "later" is enough
- **Why**: a TODO that does not say when tends to stay in the code with nobody working on it. A TODO left in the code misleads readers about what is finished
- **Fix**: say when or by whom (`TODO(stage 2): …`), or do it now, or delete it
  - The fix of a finding shows the configured form and an example that matches it. The example is the first of some common forms (`TODO(stage 2): …`, `TODO(#123): …`, `TODO(alice): …` and so on) that the configured form accepts. When none does, the fix shows no example
- A product sets its own form, such as `^TODO\(stage[0-9]+\): \S`. Changing the form needs a person's review, because hadome cannot tell whether the new one is stricter
- The anchor is the comment line's text, so moving the comment does not make it new

### `comments/missing-doc`

- **Finds**: an exported function, a method on an exported type, or an exported type (including interfaces) with no doc comment
- **Why**: the doc comment is what callers read instead of the body. Without it, every caller reads the implementation and guesses
- **Fix**: write one sentence that starts with the name and says what it does or is, and what a caller would otherwise get wrong
  - In TypeScript, JavaScript, Java and Kotlin a doc comment is `/** */`. A `//` line comment does not count as one, and the fix says so.
- **Not required**: a method that convention documents somewhere else (`Symbol.DocElsewhere`).

  | Language | Method | Where it is documented |
  |---|---|---|
  | Kotlin | `override fun` | the declaration it overrides |
  | Java | a method annotated `@Override` | the declaration it overrides |
  | TypeScript | a method marked `override` | the declaration it overrides |
  | TypeScript | a class's `constructor` | the class's doc comment |
  | Go | a method a standard library interface defines (list below) | that interface |

  - A Go method is left out only when its name and its parameter and result types all match the list. For example, `Len() int` is left out, but `Len() int64` needs a doc comment.
  - The Go list: `Error() string`, `String() string`, `ServeHTTP(http.ResponseWriter, *http.Request)`, `Unwrap() error`, `Unwrap() []error`, `Is(error) bool`, `As(any) bool`, `Format(fmt.State, rune)`, `MarshalJSON() ([]byte, error)`, `UnmarshalJSON([]byte) error`, `MarshalText() ([]byte, error)`, `UnmarshalText([]byte) error`, `Len() int`, `Less(int, int) bool`, `Swap(int, int)`
  - A Java constructor, and a TypeScript override not marked `override`, still need a doc comment: the declaration alone does not tell that it overrides anything.
- For a value (a symbol of kind `value`), the description is checked instead of a doc comment ([adapters-iac.md](adapters-iac.md)). The why and the fix are the ones for values
  - **Why**: whoever sets or reads the value reads its description instead of the code that uses it. Without one, each of them reads that code and guesses what the value is for
  - **Fix**: write in the description what the value is for and what a caller would otherwise get wrong: Terraform's `description` argument, or the variable's `description` in the role's `meta/argument_specs.yml`
- Test code is not checked: tests are not called by anyone
- The anchor is the symbol's ID

### `comments/broken-reference`

- **Finds**: a word in a comment that ends in `.md` (optionally followed by `#section`) and names a file that does not exist in the version
- **Resolution**: where to look depends on the form of the reference.

  | Form | Example | Where |
  |---|---|---|
  | starts with `./` or `../` | `./README.md` | relative to the commented file |
  | any other with a `/` | `docs/design/store.md` | relative to the repository root |
  | no `/` (a file name only) | `orders.md` | anywhere in the repository: it exists if any file has that name |

  - A reference by file name only usually means a document nearby. Which one cannot be decided, so any file of that name will do.
  - When looking for a file name only, the directories the adapters do not read (`vendor`, `node_modules`, those starting with `.` or `_`, and so on) are not searched.
- **Where a reference starts**: at the start of a line, or after a space, a bracket or a quote. The same holds after Japanese separators (`・`, `、`, `（`, `「`, `『`, `【`, `：`, and the full-width space). For example, the reference in `設計（docs/design/store.md）` is found.
- **Why**: when a comment refers to a design document, readers assume the reasoning is written there. If the document is missing, they cannot read it
- **Fix**: point at the document's current path, or remove the reference
- Only the file is checked, not the section after `#`
- The anchor is the reference as written

## Configuration

```yaml
rules:
  comments:
    todo: '^TODO\(stage[0-9]+\): \S'   # the form a TODO must have
```

`todo` must be a valid regular expression; an invalid one is a configuration error (exit code `2`).

## Facts

| Fact | Used for |
|---|---|
| `Comment` (path, line, text) and `File.Role` | `todo-format`, `broken-reference`. Comments marked `Directive` (`//go:embed`, `//line`) are instructions to a tool, not prose, and are skipped |
| `Symbol.Exported`, `Symbol.Doc`, `Symbol.DocElsewhere`, `Symbol.Kind` (including `type`) | `missing-doc` |

## Self-test cases

| Rule | Cases |
|---|---|
| `comments/todo-format` | a new bare TODO fails · a TODO with the form passes · a FIXME fails · an existing bare TODO that moved passes · "TODO" in the middle of a sentence passes · a product's own form is applied |
| `comments/missing-doc` | a new exported function without a doc comment fails · an unexported one passes · a method on an unexported type passes · an exported type without one fails · a test file is not checked · a Go `String() string` method passes · a method with the same name but another shape fails · a new Terraform `variable` without a `description` fails · a new `output` with one passes · a new Ansible default `argument_specs` does not describe fails · one it describes passes · one starting with `_` passes |
| `comments/broken-reference` | a reference to a missing file fails · a reference to an existing file passes · a relative reference resolves from the file · deleting a document that a comment names fails · a directive is not a reference · a URL is not a reference · a file name only, of a file elsewhere, passes · a file name only, of a file nowhere, fails · a file name only does not count a file in a skipped directory (`node_modules`, one starting with `.`) · a reference after a Japanese bracket is found |

## Invariants

1. Generated files are never checked
2. A comment already in base never blocks, wherever it moves

## Deliberately left out

- Checking that a doc comment is accurate. A rule cannot check it, so it is left to reviews and agents' instructions
- Checking `#section` anchors inside Markdown files
- References in other forms (URLs, `see foo.go`). Only Markdown documents are the design record
