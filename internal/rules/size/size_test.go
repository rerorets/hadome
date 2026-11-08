package size

import (
	"testing"

	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
)

// viewpoint: behaviour
// target: size.limitRule.Find
// A nested function over the complexity limit is reported under the function that holds it, at the
// nested function's line, with that line in the message; the outer function's own complexity is
// judged apart, and a nested function within the limit is not reported.
func TestNestedFunctionOverLimit(t *testing.T) {
	var rule engine.FindingRule
	for _, r := range All() {
		if r.ID() == "size/complexity" {
			rule = r.(engine.FindingRule)
		}
	}
	in := engine.Input{Config: config.Config{Size: config.Limits{"complexity": 15}}, Facts: &facts.Analysis{Symbols: []facts.Symbol{{
		ID: "example.com/app/store.Setup", Kind: facts.SymbolFunction, Path: "store/store.go", Line: 3, Complexity: 1,
		Nested: []facts.Nested{{Line: 4, Complexity: 16}, {Line: 30, Complexity: 15}},
	}}}}
	got, err := rule.Find(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d findings, want 1: %v", len(got), got)
	}
	f := got[0]
	if f.Line != 4 || f.Value != 16 || f.File != "store/store.go" {
		t.Errorf("got line %d value %d file %s, want line 4 value 16 file store/store.go", f.Line, f.Value, f.File)
	}
	if m, want := f.Message.In("en"), "the function in store.Setup at line 4: cyclomatic complexity 16, limit 15"; m != want {
		t.Errorf("got %q, want %q", m, want)
	}
	if m, want := f.Message.In("ja"), "store.Setup の 4 行目の関数: 循環的複雑度 16（上限 15）"; m != want {
		t.Errorf("got %q, want %q", m, want)
	}
}
