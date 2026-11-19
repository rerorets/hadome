<!--
English or Japanese is fine. / 英語でも日本語でも構いません。
Do not include code, names or paths from a private repository. / 非公開のリポジトリのコード・名前・パスは入れないでください。
-->

## What changes and why / 何を、なぜ変えるか



## Checklist / 確かめたこと

- [ ] `go test ./...` passes / 通る
- [ ] `hadome check` exits 0 (and `hadome mutate`, when `.hadome.yml` has `mutation`) / exit 0 になる
- [ ] A new package or rule starts from a design document in `docs/design/` / 新しいパッケージやルールは `docs/design/` の設計書から始めた
- [ ] Changed documents are updated in both `foo.ja.md` and `foo.md`, Japanese first / 文書は日本語を先に直し、英語も揃えた
- [ ] User-visible changes are described above; the maintainer writes `CHANGELOG` when the version goes up / 利用者に見える変更は上に書いた（`CHANGELOG` はメンテナが版を上げるときに書く）
