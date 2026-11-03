# リリース

リリースはタグだ。それ以外はすべて `release` ワークフロー（[.github/workflows/release.yml](../.github/workflows/release.yml)）が行う。設計は [distribution.ja.md](design/distribution.ja.md) にある。

```sh
git tag v1.2.0
git push origin v1.2.0
```

ワークフローは6つのバイナリをネイティブにビルドし、`SHA256SUMS` と一緒に GitHub のリリースを公開し、有効にしてある各エコシステムに公開する。ワークフローを手で動かす（Actions → release → Run workflow）と、公開せずにすべてをビルドして確かめる。

## エコシステムを有効にする

各エコシステムは、リポジトリの変数が `true` のときだけ公開する（Settings → Secrets and variables → Actions → Variables）。資格情報はシークレットとして要る。

| エコシステム | 変数 | シークレット | 一度だけ用意するもの |
|---|---|---|---|
| npm | `PUBLISH_NPM` | `NPM_TOKEN` | `hadome` と `hadome-<os>-<arch>` を公開できるアカウントの npm の automation トークン |
| PyPI | `PUBLISH_PYPI` | 無し | PyPI のプロジェクト `hadome` の trusted publisher: このリポジトリ、ワークフロー `release.yml`、環境 `pypi` |
| crates.io | `PUBLISH_CRATES` | `CARGO_REGISTRY_TOKEN` | `hadome` を公開できる crates.io のトークン |
| Gradle Plugin Portal | `PUBLISH_GRADLE` | `GRADLE_PUBLISH_KEY`、`GRADLE_PUBLISH_SECRET` | Plugin Portal のアカウント。名前空間 `io.github.rerorets` は GitHub を通して検証する |
| Maven Central | `PUBLISH_MAVEN` | `CENTRAL_USERNAME`、`CENTRAL_PASSWORD`、`MAVEN_GPG_PRIVATE_KEY`、`MAVEN_GPG_PASSPHRASE` | Central Portal のユーザートークン、検証済みの名前空間 `io.github.rerorets`、鍵サーバーに公開した GPG 鍵 |
| Homebrew | `PUBLISH_HOMEBREW` | `HOMEBREW_TAP_TOKEN` | リポジトリ `rerorets/homebrew-tap` と、そこへ push できるトークン |
| Go | — | — | 無し。モジュールプロキシがタグを配る |

## リリースの前にパッケージを試す

```sh
packaging/smoke.sh 0.0.0-local    # SMOKE_SKIP_JVM=1 で Gradle と Maven を外す
```

この手元の Linux バイナリから仮のリリースを作ってすべてのパッケージを作り、それぞれを入れて、版を表示することと終了コードをそのまま返すことを確かめる。CI はこれを push のたびに動かす。
