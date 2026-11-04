# コントリビュートの手引き

不具合の報告や要望は[Issues](https://github.com/rerorets/hadome/issues)へどうぞ。コードや文書の変更はプルリクエストで送ってください。

## 文書の地図

| 読み手 | 文書 |
|---|---|
| hadomeを使う人 | [README](README.ja.md)、[テストを信じるための3つの段](docs/model.ja.md)、[製品固有の門番](docs/external-gates.ja.md) |
| 利用者のリポジトリで動くエージェント | [エージェント向けの手引き](docs/for-agents.ja.md)。`hadome agents`が書き込むブロックの本体は`cmd/hadome/agents.ja.md` |
| hadomeを作る人 | この文書、[設計書](docs/design)、[依存の図](docs/design/dependencies.ja.md) |
| メンテナ | [公開の手順](docs/maintaining/releasing.ja.md)、[JVMのプラグイン](packaging/jvm/README.ja.md) |

## 手元で動かす

GoとCコンパイラがあれば、6言語すべてを読むビルドになります。Cコンパイラが無いとGoだけを読むビルドになり、ほかの言語のファイルは「読めなかった」と報告します。

```sh
go test ./...                           # 6言語を読むビルド（cgo あり）
CGO_ENABLED=0 go test ./...             # Go だけを読むビルド
go run ./cmd/hadome self-test           # 各ルールの自己テスト
go run ./cmd/hadome check --format json # hadome で hadome 自身を検査する
packaging/smoke.sh 0.0.0-local          # 全パッケージを作り、入れて動かす
```

CIはプルリクエストごとに、上の4つをLinux・macOS・Windowsで、最後の1つをLinuxで動かします。

### フック

一度だけ有効にします。

```sh
git config core.hooksPath .githooks
```

`.githooks/pre-commit`は、メンテナが手元に置いている非公開の語のリストを使って、コミットを止めます。リストが無ければ何もしません。

## 変更の進め方

- 新しいパッケージや、形の大きな変更は、`docs/design/`の設計書から始めます。設計書には責務、しないこと、型とインターフェースのMermaidの図、不変条件、意図して入れなかったものを書きます。設計書だけのプルリクエストで確かめてもらってから、コードを書きます。
- 新しいパッケージやimportは、[依存の図](docs/design/dependencies.ja.md)に矢印を足すところから始めます。図に無いimportはhadome自身の検査で落ちます。
- ルールを足すときは、そのルールの自己テストも足します。
- `go run ./cmd/hadome check`が終了コード`3`を返したら、何を確かめてほしいかをプルリクエストに書きます。
- プルリクエストはsquashでマージします。プルリクエストのタイトルがmainのコミットメッセージになり、リリースノートの1行にもなります。

## 書き方

- コード・コメント・コミットメッセージは英語で書きます。
- 文書は日本語の`foo.ja.md`と英語の`foo.md`の2本です。日本語が正で、日本語を先に直し、同じ変更で英語を合わせます。2本の見出しとコードブロックがそろっていないと、`cmd/hadome/docs_test.go`が落ちます。
- コメントにはコードを読んでも分からないことだけを書きます。たとえば、一般的でない書き方をした理由や、不変条件です。経緯・進捗・チケット番号は書きません。
- テストは[テストを信じるための3つの段](docs/model.ja.md)に沿って書きます。外から見える振る舞いだけを確かめ、期待値を実装からコピーせず、時刻を読まず、sleepで待たず、黙ってスキップしません。
- 公開リポジトリなので、勤め先や非公開のプロジェクトのコード・名前・パス・テーブル名を持ち込みません。例は`store.Append`や「製品A」のような一般的なものにします。

## ライセンス

hadomeは[MIT](LICENSE)ライセンスです。送ってもらった変更も同じライセンスで公開します。
