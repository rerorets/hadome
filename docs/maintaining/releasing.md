# Releasing

A release starts by pushing a version tag. The `release` workflow ([.github/workflows/release.yml](../../.github/workflows/release.yml)) does everything after that. [distribution.md](../design/distribution.md) explains the design.

```sh
git tag v1.2.0
git push origin v1.2.0
```

The workflow builds the six binaries natively, publishes the GitHub release with `SHA256SUMS`, then publishes to each ecosystem that is switched on. Running the workflow by hand (Actions → release → Run workflow) builds and checks everything without publishing.

If publishing fails part way, fix the cause and re-run only the failed jobs. The npm job skips versions already on the registry.

## Switching an ecosystem on

Each ecosystem publishes only when its repository variable is `true` (Settings → Secrets and variables → Actions → Variables), and needs its credentials as secrets.

The secrets go in the `release` environment, not at the repository level (Settings → Environments → release). Both `release` and `pypi` allow deployments from tags `v[0-9]*` only, so a workflow running on a branch cannot read the credentials.

| Ecosystem | Variable | Secrets | Set up once |
|---|---|---|---|
| npm | `PUBLISH_NPM` | `NPM_TOKEN` | A granular access token of an account that may publish `hadome` and `hadome-<os>-<arch>` (read and write, all packages, bypassing 2FA; a token with write access expires after 90 days at most) |
| PyPI | `PUBLISH_PYPI` | none | A trusted publisher on PyPI for project `hadome`: this repository, workflow `release.yml`, environment `pypi` |
| crates.io | `PUBLISH_CRATES` | `CARGO_REGISTRY_TOKEN` | A crates.io token that may publish `hadome` (publish-new and publish-update) |
| Gradle Plugin Portal | `PUBLISH_GRADLE` | `GRADLE_PUBLISH_KEY`, `GRADLE_PUBLISH_SECRET` | A Plugin Portal account; the namespace `io.github.rerorets` is verified through GitHub |
| Maven Central | `PUBLISH_MAVEN` | `CENTRAL_USERNAME`, `CENTRAL_PASSWORD`, `MAVEN_GPG_PRIVATE_KEY`, `MAVEN_GPG_PASSPHRASE` | A Central Portal user token, the namespace `io.github.rerorets` verified, and a GPG key published to a keyserver |
| Homebrew | `PUBLISH_HOMEBREW` | `HOMEBREW_TAP_TOKEN` | The repository `rerorets/homebrew-tap` (with at least one commit), and a fine-grained token allowed to write Contents there only |
| Go | — | — | Nothing: the module proxy serves the tag |

## Trying the packages before a release

```sh
packaging/smoke.sh 0.0.0-local    # SMOKE_SKIP_JVM=1 to leave out Gradle and Maven
```

It builds every package from a pretend release of this machine's Linux binary, installs each one, and checks that it prints the version and passes exit codes through. CI runs it on every push.
