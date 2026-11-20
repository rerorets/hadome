# Roadmap

What hadome works on in the year from November 2026 to November 2027. This is not a fixed schedule; when it changes, this document changes.

## Before 1.0.0

| Order | What | Done when |
|---|---|---|
| 1 | Support more languages, starting with Swift | Finding tests, reading imports, measuring size and reading docblocks all work for Swift |
| 2 | Tune how fine-grained the rules are | An AI agent runs the loop of fixing what hadome reports while building real software, and the record of it shows the findings have settled at a size that is neither too fine nor too coarse |
| 3 | Release 1.0.0 | 1 and 2 are done |

- A new language starts as a design document, like any other package ([contributing](CONTRIBUTING.md)).
- From 1.0.0, changing configuration keys, the shape of the JSON output or the meaning of exit codes raises the major version ([versioning](CONTRIBUTING.md#versioning)).

## Giving your opinion

Write the languages you want, or examples of findings that felt too fine or too coarse, in [issues](https://github.com/rerorets/hadome/issues) or [discussions](https://github.com/rerorets/hadome/discussions). They go into the tuning.
