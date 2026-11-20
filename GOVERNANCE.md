# Governance

Who runs hadome, and how decisions are made.

## Roles

| Role | Who | Responsibilities |
|---|---|---|
| Maintainer | [@rerorets](https://github.com/rerorets) | Decides the direction and the design. Reviews and merges pull requests. Releases. Holds the accounts of the registries (npm, PyPI, crates.io, Maven Central, Homebrew). Handles vulnerability reports and Code of Conduct reports |
| Contributor | Anyone | Reports bugs, makes requests, sends changes and talks ideas over in issues, pull requests and discussions |

## How decisions are made

- The maintainer decides, and writes the reason in an issue, a pull request or a discussion.
- A new package or a large change of shape starts as a design document ([contributing](CONTRIBUTING.md)). Opinions are gathered on the design document's pull request before the code is written.
- If you disagree with a decision, say so in an issue or a discussion.

## Adding maintainers

- The maintainer invites people who keep contributing.
- Permissions are handed over in this order: review, merge, release.
- Once there are two or more maintainers, this document lists their names and what each looks after.

## Continuity

If the maintainer can no longer continue, [@nagiurrrr](https://github.com/nagiurrrr) takes over. So that issues can be handled, pull requests merged and versions released within a week, these permissions have already been handed over:

| What | Permission held |
|---|---|
| Opening and closing issues, merging pull requests | Write access to the repository |
| Tagging a release | Creating `v*` tags (the tag ruleset allows the write role) |
| Approving staged npm releases | Owner of the seven packages `hadome` and `hadome-<os>-<arch>` |
| Taking over the repository | Successor of the maintainer's GitHub account |

- The release workflow publishes with the credentials kept in the `release` and `pypi` environments. Pushing a tag publishes to every registry other than npm without approval.
- The credentials' tokens were made with the maintainer's accounts. To keep publishing after they expire, the successor has to be added as an owner on each service, or the namespace `io.github.rerorets` (Maven Central and the Gradle Plugin Portal) has to be moved.

## Related documents

- [Code of Conduct](CODE_OF_CONDUCT.md)
- [Security](SECURITY.md) and [the security assurance case](docs/assurance-case.md)
- [Releasing](docs/maintaining/releasing.md)
- [Roadmap](ROADMAP.md)
