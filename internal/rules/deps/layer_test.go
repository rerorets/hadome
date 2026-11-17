package deps_test

import (
	"testing"

	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/rules/deps"
)

// viewpoint: external contract
// target: deps/layer
// A finding names the two directories and the two layers, so that a reader sees which way the
// import goes against the layers without opening the configuration.
func TestALayerFindingNamesBothLayers(t *testing.T) {
	var rule engine.FindingRule
	for _, r := range deps.All() {
		if r.ID() == "deps/layer" {
			rule = r.(engine.FindingRule)
		}
	}
	in := engine.Input{
		Config: config.Config{ArchLayers: []config.ArchLayer{{Name: "domain", Paths: []string{"domain/**"}},
			{Name: "usecase", Paths: []string{"usecase/**"}}, {Name: "adapters", Paths: []string{"infra/**"}}}},
		Facts: units([]string{"domain/user", "infra/db"}, imp("domain/user", "infra/db", "domain/user/f.go", 4)),
	}
	got, err := rule.Find(in)
	if err != nil {
		t.Fatal(err)
	}
	want := "import from an inner layer: domain/user → infra/db (layer domain imports the outer layer adapters)"
	if len(got) != 1 || got[0].Message.In("en") != want || got[0].Line != 4 {
		t.Errorf("want one finding on line 4 saying %q, got %+v", want, got)
	}
	if none, _ := rule.Find(engine.Input{Facts: in.Facts}); len(none) != 0 {
		t.Errorf("want nothing without layers, got %+v", none)
	}
}
