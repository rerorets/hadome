# Releasing

A release is a tag. Everything else is the `release` workflow ([.github/workflows/release.yml](../.github/workflows/release.yml)); [distribution.md](design/distribution.md) explains the design.

```sh
git tag v1.2.0
git push origin v1.2.0
```

The workflow builds the six binaries natively, publishes the GitHub release with `SHA256SUMS`, then publishes to each ecosystem that is switched on. Running the workflow by hand (Actions → release → Run workflow) builds and checks everything without publishing.

If publishing fails part way, fix the cause and re-run only the failed jobs. The npm job skips versions already on the registry.

## Switching an ecosystem on

Each ecosystem publishes only when its repository variable is `true` (Settings → Secrets and variables → Actions → Variables), and needs its credentials as secrets.

| Ecosystem | Variable | Secrets | Set up once |
|---|---|---|---|
| npm | `PUBLISH_NPM` | `NPM_TOKEN` | An npm automation token of an account that may publish `hadome` and `hadome-<os>-<arch>` |
| PyPI | `PUBLISH_PYPI` | none | A trusted publisher on PyPI for project `hadome`: this repository, workflow `release.yml`, environment `pypi` |
| crates.io | `PUBLISH_CRATES` | `CARGO_REGISTRY_TOKEN` | A crates.io token that may publish `hadome` |
| Gradle Plugin Portal | `PUBLISH_GRADLE` | `GRADLE_PUBLISH_KEY`, `GRADLE_PUBLISH_SECRET` | A Plugin Portal account; the namespace `io.github.rerorets` is verified through GitHub |
| Maven Central | `PUBLISH_MAVEN` | `CENTRAL_USERNAME`, `CENTRAL_PASSWORD`, `MAVEN_GPG_PRIVATE_KEY`, `MAVEN_GPG_PASSPHRASE` | A Central Portal user token, the namespace `io.github.rerorets` verified, and a GPG key published to a keyserver |
| Homebrew | `PUBLISH_HOMEBREW` | `HOMEBREW_TAP_TOKEN` | The repository `rerorets/homebrew-tap`, and a token that may push to it |
| Go | — | — | Nothing: the module proxy serves the tag |

## Trying the packages before a release

```sh
packaging/smoke.sh 0.0.0-local    # SMOKE_SKIP_JVM=1 to leave out Gradle and Maven
```

It builds every package from a pretend release of this machine's Linux binary, installs each one, and checks that it prints the version and passes exit codes through. CI runs it on every push.
