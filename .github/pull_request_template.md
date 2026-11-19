<!--
English or Japanese is fine. / 英語でも日本語でも構いません。
Do not include code, names or paths from a private repository. / 非公開のリポジトリのコード・名前・パスは入れないでください。
-->

## What changes and why / 何を、なぜ変えるか



## Checklist / 確かめたこと

- [ ] `go test ./...` passes / 通る
- [ ] `hadome check` exits 0 (and `hadome mutate`, when `.hadome.yml` has `mutation`) / exit 0 になる
- [ ] A new package or rule starts from a design document in `docs/design/` / 新しいパッケージやルールは `docs/design/` の設計書から始めた
- [ ] Changed documents are updated in both `foo.ja.md` and `foo.md`; a machine translation into Japanese is fine, and the maintainer may edit the wording / 文書は日本語と英語の両方を直した（日本語は機械翻訳でも可。文面はメンテナが手を入れることがある）
- [ ] What users will see change (output, findings, configuration) is written in "What changes and why"; no need to edit `CHANGELOG` / 利用者から見て変わること（出力・指摘・設定）を「何を、なぜ変えるか」に書いた（`CHANGELOG` は直さなくてよい）
