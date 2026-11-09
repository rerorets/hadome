package comments_test

import (
	"slices"
	"testing"

	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
	"github.com/rerorets/hadome/internal/rules/comments"
)

// viewpoint: external contract
// target: comments/missing-doc
// An undocumented value lacks a description, an undocumented function a doc comment, and a database
// object is asked for neither.
func TestMissingDocNamesWhatIsMissing(t *testing.T) {
	var rule engine.FindingRule
	for _, r := range comments.All() {
		if r.ID() == "comments/missing-doc" {
			rule = r.(engine.FindingRule)
		}
	}
	got, err := rule.Find(engine.Input{Facts: &facts.Analysis{Symbols: []facts.Symbol{
		{ID: "infra.region", Kind: facts.SymbolValue, Path: "infra/variables.tf", Line: 1, Exported: true},
		{ID: "example.com/app/store.Load", Kind: facts.SymbolFunction, Path: "store/load.go", Line: 3, Exported: true},
		{ID: ".sql/public.accounts", Kind: facts.SymbolRelation, Path: "db/a.sql", Line: 1, Exported: true},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	var messages []string
	for _, f := range got {
		messages = append(messages, f.Message.In("en"))
	}
	want := []string{"value infra.region has no description", "exported function store.Load has no doc comment"}
	if !slices.Equal(messages, want) {
		t.Errorf("messages %q, want %q", messages, want)
	}
}
