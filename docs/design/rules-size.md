# Rules: `size/`

## Purpose

Code that grows past what people and agents can read in one go tends to be changed without understanding all of it. The `size/` rules put an upper limit on files, functions and interfaces, and, like every rule, fail only what a change makes worse.

## The rules

| Rule | Measures | Default limit |
|---|---|---|
| `size/file-lines` | Lines of a source file | 400 |
| `size/function-lines` | Lines of a function or method, from its declaration to its closing brace | 60 |
| `size/params` | Parameters of a function or method (the receiver does not count) | 5 |
| `size/complexity` | Cyclomatic complexity of a function or method | 15 |
| `size/interface-methods` | Methods of an interface | 5 |

All are findings and on by default.

- **Finds**: a file, function or interface over its limit
- **Why**: past the limit, the code no longer fits in one reading; changes to it get made without understanding all of it
- **Fix**: split it along a line it already has (a helper per step, a type per concern). Raising the limit is a configuration change a person reviews

Cyclomatic complexity is 1, plus one for each `if`, `for`, `case` (not `default`), `&&` and `||`. Each adapter maps its language's branches to the same count.

### Nested functions

A function inside a function is counted apart from the one that holds it, as ESLint's `complexity` does. Nested functions are arrow functions, lambdas, closures, Go function literals, nested `def`s, methods of anonymous classes and the like.

- **The outer function**: counts only its own branches. Branches inside its nested functions are not added. Example: a function with no branches of its own that holds an arrow function with 16 `if`s has complexity 1.
- **Each nested function**: counts only its own branches, against the same limit. A function nested in a nested function is counted on its own too.
- **A nested function over the limit**: is reported as a violation of the enclosing symbol (function or method), with the nested function's line in the message. Example: "the function in `setup` at line 4: cyclomatic complexity 16, limit 15". The violation's line is the nested function's line too.

Nested functions are reported under the enclosing symbol so that named ones (`const onMessage = () =>`) and anonymous ones are reported the same way in all six languages. Making each nested function a symbol of its own would add new symbols to other rules too (`size/function-lines`, `comments/` and others).

Against base, a nested function's violation is matched by the enclosing symbol. Line numbers are not used for matching. So a nested function that only moved is not a regression. More nested functions over the limit in the same enclosing symbol, or a larger value for one of them, is a regression. They are matched apart from the outer function's own violation.

Base and head are measured with the same counting, so a change in how it counts never makes an unchanged file regress.

### What is measured

Only product code: source files and the symbols in them. Test files and generated files are not measured. Tests are long by nature (tables of cases), and generated code is nobody's to split.

### Worse is a regression

The anchor of a finding is the file or the symbol, so a violation already in base never blocks however lines move. A finding also carries the **measured value**. When a head finding matches one in base but its value is larger, it is a regression: an 80-line function that grows to 90 lines fails, one that shrinks to 75 passes. This is the engine's general rule for findings with a measure ([engine.md](engine.md)), not something `size/` does by itself.

### The line a file-lines finding points at

A `size/file-lines` finding points at the first line over the limit. Example: with a limit of 300, line 301. `text` writes `store/store.go:301`, and `json` sets `line` to `301`.

- **How the line is decided**: by the limit alone, not by the file's content. So it is the same line in a file of any language.
- **Why it has a line**: without one, editors and CI annotations point at the top of the file. The first line over the limit shows where the part over the limit starts.
- **Matching against base**: the anchor of the finding is the file. The line is not used for matching. So a line that changes because the limit changed is no regression by itself.
- **Exception markers**: as for every other rule, a marker applies to its own line and the next one. So the marker goes on the reported line or the line before it. Example: with a limit of 300, line 300 or 301. When lines added above the marker, or a changed limit, move the marker off that line, it suppresses nothing. Then both the finding and `engine/marker-unused` are reported.

## Configuration

```yaml
rules:
  size:
    file-lines: 300           # any of the five; omitted keys keep the default
    function-lines: 60
    params: 5
    complexity: 15
    interface-methods: 5
```

A higher limit is looser than a lower one, so raising a limit needs a person's review (exit code `3`), and lowering it never does. Limits come from head's configuration and apply to both versions ([configuration.md](configuration.md)), so lowering a limit reports only what the change adds, never what was already over it.

## Facts

| Fact | Used for |
|---|---|
| `File.Lines`, `File.Role` | `size/file-lines` |
| `Symbol.EndLine`, `Symbol.Params`, `Symbol.Complexity`, `Symbol.Nested` | functions and methods; `Nested` holds the line and complexity of each nested function |
| `Symbol.Kind` = `interface`, `Symbol.Methods` | `size/interface-methods` |

## Self-test cases

| Rule | Cases |
|---|---|
| `size/function-lines` | a new long function fails · an existing long function that moved passes · an existing long function that grew fails · one that shrank passes · a long test function passes |
| `size/file-lines` | a file that grows past the limit fails · an unchanged file over the limit passes · a generated file passes · an exception marker on the first line over the limit suppresses the file's finding |
| `size/params` | a new function with too many parameters fails · five parameters pass |
| `size/complexity` | a new function with too many branches fails · a function at the limit passes · `default` is not a branch · a nested function's branches are not added to the outer function · a new nested function with too many branches fails · an unchanged nested function passes |
| `size/interface-methods` | a new wide interface fails · an interface of five passes |
| (configuration) | a lowered limit reports only new code · a raised limit needs review |

## Invariants

1. A limit applies to product code only; tests and generated code are never measured
2. A violation in base never blocks unless the change makes it larger

## Deliberately left out

- Per-path limits (a smaller limit for a frontend directory)
- Counting "logical" lines (without comments or blank lines). Lines on screen are what a reader reads
