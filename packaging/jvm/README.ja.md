# JVM のプラグイン

JVM 向けのプラグインは Gradle と Maven の2つです。

| ビルドツール | プラグイン |
|---|---|
| Gradle | `io.github.rerorets.hadome` |
| Maven | `io.github.rerorets:hadome-maven-plugin` |

2つのプラグインは `installer/` のインストーラを共有します。インストーラは次の順に動きます。

1. プラットフォームに合うリリースのアーカイブをダウンロードする
2. アーカイブをリリースの `SHA256SUMS` と照らし合わせる
3. バイナリをビルドツールのキャッシュに置く

バージョンとチェックサムは、プラグインをビルドする時点で固定します。リリースをビルドする前に、次のコマンドでそれらを `release/` に書き込みます。`release/` は git では無視されます。

```sh
packaging/jvm/release.sh <version> <path to the release's SHA256SUMS>
```

そのあと、それぞれのディレクトリでプラグインをビルドします。

- `gradle-plugin/` で `gradle build -PhadomeVersion=<version>` を実行する
- `maven-plugin/` で `mvn package -Drevision=<version>` を実行する

`release/` なしでビルドしたプラグインは、何もインストールしません。

`-Dhadome.downloadBaseUrl=<url>` を付けると、インストーラは GitHub の代わりに `<url>` からダウンロードします。`<url>` には、ミラーか、ローカルのディレクトリ（`file:///…` の形）を指定できます。この場合も、チェックサムの照合は効きます。
