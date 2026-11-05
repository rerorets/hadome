# Dependencies

Which of hadome's units may import which. This diagram is the source of truth: hadome checks itself against it (`rules.deps.diagram` in [.hadome.yml](../../.hadome.yml)), so an import it does not draw fails, and drawing a new arrow needs review.

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

- `facts`, `config`, `walk` and `messages` import nothing of hadome's: everything else is built on them
- The engine knows no language, no rule and no process: adapters, rules and gates depend on it, never the reverse
- Rules never import adapters: they see facts only, which is why every rule works in every language
- `cmd/hadome` is the only place that puts the pieces together
- Only the test that writes and checks `THIRD_PARTY_NOTICES` uses `notices`. `notices` imports nothing of hadome, and no other unit imports it
- `packaging/` is the distribution: it imports nothing of the Go code, and its units (the installer and the plugins) are one box
