# Dependencies

Which of hadome's units may import which. This diagram is the source of truth: hadome checks itself against it (`rules.deps.diagram` in [.hadome.yml](../../.hadome.yml)), so an import it does not draw fails, and drawing a new arrow needs review.

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

- `facts`, `config` and `walk` import nothing of hadome's: everything else is built on them
- The engine knows no language, no rule and no process: adapters, rules and gates depend on it, never the reverse
- Rules never import adapters: they see facts only, which is why every rule works in every language
- `cmd/hadome` is the only place that puts the pieces together
- `packaging/` is the distribution: it imports nothing of the Go code, and its units (the installer and the plugins) are one box
