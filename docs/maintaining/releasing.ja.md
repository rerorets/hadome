# リリース

リリースはバージョンのタグを push して始めます。それ以降の作業は、すべて `release` ワークフローが行います。ワークフローの定義は [.github/workflows/release.yml](../../.github/workflows/release.yml) に、設計は [distribution.ja.md](../design/distribution.ja.md) にあります。

## リリースの手順

1. main の CI が通っていることを確かめます。
2. 前のタグからの差分で `hadome mutate` を流し、`mutation/survived` の後退が無いことを確かめます。後退があれば、テストを足す PR を先にマージします。
   - 差分が `mutation.max-mutants` を超えると、試さなかった分が確認（exit 3）として出ます。この確認は、リリースを止める理由にしません。
3. `README.md` と `README.ja.md` に書いた版を上げる PR を作り、マージします。タイトルは `The README pins 1.2.0` の形にします。
4. その PR のマージコミットに、署名つきのタグを打って push します。
5. ワークフローの結果を確かめます。落ちたジョブは、原因を直してから、そのジョブだけを再実行します。

```sh
git fetch origin
git tag -s v1.2.0 -m "hadome 1.2.0" <版を上げた PR のマージコミット>
git push origin v1.2.0
```

- タグは、版を上げた PR のマージコミットに打ちます。そうすると、タグが指すコミットの README の版と、リリースの版が一致します。タグを打つ前にほかの PR がマージされても、コミットを指定して打てば、この一致は崩れません。
- 一度 push したタグは、消したり付け替えたりしません。誤りは、次のパッチ版で直します。Go のモジュールプロキシは、最初に見たタグの中身を配り続けます。npm・PyPI・crates.io も、同じ版の中身を差し替えられません。タグを付け替えると、配り先ごとに中身が食い違います。
- リリースノートは、GitHub が前のタグからマージした PR のタイトルで作ります。そのため、PR のタイトルは、利用者が読んで変わったことが分かる1文にします。

タグは署名つきで打ちます。署名があると GitHub のタグに Verified が付き、誰が打ったタグかを確かめられます。ワークフローはタグの署名を確かめないので、署名が無くても動きは変わりません。

タグで動いたワークフローは、次のことをします。

- 6つのバイナリをネイティブにビルドする
- `SHA256SUMS` と一緒に GitHub のリリースを公開する
- 有効にしてある各エコシステムに公開する

ワークフローを手で動かすと、公開せずに、すべてをビルドして確かめます。手で動かすには、Actions → release → Run workflow を使います。

公開が途中で落ちたら、原因を直してから、落ちたジョブだけを再実行します。npm のジョブはレジストリにすでにあるバージョンを飛ばします。

## エコシステムを有効にする

各エコシステムは、リポジトリの変数が `true` のときだけ公開します。変数は Settings → Secrets and variables → Actions → Variables に置きます。資格情報はシークレットとして要ります。

シークレットはリポジトリ直下ではなく環境 `release` に入れます。環境は Settings → Environments → release にあります。環境 `release` と `pypi` は、どちらもデプロイできる参照をタグ `v[0-9]*` だけにします。こうすると、ブランチで動くワークフローは資格情報を読めません。

