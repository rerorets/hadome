# Releasing

A release starts by pushing a version tag. The `release` workflow ([.github/workflows/release.yml](../../.github/workflows/release.yml)) does everything after that. [distribution.md](../design/distribution.md) explains the design.

## Release steps

1. Check that CI on main passes.
2. Run `hadome mutate` on the change since the previous tag, and check that it reports no `mutation/survived` regression. If it does, merge a PR that adds the tests first.
   - When the change exceeds `mutation.max-mutants`, the mutants not tried are reported as a review (exit 3). That review does not hold back a release.
3. Open a PR that raises the version, and merge it. Title it like `The README pins 1.2.0`. The PR changes two things:
   - the version written in `README.md` and `README.ja.md`
   - the new version's section in [CHANGELOG.ja.md](../../CHANGELOG.ja.md) and [CHANGELOG.md](../../CHANGELOG.md) (see "[Changelog](#changelog)")
4. Put a signed tag on that PR's merge commit, and push it.
5. Check the workflow's results. Fix the cause of a failed job, then rerun only that job.

```sh
git fetch origin
git tag -s v1.2.0 -m "hadome 1.2.0" <merge commit of the version PR>
git push origin v1.2.0
```

- Tag the merge commit of the version PR. The version in the README at the tagged commit then matches the release. Naming the commit keeps them matched even when another PR is merged before you tag.
- Never delete or move a tag once it is pushed. Fix a mistake in the next patch release. The Go module proxy keeps serving the first content it saw for a tag, and npm, PyPI and crates.io cannot replace a published version. A moved tag leaves each channel with different content.
- The GitHub release's body is the tagged version's section of `CHANGELOG.md`, followed by a link to `CHANGELOG.ja.md`.

Sign the tag. GitHub then marks it Verified, which shows who tagged the release. The workflow does not check the signature, so an unsigned tag behaves the same.

The workflow builds the six binaries natively, publishes the GitHub release with `SHA256SUMS`, then publishes to each ecosystem that is switched on. Running the workflow by hand (Actions → release → Run workflow) builds and checks everything without publishing.

If publishing fails part way, fix the cause and re-run only the failed jobs. The npm job skips versions already on the registry.

## Changelog

The release notes are `CHANGELOG.ja.md` and `CHANGELOG.md` in the repository. They are not made with GitHub features (PR labels, generated release notes).

### Who writes it, and when

- The new version's section is written in the version PR. The README version and the changelog merge in the same PR.
- An AI drafts it from the titles and descriptions of the PRs merged into main since the previous tag.
- A person reads and edits the draft, then merges the PR.

```sh
git log --first-parent --format='%s' v1.1.0..origin/main
```

- A section's heading looks like `## 1.2.0 - 2026-12-01`: the version, a space, `-`, a space, the date. The workflow looks for the section whose heading's second word equals the version.
- A trial version (`1.2.0-rc.1`) needs a section with its own heading too.

### Grouping

The groups differ in what happens to a user's CI after upgrading. Leave out empty groups.

| Group (Japanese) | Group (English) | What goes in |
|---|---|---|
| 検出が増える変更 | Finds more | New rules, checks that look at more, changes that send more to review (exit 3). Upgrading may turn a user's CI red or stop it for review |
| 誤検知・漏れの修正 | Fewer false findings and misses | Findings that were wrong and no longer appear, tests that were not read and now are |
| 設定 | Configuration | New or changed `.hadome.yml` keys |
| その他 | Other | Output, documents, distribution. Check results do not change |

