# リリース

リリースはタグだ。それ以外はすべて `release` ワークフロー（[.github/workflows/release.yml](../../.github/workflows/release.yml)）が行う。設計は [distribution.ja.md](../design/distribution.ja.md) にある。

```sh
git tag v1.2.0
git push origin v1.2.0
```

ワークフローは6つのバイナリをネイティブにビルドし、`SHA256SUMS` と一緒に GitHub のリリースを公開し、有効にしてある各エコシステムに公開する。ワークフローを手で動かす（Actions → release → Run workflow）と、公開せずにすべてをビルドして確かめる。

公開が途中で落ちたら、原因を直してから落ちたジョブだけを再実行する。npm のジョブは、レジストリにすでにある版を飛ばす。

## エコシステムを有効にする

各エコシステムは、リポジトリの変数が `true` のときだけ公開する（Settings → Secrets and variables → Actions → Variables）。資格情報はシークレットとして要る。

シークレットはリポジトリ直下ではなく、環境 `release` に入れる（Settings → Environments → release）。環境 `release` と `pypi` は、どちらもデプロイできる参照をタグ `v[0-9]*` だけにする。こうすると、ブランチで動くワークフローは資格情報を読めない。

| エコシステム | 変数 | シークレット | 一度だけ用意するもの |
|---|---|---|---|
| npm | `PUBLISH_NPM` | `NPM_TOKEN` | `hadome` と `hadome-<os>-<arch>` を公開できるアカウントの granular access token（Read and write・All packages・2FA の迂回。書き込みのトークンは最長90日で切れる） |
| PyPI | `PUBLISH_PYPI` | 無し | PyPI のプロジェクト `hadome` の trusted publisher: このリポジトリ、ワークフロー `release.yml`、環境 `pypi` |
| crates.io | `PUBLISH_CRATES` | `CARGO_REGISTRY_TOKEN` | `hadome` を公開できる crates.io のトークン（publish-new と publish-update） |
| Gradle Plugin Portal | `PUBLISH_GRADLE` | `GRADLE_PUBLISH_KEY`、`GRADLE_PUBLISH_SECRET` | Plugin Portal のアカウント。名前空間 `io.github.rerorets` は GitHub を通して検証する |
| Maven Central | `PUBLISH_MAVEN` | `CENTRAL_USERNAME`、`CENTRAL_PASSWORD`、`MAVEN_GPG_PRIVATE_KEY`、`MAVEN_GPG_PASSPHRASE` | Central Portal のユーザートークン、検証済みの名前空間 `io.github.rerorets`、鍵サーバーに公開した GPG 鍵 |
| Homebrew | `PUBLISH_HOMEBREW` | `HOMEBREW_TAP_TOKEN` | リポジトリ `rerorets/homebrew-tap`（コミットが1つ以上あること）と、そこだけに Contents の書き込みを許した fine-grained トークン |
| Go | — | — | 無し。モジュールプロキシがタグを配る |

### 初めて公開するときの注意

- npm と crates.io の trusted publishing（トークンを使わない公開）は、パッケージが存在してからでないと設定できない。初回はトークンで公開する
- npm は、似た名前の新しいパッケージを続けて作ると、スパム検出で 403 を返すことがある。npm のサポートに解除を頼む
- Gradle Plugin Portal は、新しいプラグインの初回の公開を人が承認する。使えるようになるまで数日かかることがある
- Maven Central は、公開する POM の版を見て置き場所と照合する。ワークフローは公開の直前に、POM の `${revision}` を版に書き換える

## リリースの前にパッケージを試す

```sh
packaging/smoke.sh 0.0.0-local    # SMOKE_SKIP_JVM=1 で Gradle と Maven を外す
```

この手元の Linux バイナリから仮のリリースを作ってすべてのパッケージを作り、それぞれを入れて、版を表示することと終了コードをそのまま返すことを確かめる。CI はこれを push のたびに動かす。
