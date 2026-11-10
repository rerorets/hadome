# リリース

リリースはバージョンのタグを push して始めます。それ以降の作業は、すべて `release` ワークフローが行います。ワークフローの定義は [.github/workflows/release.yml](../../.github/workflows/release.yml) に、設計は [distribution.ja.md](../design/distribution.ja.md) にあります。

## リリースの手順

1. main の CI が通っていることを確かめます。
2. 前のタグからの差分で `hadome mutate` を流し、`mutation/survived` の後退が無いことを確かめます。後退があれば、テストを足す PR を先にマージします。
   - 差分が `mutation.max-mutants` を超えると、試さなかった分が確認（exit 3）として出ます。この確認は、リリースを止める理由にしません。
3. 手元の実際のリポジトリで、`hadome check` の CPU 時間と最大のメモリを、前の版と比べます（「[性能を比べる](#性能を比べる)」）。理由の無い後退があれば、直す PR を先にマージします。
4. 版を上げる PR を作り、マージします。タイトルは `The README pins 1.2.0` の形にします。この PR では、次の2つを変えます。
   - `README.md` と `README.ja.md` に書いた版
   - [CHANGELOG.ja.md](../../CHANGELOG.ja.md) と [CHANGELOG.md](../../CHANGELOG.md) の新しい版の節（書き方は「[変更履歴](#変更履歴)」）
5. その PR のマージコミットに、署名つきのタグを打って push します。
6. ワークフローの結果を確かめます。落ちたジョブは、原因を直してから、そのジョブだけを再実行します。
7. npm に段階公開（stage）された各パッケージを、2FA で承認します（「[npm への公開](#npm-への公開)」）。

```sh
git fetch origin
git tag -s v1.2.0 -m "hadome 1.2.0" <版を上げた PR のマージコミット>
git push origin v1.2.0
```

- タグは、版を上げた PR のマージコミットに打ちます。そうすると、タグが指すコミットの README の版と、リリースの版が一致します。タグを打つ前にほかの PR がマージされても、コミットを指定して打てば、この一致は崩れません。
- 一度 push したタグは、消したり付け替えたりしません。誤りは、次のパッチ版で直します。Go のモジュールプロキシは、最初に見たタグの中身を配り続けます。npm・PyPI・crates.io も、同じ版の中身を差し替えられません。タグを付け替えると、配り先ごとに中身が食い違います。
- GitHub のリリースの本文は、タグの版の `CHANGELOG.md` の節です。末尾に、`CHANGELOG.ja.md` へのリンクが付きます。

タグは署名つきで打ちます。署名があると GitHub のタグに Verified が付き、誰が打ったタグかを確かめられます。ワークフローはタグの署名を確かめないので、署名が無くても動きは変わりません。

タグで動いたワークフローは、次のことをします。

- 6つのバイナリをネイティブにビルドする
- `SHA256SUMS` と一緒に GitHub のリリースを公開する
- 有効にしてある各エコシステムに公開する

ワークフローを手で動かすと、公開せずに、すべてをビルドして確かめます。手で動かすには、Actions → release → Run workflow を使います。

公開が途中で落ちたら、原因を直してから、落ちたジョブだけを再実行します。npm のジョブは、公開済みのバージョンと、承認待ちで段階公開済みのバージョンを飛ばします。

## 性能を比べる

リリースの前に、前の版と main の `hadome check` を、手元の実際のリポジトリで比べます。見るのは CPU 時間（user）と最大のメモリ（最大 RSS）です。hadome の自己テストと単体テストは小さな入力しか読まないので、大きなリポジトリでの遅れやメモリの膨らみは、ここでしか分かりません。

**比べるリポジトリ**

- 言語の違うリポジトリを3つ以上使います。`.sql` のファイルを持つものを1つ入れます
- どのリポジトリも hadome には取り込みません。名前や結果も、hadome の文書や事例に書きません

**測り方**

| 測るもの | コマンド |
|---|---|
| 版の全体 | `hadome check --base-dir <空のディレクトリ> --head-dir <リポジトリ> --format json` |
| 1行の変更 | リポジトリの複製に1行だけ変えたコミットを足し、その中で `hadome check --base HEAD~1 --format json` |

- 2つのバイナリはどちらも手元でビルドします。前の版はそのタグから、新しい版は main からビルドします
- 時間とメモリは `/usr/bin/time` で測ります。macOS では `-l`、Linux では `-v` を付けます
- 前の版と新しい版を交互に、3回ずつ流します。比べるのは user の時間と最大 RSS の中央値です。経過時間（real）はほかの処理の負荷で変わるので比べません

```sh
git worktree add ../hadome-prev v1.1.0
(cd ../hadome-prev && go build -o /tmp/hadome-prev ./cmd/hadome)
go build -o /tmp/hadome-next ./cmd/hadome
mkdir -p /tmp/empty
for i in 1 2 3; do
  for b in /tmp/hadome-prev /tmp/hadome-next; do
    /usr/bin/time -l $b check --base-dir /tmp/empty --head-dir ~/src/product-a --format json > /dev/null
  done
done
```

**止める基準**

- user の時間か最大 RSS が、前の版より目に見えて増えたとき（目安は1割以上）は、その理由を探します
- 理由がこの版で足した検査（新しいルール、新しい言語、読む範囲が広がった検査）にあり、変更履歴に書いてあれば、リリースを続けます
- 理由が無いか、検査に見合わない増え方なら、リリースを止めます。原因を直す PR をマージしてから、手順をやり直します

> [!NOTE]
> この比較は PR の CI では流しません。CI は、性能の測定に要る大きなリポジトリを持たず、実行するマシンの負荷も揃わないからです。

## npm への公開

npm のジョブは、パッケージを公開せず、段階公開（`npm stage publish`）だけをします。段階公開したバージョンは、メンテナが 2FA で承認するまで誰にも見えません。

- ジョブは npm の trusted publishing（GitHub Actions の OIDC）で認証します。npm のトークンをシークレットには置きません。
- 各パッケージの trusted publisher は、段階公開だけを許します。設定は次のとおりです。

| 項目 | 値 |
|---|---|
| ユーザー | `rerorets` |
| リポジトリ | `hadome` |
| ワークフロー | `release.yml` |
| 環境 | `release` |
| Allow npm publish | オフ |
| Allow npm dist-tag | オフ |

- ジョブの中身は [packaging/npm/stage.sh](../../packaging/npm/stage.sh) です。段階公開には npm 11.15.0 以上と Node 22.14.0 以上が要るので、ジョブは版を固定した npm を入れます。
- 試しの版（`1.2.0-rc.1`）は、dist-tag `next` で段階公開します。`npm install hadome` で入る版にはなりません。
- provenance は、trusted publishing で公開すると npm が自動で付けます。

### 承認する

タグのワークフローが終わったら、メンテナが段階公開された各バージョンを 2FA で承認します。承認した時点で、そのバージョンが公開されます。

- 承認の場所は、npmjs.com の各パッケージの Staged Packages タブです。CLI なら、ログインした手元の npm で次のようにします。
- 承認は、`hadome-<os>-<arch>` をすべて済ませてから、最後に `hadome` にします。`hadome` は各プラットフォームのパッケージを `optionalDependencies` に持つので、先に公開すると入れられない版ができます。

```sh
npm stage list hadome-linux-x64
npm stage approve <stage-id>
```

### ジョブが飛ばすものと落ちるとき

ジョブはパッケージごとに次のように動きます。プラットフォームのパッケージを先に、`hadome` を最後に段階公開します。

| パッケージの状態 | ジョブの動き |
|---|---|
| そのバージョンが公開済み | 飛ばす |
| そのバージョンが段階公開済みで承認待ち | 飛ばす。段階公開し直さない |
| パッケージが npm に無い | 何も段階公開せずに落ちる。無いパッケージの名前を出す |
| それ以外の失敗 | 落ちる |

- OIDC のトークンでは `npm stage list` を使えません。そのため、承認待ちかどうかは、段階公開を断られたときのレジストリのメッセージで見分けます。
- パッケージが1つでも無いと、ジョブは何も段階公開しません。こうすると、`hadome` だけが段階公開され、プラットフォームのパッケージが欠けることがありません。

### npm のパッケージを作る

trusted publisher は、npm にすでにあるパッケージにしか設定できません。そのため、新しいパッケージは、メンテナが一度だけ手元で作ります。たとえば `hadome-win32-x64` を作るときは、次のようにします。

1. ジョブが落ちたタグのリリースからアーカイブを取り、パッケージを作ります。
2. 自分のアカウントでログインした npm で、無いパッケージを段階公開します。新しいパッケージを段階公開すると、npm は仮のバージョン `0.0.0-stage` を公開して、パッケージを作ります。
3. npmjs.com で、そのパッケージに trusted publisher を設定します。値は「[npm への公開](#npm-への公開)」の表のとおりです。
4. 手順2で段階公開したバージョンを承認します。
5. 無いパッケージがすべて揃ったら、落ちた npm のジョブを再実行します。

```sh
gh release download v1.2.0 --dir dist
packaging/extract.sh dist bins
node packaging/npm/build.mjs 1.2.0 bins npm
cd npm/hadome-win32-x64 && npm stage publish --access public --tag latest
```

- `hadome` は、プラットフォームのパッケージをすべて作ってから、最後に作ります。
- 試しの版でパッケージを作るときは、`--tag next` にします。

## 変更履歴

リリースノートは、リポジトリの `CHANGELOG.ja.md` と `CHANGELOG.md` です。GitHub の機能（PR のラベル、自動で作るリリースノート）では作りません。

### 書く人と時期

- 版を上げる PR の中で、新しい版の節を書きます。README の版と変更履歴は、同じ PR でマージします。
- 下書きは AI が作ります。材料は、前のタグから main にマージした PR のタイトルと本文です。
- 人が下書きを読んで直し、PR をマージします。

```sh
git log --first-parent --format='%s' v1.1.0..origin/main
```

- 節の見出しは `## 1.2.0 - 2026-12-01` の形にします。版、半角スペース、`-`、半角スペース、日付の順です。ワークフローは、見出しの2つ目の語が版と一致する節を探します。
- 試しの版（`1.2.0-rc.1`）にも、その版の見出しの節が要ります。

### 分け方

上げたあとに利用者の CI で何が起きるかが違うので、変更を次の4つに分けます。空の分類は書きません。

| 分類（日本語） | 分類（英語） | 何が入るか |
|---|---|---|
| 検出が増える変更 | Finds more | 新しいルール、見る範囲が広がった検査、確認（exit 3）に回すものが増えた変更。上げると、利用者の CI が赤や確認になることがある |
| 誤検知・漏れの修正 | Fewer false findings and misses | 誤って出していた指摘を出さなくなった変更、読めていなかったテストを読めるようになった変更 |
| 設定 | Configuration | `.hadome.yml` のキーの追加・変更 |
| その他 | Other | 表示、文書、配り方の変更。検査の結果は変わらない |

- 1つの変更を1行に書き、末尾に PR の番号を付けます。例: 「`comments/broken-reference` が、`.gitignore` で無視されたパスへの参照を報告しません。（#96）」
- 1つの PR に分類の違う変更が入っていれば、分類ごとに行を分けます。
- 利用者に見えない変更（hadome 自身のテスト、CI、依存の更新）は書きません。

### 版の上げ方

- 「検出が増える変更」が1行でもあれば、マイナー版を上げます（`1.2.3` → `1.3.0`）。
- それ以外は、パッチ版を上げます（`1.2.3` → `1.2.4`）。
- こうすると、利用者はパッチ版なら CI が新しく赤にならないと見込んで上げられます。ESLint と同じ決め方です。

### 公開との関係

- `release` ワークフローは、タグの版の節を `CHANGELOG.md` から抜き出して、GitHub のリリースの本文にします。抜き出すのは [packaging/release-notes.sh](../../packaging/release-notes.sh) です。
- 本文は英語です。末尾に、タグの時点の `CHANGELOG.ja.md` へのリンクを足します。
- タグの版の節が無いか空なら、ワークフローはビルドの最初の段で落ち、何も公開しません。落ちたら、節を足す PR をマージし、新しいパッチ版でリリースし直します。タグは付け替えません。

手元で抜き出しを確かめるには、次のようにします。

```sh
packaging/release-notes.sh 1.2.0 CHANGELOG.md
```

## エコシステムを有効にする

各エコシステムは、リポジトリの変数が `true` のときだけ公開します。変数は Settings → Secrets and variables → Actions → Variables に置きます。資格情報はシークレットとして要ります。

シークレットはリポジトリ直下ではなく環境 `release` に入れます。環境は Settings → Environments → release にあります。環境 `release` と `pypi` は、どちらもデプロイできる参照をタグ `v[0-9]*` だけにします。こうすると、ブランチで動くワークフローは資格情報を読めません。

| エコシステム | 変数 | シークレット | 一度だけ用意するもの |
|---|---|---|---|
| npm | `PUBLISH_NPM` | 無し | `hadome` と `hadome-<os>-<arch>` の各パッケージの trusted publisher（「[npm への公開](#npm-への公開)」）。npm に無いパッケージは、先に手元で作る |
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
