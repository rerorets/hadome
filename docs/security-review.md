# Security review

This is the record of hadome's security reviews. What is protected and where the trust boundaries lie is in [the security assurance case](assurance-case.md); a review checks that its claims match the code.

## November 2026 review

| Item | What |
|---|---|
| When | 2026-11-20 to 2026-11-21 |
| Scope | main as of 0.10.1. What was found is fixed in 0.11.0 |
| Reviewed by | The maintainer (rerorets). An AI agent (Claude Code) traced the code, and every finding was reproduced locally before it was accepted |
| What an attacker controls | Everything in a pull request: files, file names, `.hadome.yml`, symbolic links, exception reasons |
| What is trusted | The base branch's content and configuration, the maintainers, GitHub and the registries hadome is published to |

### Method

- For each threat in [the security assurance case](assurance-case.md), the code was read to see whether it does what the countermeasure says.
- From the inputs an attacker controls, four paths were traced: what hadome reads, runs, writes and prints.
- Only what could be reproduced became a finding.

### Findings and fixes

| Severity | Finding | Fix | PR |
|---|---|---|---|
| Medium | `hadome check` ran the commands of `gates` a pull request added to `.hadome.yml` before a person had reviewed them | Only gates the base branch's configuration declares with the same `id` and the same `run` are run. Any other gate is not run and goes to a person as `config/gate-not-run` with exit code `3` | #257 |
| Medium | With `--head REV`, a file name holding a newline shifted the answers of `git cat-file --batch`, so a file with a violation was read as other content and missed | git is given object IDs only, and each answer is checked to be the object asked for. A missing answer stops hadome with exit code `2` | #263 |
| Low | `extends` and symbolic links could make hadome read files outside the repository. Configuration validation errors showed part of the values, which could end up in a CI log | Symbolic links are not followed and are read as their target text. Only the `extends` base's configuration lists are read. Errors in shared configuration show no values | #263 |
| Low | A symbolic link to `/dev/zero`, or a FIFO, made hadome read until memory ran out | Only regular files are read. A file over 128 MiB is not read and goes to a person, and a configuration file over 1 MiB stops hadome | #263 |
| Low | The text output printed file names and exception reasons as they were, so terminal control characters or Actions commands (`::error` and the like) could be slipped in | A value with a control character is quoted and escaped. The CI example escapes the values it puts into warning commands and table cells | #253 |
| Low | Checkouts in the release jobs left git credentials in `.git/config` (no way to abuse them was found) | Checkouts of this repository use `persist-credentials: false` | #256 |

### What was found sound

- Network: hadome does not talk to the network.
- Running commands: no shell is involved, and arguments are passed separately. `--end-of-options` precedes every revision given to git (added in #263).
- Distribution: packages carry checksums and signatures, and the release workflows have only the permissions they need.
- Secrets: the repository and its workflows hold no secret values.

### Documents corrected

[The security assurance case](assurance-case.md) and [SECURITY.md](../SECURITY.md) made three claims that did not match the code at the time of the review. All three were corrected along with the fixes above.

- `hadome check` does not run the repository's code
- hadome reads only the repository it is pointed at
- hadome does not write through symbolic links

## Next review

The next review comes before 1.0.0. Apart from that, a review is done whenever a change moves a trust boundary, such as a new command that runs code or a new way of reading input.
