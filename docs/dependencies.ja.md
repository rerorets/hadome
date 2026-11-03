# 依存

hadome のどのユニットがどれを import してよいか。この図が正であり、hadome は自分自身をこれと照らして検査する（[.hadome.yml](../.hadome.yml) の `rules.deps.diagram`）。図に描かれていない import は落ち、新しい矢印を描くには人の確認が要る。

```mermaid
flowchart TB
  cmd["cmd/hadome"] --> adapter["internal/adapter"] & gate["internal/gate"] & rules["internal/rules/..."]
  cmd --> suggest["internal/suggest"] & source["internal/source"] & engine["internal/engine"] & config["internal/config"]
  adapter --> golang["internal/adapter/golang"] & treesitter["internal/adapter/treesitter"]
  adapter & golang & treesitter --> walk["internal/adapter/walk"]
  adapter & golang & treesitter --> engine & config & facts["internal/facts"]
  engine --> config & facts
  gate --> engine & config
  rules --> engine & config & facts
  suggest --> engine & config & facts
  source --> engine
  packaging["packaging/..."]
```

- `facts`、`config`、`walk` は hadome のものを何も import しない。他のすべてはこれらの上に作る
- エンジンは言語もルールもプロセスも知らない。アダプタ、ルール、門番がエンジンに依存し、逆はない
- ルールはアダプタを import しない。ルールが見るのは事実だけで、だからどのルールもどの言語でも動く
- 部品を組み合わせるのは `cmd/hadome` だけ
- `packaging/` は配布物である。Go のコードを何も import せず、そのユニット（インストーラとプラグイン）は1つの箱にまとめる
