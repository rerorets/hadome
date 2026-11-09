package mutation

import (
	"testing"

	"github.com/rerorets/hadome/internal/facts"
)

// viewpoint: behaviour
// target: the message of a surviving mutant
// A mutant that replaces code with other code says what it changed to what; one that replaces code
// with nothing, such as dropping a `!`, says it removed the code, in English and in Japanese.
func TestSurvivorMessagesSayRemovingForNothing(t *testing.T) {
	cases := []struct {
		m      facts.Mutant
		en, ja string
	}{
		{facts.Mutant{Func: "calc.Sign", Operator: "flip a comparison", Original: ">", Replacement: "<="},
			"calc.Sign: changing `>` to `<=` does not fail any test", "calc.Sign: `>` を `<=` に変えても、どのテストも落ちない"},
		{facts.Mutant{Func: "calc.Sign", Operator: "negate a boolean", Original: "!", Replacement: ""},
			"calc.Sign: removing `!` does not fail any test", "calc.Sign: `!` を消しても、どのテストも落ちない"},
	}
	for _, c := range cases {
		msg := finding(c.m, nil).Message
		if got := msg.In("en"); got != c.en {
			t.Errorf("en: got %q, want %q", got, c.en)
		}
		if got := msg.In("ja"); got != c.ja {
			t.Errorf("ja: got %q, want %q", got, c.ja)
		}
	}
}

// viewpoint: behaviour
// target: the finding for untried mutants
// The functions whose mutants were not tried are named once each, in order, whatever order the
// mutants come in.
func TestUntriedNamesEachFunctionOnceInOrder(t *testing.T) {
	var rest []facts.Mutant
	for _, fn := range []string{"calc.Zeta", "calc.Beta", "calc.Zeta", "calc.Delta", "calc.Alpha"} {
		rest = append(rest, facts.Mutant{Func: fn})
	}
	want := "5 mutants beyond mutation.max-mutants were not tried, in calc.Alpha, calc.Beta, calc.Delta, calc.Zeta"
	if got := untried(rest, nil).Message.In("en"); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}
