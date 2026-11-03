# Rules: `size/`

## Purpose

Code that grows past a size people and agents can hold in their heads gets changed by guesswork. The `size/` rules put an upper limit on files, functions and interfaces, and, like every rule, fail only what a change makes worse.

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

### What is measured

Only product code: source files and the symbols in them. Test files and generated files are not measured. Tests are long by nature (tables of cases), and generated code is nobody's to split.

### Worse is a regression

The anchor of a finding is the file or the symbol, so a violation already in base never blocks however lines move. A finding also carries the **measured value**. When a head finding matches one in base but its value is larger, it is a regression: an 80-line function that grows to 90 lines fails, one that shrinks to 75 passes. This is the engine's general rule for findings with a measure ([engine.md](engine.md)), not something `size/` does by itself.

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
| `Symbol.EndLine`, `Symbol.Params`, `Symbol.Complexity` | functions and methods |
| `Symbol.Kind` = `interface`, `Symbol.Methods` | `size/interface-methods` |

## Self-test cases

| Rule | Cases |
|---|---|
| `size/function-lines` | a new long function fails · an existing long function that moved passes · an existing long function that grew fails · one that shrank passes · a long test function passes |
| `size/file-lines` | a file that grows past the limit fails · a generated file passes |
| `size/params` | a new function with too many parameters fails |
| `size/complexity` | a new function with too many branches fails |
| `size/interface-methods` | a new wide interface fails |
| (configuration) | a lowered limit reports only new code · a raised limit needs review |

## Invariants

1. A limit applies to product code only; tests and generated code are never measured
2. A violation in base never blocks unless the change makes it larger

## Deliberately left out

- Per-path limits (a smaller limit for a frontend directory). Added when a product needs it, as a list of path globs with their own limits
- Counting "logical" lines (without comments or blank lines). Lines on screen are what a reader reads
