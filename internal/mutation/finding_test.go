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
		msg := finding(c.m).Message
		if got := msg.In("en"); got != c.en {
			t.Errorf("en: got %q, want %q", got, c.en)
		}
		if got := msg.In("ja"); got != c.ja {
			t.Errorf("ja: got %q, want %q", got, c.ja)
		}
	}
}