- One line per change, ending with the PR number. Example: "`comments/broken-reference` does not report references to paths `.gitignore` ignores. (#96)"
- When one PR holds changes of different groups, give each group its own line.
- Changes users cannot see (hadome's own tests, CI, dependency updates) are left out.

### Choosing the version

- If "Finds more" has at least one line, raise the minor version (`1.2.3` → `1.3.0`).
- Otherwise, raise the patch version (`1.2.3` → `1.2.4`).
- Users can then upgrade a patch version expecting no new red CI. ESLint decides the same way.

### How it reaches the release

- The `release` workflow takes the tagged version's section out of `CHANGELOG.md` and makes it the GitHub release's body. [packaging/release-notes.sh](../../packaging/release-notes.sh) does the extraction.
- The body is in English. A link to `CHANGELOG.ja.md` as of the tag is added at the end.
- If the tagged version has no section, or an empty one, the workflow fails at the first step of the build and publishes nothing. Then merge a PR that adds the section and release again as a new patch version. The tag is not moved.

To check the extraction locally:

```sh
packaging/release-notes.sh 1.2.0 CHANGELOG.md
```

## Switching an ecosystem on

Each ecosystem publishes only when its repository variable is `true` (Settings → Secrets and variables → Actions → Variables), and needs its credentials as secrets.

The secrets go in the `release` environment, not at the repository level (Settings → Environments → release). Both `release` and `pypi` allow deployments from tags `v[0-9]*` only, so a workflow running on a branch cannot read the credentials.

| Ecosystem | Variable | Secrets | Set up once |
|---|---|---|---|
| npm | `PUBLISH_NPM` | `NPM_TOKEN` | A granular access token of an account that may publish `hadome` and `hadome-<os>-<arch>` (read and write, all packages, bypassing 2FA; a token with write access expires after 90 days at most) |
| PyPI | `PUBLISH_PYPI` | none | A trusted publisher on PyPI for project `hadome`: this repository, workflow `release.yml`, environment `pypi` |
| crates.io | `PUBLISH_CRATES` | `CARGO_REGISTRY_TOKEN`, `MINISIGN_SECRET_KEY` | A crates.io token that may publish `hadome` (publish-new and publish-update). A minisign key pair that signs the archives (`minisign -G -W`; no password, because the workflow cannot type one). The secret key file's contents go into the secret, and the public key into `pubkey` in `packaging/cargo/Cargo.toml` |
| Gradle Plugin Portal | `PUBLISH_GRADLE` | `GRADLE_PUBLISH_KEY`, `GRADLE_PUBLISH_SECRET` | A Plugin Portal account; the namespace `io.github.rerorets` is verified through GitHub |
| Maven Central | `PUBLISH_MAVEN` | `CENTRAL_USERNAME`, `CENTRAL_PASSWORD`, `MAVEN_GPG_PRIVATE_KEY`, `MAVEN_GPG_PASSPHRASE` | A Central Portal user token, the namespace `io.github.rerorets` verified, and a GPG key published to a keyserver |
| Homebrew | `PUBLISH_HOMEBREW` | `HOMEBREW_TAP_TOKEN` | The repository `rerorets/homebrew-tap` (with at least one commit), and a fine-grained token allowed to write Contents there only |
| Go | — | — | Nothing: the module proxy serves the tag |

## Trying the packages before a release

```sh
packaging/smoke.sh 0.0.0-local    # SMOKE_SKIP_JVM=1 to leave out Gradle and Maven
```

It builds every package from a pretend release of this machine's Linux binary, installs each one, and checks that it prints the version and passes exit codes through. CI runs it on every push.

## Licenses of the dependencies

The distributed executables come with hadome's `LICENSE` and with `THIRD_PARTY_NOTICES`, which collects the licenses of the modules linked in.

| Channel | Where |
|---|---|
| Release archives | Next to the binary |
| npm | At the top of every package |
| PyPI | In the wheel's `dist-info/licenses/` |
| Homebrew | In the formula's install prefix |

The Gradle and Maven plugins and `cargo binstall` download the archives, so the archives carrying the files is enough.

After adding or upgrading a dependency, regenerate `THIRD_PARTY_NOTICES`. Until you do, `go test ./...` fails.

```sh
go test ./cmd/hadome -run TestThirdPartyNoticesListEveryModule -update-notices
```

- It lists the modules the release build (with cgo) links on any of Linux, macOS and Windows
- For each module it gives the version, where the source can be downloaded (the Go module proxy), and the license text the module ships
- A module with no license file at its top fails the test. Find out its license before adding it as a dependency as a dependency

## Actions the workflows use

A workflow's `uses:` names a commit SHA, not a tag. A comment after it gives the version of that SHA.

```yaml
- uses: actions/checkout@fbc6f3992d24b796d5a048ff273f7fcc4a7b6c09 # v5.1.0
```

- An action's owner can move a tag later. A SHA follows from the content, so moving the tag does not change the code hadome runs.
- The release jobs can read the publishing credentials. Pinning keeps the code of a moved tag from reading them.
- Dependabot raises the versions ([.github/dependabot.yml](../../.github/dependabot.yml)). Once a week it gathers the actions to raise into one PR. Merge it when its CI passes and the raised versions' changes look fine.
- When you add an action, write it in the same SHA-and-comment form.
