# 依存

hadome のどのユニットがどれを import してよいか。この図が正であり、hadome は自分自身をこれと照らして検査する（[.hadome.yml](../../.hadome.yml) の `rules.deps.diagram`）。図に描かれていない import は落ち、新しい矢印を描くには人の確認が要る。

```mermaid
flowchart TB
  cmd["cmd/hadome"] --> adapter["internal/adapter"] & gate["internal/gate"] & rules["internal/rules/..."]
  cmd --> suggest["internal/suggest"] & source["internal/source"] & engine["internal/engine"] & config["internal/config"]
  cmd --> mutation["internal/mutation"]
  adapter --> golang["internal/adapter/golang"] & treesitter["internal/adapter/treesitter"] & terraform["internal/adapter/terraform"] & ansible["internal/adapter/ansible"]
  adapter & golang & treesitter --> walk["internal/adapter/walk"]
  adapter & golang & treesitter & terraform & ansible --> engine & config & facts["internal/facts"]
  engine --> config & facts & messages["internal/messages"]
  gate --> engine & config
  rules --> engine & config & facts
  suggest --> engine & config & facts
  source --> engine
  mutation --> engine & config & facts
  notices["internal/notices"]
  packaging["packaging/..."]
```

- `facts`、`config`、`walk`、`messages` は hadome のものを何も import しない。他のすべてはこれらの上に作る
- エンジンは言語もルールもプロセスも知らない。アダプタ、ルール、門番がエンジンに依存し、逆はない
- ルールはアダプタを import しない。ルールが見るのは事実だけで、だからどのルールもどの言語でも動く
- 部品を組み合わせるのは `cmd/hadome` だけ
- `notices` を使うのは、`THIRD_PARTY_NOTICES` を作って確かめるテストだけである。`notices` は hadome のものを何も import せず、ほかのユニットからも import されない
- `packaging/` は配布物である。Go のコードを何も import せず、そのユニット（インストーラとプラグイン）は1つの箱にまとめる
