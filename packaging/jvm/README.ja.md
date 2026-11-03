# JVM のプラグイン

Gradle のプラグイン（`io.github.rerorets.hadome`）と Maven のプラグイン（`io.github.rerorets:hadome-maven-plugin`）は、`installer/` のインストーラを共有する。インストーラはプラットフォームに合うリリースのアーカイブをダウンロードし、リリースの `SHA256SUMS` と照らし合わせ、バイナリをビルドツールのキャッシュに置く。

版とチェックサムは、プラグインをビルドする時点で固定する。リリースをビルドする前に、それらを `release/`（git では無視される）に書き込む。

```sh
packaging/jvm/release.sh <version> <path to the release's SHA256SUMS>
```

その後、`gradle-plugin/` で `gradle build -PhadomeVersion=<version>`、`maven-plugin/` で `mvn package -Drevision=<version>` を実行する。`release/` なしでビルドしたプラグインは、何もインストールしない。

`-Dhadome.downloadBaseUrl=<url>` を付けると、インストーラは GitHub の代わりにミラー（またはローカルのディレクトリ、`file:///…`）からダウンロードする。チェックサムの照合はそのまま効く。
