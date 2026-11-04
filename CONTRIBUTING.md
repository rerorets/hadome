# Contributing

Bugs and requests go to [Issues](https://github.com/rerorets/hadome/issues). Send changes to code or documents as pull requests.

## Map of the documents

| Reader | Documents |
|---|---|
| People who use hadome | [README](README.md), [three layers for trusting tests](docs/model.md), [product-specific gates](docs/external-gates.md) |
| Agents working in a user's repository | [The guide for agents](docs/for-agents.md). The block `hadome agents` writes is `cmd/hadome/agents.en.md` |
| People who build hadome | This document, [the design documents](docs/design), [the dependency diagram](docs/design/dependencies.md) |
| Maintainers | [Releasing](docs/maintaining/releasing.md), [the JVM plugins](packaging/jvm/README.md) |

## Running it locally

With Go and a C compiler, the build reads all six languages. Without a C compiler, it reads Go only and reports files of the other languages as unread.

```sh
go test ./...                           # the build that reads six languages (cgo)
CGO_ENABLED=0 go test ./...             # the Go-only build
go run ./cmd/hadome self-test           # every rule's self-test
go run ./cmd/hadome check --format json # hadome checks itself
packaging/smoke.sh 0.0.0-local          # build, install and run every package
```

On every pull request, CI runs the first four on Linux, macOS and Windows, and the last one on Linux.

### Hooks

Enable them once.

```sh
git config core.hooksPath .githooks
```

`.githooks/pre-commit` stops commits using a private list of terms that the maintainer keeps locally. Without the list it does nothing.

## How to make a change

- A new package, or a large change of shape, starts as a design document in `docs/design/`: its responsibility, what it does not do, a Mermaid diagram of types and interfaces, invariants, and what is deliberately left out. Get it reviewed in a pull request with the design document alone, then write the code.
- A new package or import starts with an arrow in [the dependency diagram](docs/design/dependencies.md). An import the diagram does not draw fails hadome's check of itself.
- A new rule comes with its self-test.
- When `go run ./cmd/hadome check` returns exit code `3`, say in the pull request what needs a person's review.
- Pull requests are squash-merged. The pull request's title becomes the commit message on main, and a line of the release notes.

## Writing

- Code, comments and commit messages are in English.
- Every document has two files, Japanese `foo.ja.md` and English `foo.md`. The Japanese is the source: change it first, then bring the English up to it in the same change. If the two do not have the same headings and code blocks, `cmd/hadome/docs_test.go` fails.
- Comments say only what reading the code does not tell: for example, why an unusual approach was taken, or an invariant. No history, progress notes or ticket numbers.
- Tests follow [the three layers for trusting tests](docs/model.md): test observable behaviour only, never copy expected values from the implementation, never read the clock, never wait with sleep, never skip silently.
- This is a public repository: bring in no code, names, paths or table names from an employer or a private project. Use generic examples such as `store.Append` or "product A".

## License

hadome is under the [MIT](LICENSE) license. Changes you send are published under the same license.