| エコシステム | 変数 | シークレット | 一度だけ用意するもの |
|---|---|---|---|
| npm | `PUBLISH_NPM` | `NPM_TOKEN` | `hadome` と `hadome-<os>-<arch>` を公開できるアカウントの granular access token。権限は Read and write・All packages・2FA の迂回。書き込みのトークンは最長90日で切れる |
| PyPI | `PUBLISH_PYPI` | 無し | PyPI のプロジェクト `hadome` の trusted publisher: このリポジトリ、ワークフロー `release.yml`、環境 `pypi` |
| crates.io | `PUBLISH_CRATES` | `CARGO_REGISTRY_TOKEN`、`MINISIGN_SECRET_KEY` | `hadome` を公開できる crates.io のトークン。権限は publish-new と publish-update。アーカイブに署名する minisign の鍵の組。鍵の組は `minisign -G -W` で作り、ワークフローがパスワードを入力できないので、パスワード無しにする。秘密鍵のファイルの中身をシークレットに入れ、公開鍵を `packaging/cargo/Cargo.toml` の `pubkey` に書く |
| Gradle Plugin Portal | `PUBLISH_GRADLE` | `GRADLE_PUBLISH_KEY`、`GRADLE_PUBLISH_SECRET` | Plugin Portal のアカウント。名前空間 `io.github.rerorets` は GitHub を通して検証する |
| Maven Central | `PUBLISH_MAVEN` | `CENTRAL_USERNAME`、`CENTRAL_PASSWORD`、`MAVEN_GPG_PRIVATE_KEY`、`MAVEN_GPG_PASSPHRASE` | Central Portal のユーザートークン、検証済みの名前空間 `io.github.rerorets`、鍵サーバーに公開した GPG 鍵 |
| Homebrew | `PUBLISH_HOMEBREW` | `HOMEBREW_TAP_TOKEN` | コミットが1つ以上あるリポジトリ `rerorets/homebrew-tap` と、そのリポジトリだけに Contents の書き込みを許した fine-grained トークン |
| Go | — | — | 無し。モジュールプロキシがタグを配る |

## リリースの前にパッケージを試す

```sh
packaging/smoke.sh 0.0.0-local    # SMOKE_SKIP_JVM=1 で Gradle と Maven を外す
```

`packaging/smoke.sh` は、手元の Linux バイナリから仮のリリースを作り、すべてのパッケージを作ります。そして、それぞれのパッケージを入れて、次の2つを確かめます。

- バージョンを表示すること
- 終了コードをそのまま返すこと

CI はこれを、プルリクエストのたびと、main への push のたびに動かします。

## 依存のライセンス

配る実行ファイルには次の2つを添えます。

- hadome の `LICENSE`
- リンクしたモジュールのライセンスをまとめた `THIRD_PARTY_NOTICES`

| 配り方 | 置き場所 |
|---|---|
| リリースのアーカイブ | バイナリと同じ階層 |
| npm | 各パッケージの直下 |
| PyPI | wheel の `dist-info/licenses/` |
| Homebrew | formula のインストール先（prefix） |

Gradle と Maven のプラグインと `cargo binstall` は、アーカイブを取ってきます。そのため、アーカイブに入っていれば足ります。

依存を足したり上げたりしたら、`THIRD_PARTY_NOTICES` を作り直します。作り直さないと、`go test ./...` が落ちます。

```sh
go test ./cmd/hadome -run TestThirdPartyNoticesListEveryModule -update-notices
```

- 載せるのは、cgo ありのリリースのビルドで、Linux・macOS・Windows のどれかにリンクされるモジュールです。
- 各モジュールについて、バージョン、ソースを取れる場所、モジュールに入っているライセンスの本文を載せます。ソースを取れる場所は Go のモジュールプロキシです。
- 最上位にライセンスのファイルが無いモジュールがあると、テストが落ちます。そうしたモジュールは、ライセンスを確かめてから依存に足します。

## ワークフローが使う Action

ワークフローの `uses:` は、タグではなくコミットの SHA で指定します。末尾のコメントに、その SHA の版を書きます。

```yaml
- uses: actions/checkout@fbc6f3992d24b796d5a048ff273f7fcc4a7b6c09 # v5.1.0
```

- タグは持ち主があとから付け替えられます。SHA は中身から決まるので、付け替えても hadome が動かすコードは変わりません。
- リリースのジョブは公開用の資格情報を読めます。付け替えられたタグのコードが、その資格情報を読むのを防ぎます。
- 版を上げるのは Dependabot です（[.github/dependabot.yml](../../.github/dependabot.yml)）。週に1度、上げる Action をまとめて1つの PR にします。PR の CI が通り、上げる版の変更点に問題がなければマージします。
- Action を足すときも、SHA と版のコメントの形で書きます。
