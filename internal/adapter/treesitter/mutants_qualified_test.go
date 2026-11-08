//go:build cgo

package treesitter

import (
	"testing"
)

// viewpoint: behaviour
// target: hadome mutate's mutants
// A function that returns a constant through the class or object that declares it in the same file
// (Defaults.ZERO), whose value is already the zero value, gets no return-zero mutant: the mutant would
// change nothing. A constant that is not zero, or one of a class this file does not declare, still
// gets it.
func TestMutantsSkipQualifiedConstantsAlreadyZero(t *testing.T) {
	cases := []struct {
		lang, path, src string
		want            []string
	}{
		{"kotlin", "src/main/kotlin/calc/Calc.kt", `package calc

object Defaults {
    const val ZERO = 0
    const val ONE = 1
}

class Store {
    companion object {
        const val NONE = 0
    }
}

fun zero(): Int { return Defaults.ZERO }
fun one(): Int { return Defaults.ONE }
fun none(): Int { return Store.NONE }
fun foreign(): Int { return Imported.ZERO }
fun shadowed(Defaults: Limits): Int { return Defaults.ZERO }
`, []string{
			"one: Defaults.ONE -> 0",
			"foreign: Imported.ZERO -> 0",
			"shadowed: Defaults.ZERO -> 0",
		}},
		{"java", "src/main/java/calc/Calc.java", `class Calc {
    static final class Defaults {
        static final int ZERO = 0;
        static final int ONE = 1;
    }
    int zero() { return Defaults.ZERO; }
    int one() { return Defaults.ONE; }
    int foreign() { return Imported.ZERO; }
    int shadowed(Limits Defaults) { return Defaults.ZERO; }
}
`, []string{
			"one: Defaults.ONE -> 0",
			"foreign: Imported.ZERO -> 0",
			"shadowed: Defaults.ZERO -> 0",
		}},
	}
	runZeroConstantCases(t, cases, []string{"zero", "one", "none", "foreign", "shadowed"})
}
