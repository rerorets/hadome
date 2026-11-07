//go:build cgo

package treesitter

import (
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
)

// mutantsOf runs one language's mutator over one file, for the functions declared on the lines
// that hold each of names, and renders each mutant as "line kind: original -> replacement".
func mutantsOf(t *testing.T, name, path, src string, names ...string) ([]string, []facts.Mutant) {
	t.Helper()
	var l *lang
	for _, c := range Languages() {
		if c.name == name {
			l = c
		}
	}
	var funcs []facts.Symbol
	for _, n := range names {
		for i, text := range strings.Split(src, "\n") {
			if strings.Contains(text, " "+n+"(") {
				funcs = append(funcs, facts.Symbol{ID: "calc." + n, Kind: facts.SymbolFunction, Path: path, Line: i + 1})
				break
			}
		}
	}
	ms, err := l.Mutants(engine.Version{FS: tree(path, src)}, funcs)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, m := range ms {
		out = append(out, fmt.Sprintf("%d %s: %s -> %s", m.Line, m.Operator, m.Original, m.Replacement))
	}
	return out, ms
}

// viewpoint: behaviour
// target: lang.Mutants
// The five kinds of break in each language, spelled as the design's table says: only in the
// functions asked for, not inside a nested function, with the language's zero values, and with no
// + swapped next to a string where + joins strings.
func TestMutantsOfEachLanguage(t *testing.T) {
	cases := []struct {
		lang, path, src string
		want            []string
	}{
		{"typescript", "src/calc.ts", `export function add(a: number, b: number): number {
  if (a < 0 && !ok()) {
    log();
    return a + b;
  }
  const g = () => a > b;
  return 0;
}
function name(): string { return "x" + "y"; }
function yes(): boolean | null { return true; }
function other(a: number) { return a < 1; }
`, []string{
			"2 flip a comparison: < -> >=",
			"2 negate a boolean: ! -> ",
			"3 remove a call statement: log(); -> ;",
			"4 return the zero value: a + b -> 0",
			"4 swap arithmetic: + -> -",
			`9 return the zero value: "x" + "y" -> ""`,
			"10 negate a boolean: true -> false",
		}},
		{"python", "src/calc.py", `def add(a, b) -> int:
    if a < 0 and not ok():
        log()
        return a + b
    g = lambda: a > b
    return 0


def name() -> str:
    return "x" + "y"


def yes() -> Optional[bool]:
    return True


def other(a):
    return a < 1
`, []string{
			"2 flip a comparison: < -> >=",
			"2 negate a boolean: not  -> ",
			"3 remove a call statement: log() -> pass",
			"4 return the zero value: a + b -> 0",
			"4 swap arithmetic: + -> -",
			`10 return the zero value: "x" + "y" -> ""`,
			"14 negate a boolean: True -> False",
		}},
		{"rust", "src/calc.rs", `fn add(a: i32, b: i32) -> i32 {
    if a < 0 && !ok() {
        log();
        return a + b;
    }
    let g = |x: i32| x > b;
    a * b
}
fn name() -> String { "x".to_string() + "y" }
fn yes() -> bool { true }
fn other(a: i32) -> bool { a < 1 }
`, []string{
			"2 flip a comparison: < -> >=",
			"2 negate a boolean: ! -> ",
			"3 remove a call statement: log(); -> ;",
			"4 return the zero value: a + b -> 0",
			"4 swap arithmetic: + -> -",
			"7 swap arithmetic: * -> /",
			"7 return the zero value: a * b -> 0",
			"9 swap arithmetic: + -> -",
			`9 return the zero value: "x".to_string() + "y" -> String::new()`,
			"10 negate a boolean: true -> false",
		}},
		{"java", "src/main/java/calc/Calc.java", `class Calc {
    int add(int a, int b) {
        if (a < 0 && !ok()) {
            log();
            return a + b;
        }
        Runnable g = () -> System.out.println(a > b);
        return 0;
    }
    String name() { return "x" + "y"; }
    boolean yes() { return true; }
    boolean other(int a) { return a < 1; }
}
`, []string{
			"3 flip a comparison: < -> >=",
			"3 negate a boolean: ! -> ",
			"4 remove a call statement: log(); -> ;",
			"5 return the zero value: a + b -> 0",
			"5 swap arithmetic: + -> -",
			`10 return the zero value: "x" + "y" -> ""`,
			"11 negate a boolean: true -> false",
		}},
		{"kotlin", "src/main/kotlin/calc/Calc.kt", `fun add(a: Int, b: Int): Int {
    if (a < 0 && !ok()) {
        log()
        return a + b
    }
    val g = { a > b }
    return 0
}
fun name(): String { return "x" + "y" }
fun yes(): Boolean? { return true }
fun other(a: Int) = a < 1
`, []string{
			"2 flip a comparison: < -> >=",
			"2 negate a boolean: ! -> ",
			"3 remove a call statement: log() -> Unit",
			"4 return the zero value: a + b -> 0",
			"4 swap arithmetic: + -> -",
			`9 return the zero value: "x" + "y" -> ""`,
			"10 negate a boolean: true -> false",
		}},
	}
	for _, c := range cases {
		t.Run(c.lang, func(t *testing.T) {
			got, _ := mutantsOf(t, c.lang, c.path, c.src, "add", "name", "yes")
			expect(t, strings.Join(got, "\n"), strings.Join(c.want, "\n"))
		})
	}
}

// viewpoint: behaviour
// target: lang.Mutants
// An anchor holds no line number and no comment: moving the function down and commenting one of
// its lines leaves every anchor as it was.
func TestMutantAnchorsSurviveMovingAndCommenting(t *testing.T) {
	cases := []struct{ lang, path, comment, src string }{
		{"typescript", "a.ts", "//", "function add(a: number): number {\n  log();\n  return a + 1;\n}\n"},
		{"python", "a.py", "#", "def add(a) -> int:\n    log()\n    return a + 1\n"},
		{"rust", "src/a.rs", "//", "fn add(a: i32) -> i32 {\n    log();\n    a + 1\n}\n"},
		{"java", "A.java", "//", "class A {\n  int add(int a) {\n    log();\n    return a + 1;\n  }\n}\n"},
		{"kotlin", "A.kt", "//", "fun add(a: Int): Int {\n    log()\n    return a + 1\n}\n"},
	}
	for _, c := range cases {
		t.Run(c.lang, func(t *testing.T) {
			_, before := mutantsOf(t, c.lang, c.path, c.src, "add")
			lines := strings.Split(c.src, "\n")
			for i, l := range lines {
				if strings.Contains(l, "log()") {
					lines[i] = l + "  " + c.comment + " note"
				}
			}
			moved := c.comment + " header\n\n" + strings.Join(lines, "\n")
			_, after := mutantsOf(t, c.lang, c.path, moved, "add")
			if len(before) == 0 || len(before) != len(after) {
				t.Fatalf("got %d mutants before and %d after", len(before), len(after))
			}
			for i := range before {
				if before[i].Anchor != after[i].Anchor {
					t.Errorf("anchor changed: %q -> %q", before[i].Anchor, after[i].Anchor)
				}
				if after[i].Line != before[i].Line+2 {
					t.Errorf("line: got %d, want %d", after[i].Line, before[i].Line+2)
				}
			}
		})
	}
}

// viewpoint: behaviour
// target: lang.Mutants
// Only functions and methods are broken, never a type declared on the same line as one. Mutants
// come in the order of the files' paths, then of the functions' lines, whatever order the symbols
// are given in, so two runs over the same change try the same mutants.
func TestMutantsBreakFunctionsAndMethodsInPathAndLineOrder(t *testing.T) {
	var l *lang
	for _, c := range Languages() {
		if c.name == "typescript" {
			l = c
		}
	}
	files := tree(
		"src/b.ts", "export function late(a: number) { return a < 1 }\n\nexport function early(a: number) { return a > 1 }\n",
		"src/a.ts", "export class Box { open(a: number) { return a <= 1 } }\n",
	)
	funcs := []facts.Symbol{
		{ID: "b.early", Kind: facts.SymbolFunction, Path: "src/b.ts", Line: 3},
		{ID: "a.Box", Kind: facts.SymbolType, Path: "src/a.ts", Line: 1},
		{ID: "b.late", Kind: facts.SymbolFunction, Path: "src/b.ts", Line: 1},
		{ID: "a.Box.open", Kind: facts.SymbolMethod, Path: "src/a.ts", Line: 1},
	}
	// More files than two, so that an order left to chance almost never comes out sorted.
	var more []string
	for _, c := range "zyxwvutsrqponmlkjihgfedc" {
		p := "src/m" + string(c) + ".ts"
		files[p] = &fstest.MapFile{Data: []byte("export function f(a: number) { return a >= 1 }\n")}
		funcs = append(funcs, facts.Symbol{ID: "m" + string(c) + ".f", Kind: facts.SymbolFunction, Path: p, Line: 1})
	}
	for _, c := range "cdefghijklmnopqrstuvwxyz" {
		more = append(more, "m"+string(c)+".f flip a comparison: >= -> <")
	}
	ms, err := l.Mutants(engine.Version{FS: files}, funcs)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, m := range ms {
		got = append(got, fmt.Sprintf("%s %s: %s -> %s", m.Func, m.Operator, m.Original, m.Replacement))
	}
	expect(t, strings.Join(got, "\n"), `
a.Box.open flip a comparison: <= -> >
b.late flip a comparison: < -> >=
b.early flip a comparison: > -> <=
`+strings.Join(more, "\n"))
}

// viewpoint: behaviour
// target: lang.Mutants
// Returning the zero value in place of a constant of the same file whose value is already that
// zero value changes nothing, and no test could catch it: no such mutant is made. Everything else
// is still broken: a constant with another value, a name that can be reassigned, a name declared
// twice with different values or once in a way hadome does not read, a parameter or local of the
// same name, and (in Python, where 0 and 0.0 differ) an integer returned where a float is declared.
func TestMutantsSkipConstantsAlreadyZero(t *testing.T) {
	cases := []struct {
		lang, path, src string
		want            []string
	}{
		{"typescript", "src/calc.ts", `export const OK = 0;
const EMPTY = '';
const HEX = 0x0;
const NONE = null;
const FAILED = 1;
let count = 0;
export function done(): number { return OK; }
export function label(): string { return EMPTY; }
export function hex(): number { return HEX; }
export function nothing(): string | null { return NONE; }
export function fail(): number { return FAILED; }
export function counted(): number { return count; }
export function shadow(OK: number): number { return OK; }
export function local(): string { const EMPTY = 'x'; return EMPTY; }
`, []string{
			"fail: FAILED -> 0",
			"counted: count -> 0",
			"shadow: OK -> 0",
			`local: EMPTY -> ""`,
		}},
		{"python", "src/calc.py", `OK = 0
EMPTY = ""
NOTHING = None
RATE = 0
FAILED = 1
COUNT = 0
COUNT = 1
MOVED = 0


def bump():
    global MOVED
    MOVED = 2


def done() -> int:
    return OK


def label() -> str:
    return EMPTY


def nothing() -> Optional[int]:
    return NOTHING


def rate() -> float:
    return RATE


def fail() -> int:
    return FAILED


def counted() -> int:
    return COUNT


def moved() -> int:
    return MOVED


def shadow(OK) -> int:
    return OK


def local() -> int:
    OK = 3
    return OK
`, []string{
			"rate: RATE -> 0.0",
			"fail: FAILED -> 0",
			"counted: COUNT -> 0",
			"moved: MOVED -> 0",
			"shadow: OK -> 0",
			"local: OK -> 0",
		}},
		{"rust", "src/calc.rs", `const OK: i32 = 0;
static EMPTY: &str = "";
const WIDE: u64 = 0_u64;
static mut COUNT: i32 = 0;
const FAILED: i32 = 1;
mod inner {
    pub const TWICE: i32 = 1;
}
const TWICE: i32 = 0;
fn done() -> i32 { OK }
fn label() -> &'static str { return EMPTY; }
fn wide() -> u64 { WIDE }
fn counted() -> i32 { return COUNT; }
fn fail() -> i32 { FAILED }
fn twice() -> i32 { TWICE }
fn shadow(OK: i32) -> i32 { OK }
fn local() -> i32 { let OK = 2; OK }
`, []string{
			"counted: COUNT -> 0",
			"fail: FAILED -> 0",
			"twice: TWICE -> 0",
			"shadow: OK -> 0",
			"local: OK -> 0",
		}},
		{"java", "src/main/java/calc/Calc.java", `class Calc {
    static final int OK = 0;
    static final String EMPTY = "";
    static final long BIG = 0L;
    final Object none = null;
    static final int FAILED = 1;
    static int count = 0;
    static final int TWICE = 0;
    static class Inner { static final int TWICE = 1; }
    int done() { return OK; }
    String label() { return EMPTY; }
    long big() { return BIG; }
    Object nothing() { return none; }
    int fail() { return FAILED; }
    int counted() { return count; }
    int twice() { return TWICE; }
    int shadow(int OK) { return OK; }
    int local() { int OK = 2; return OK; }
}
`, []string{
			"fail: FAILED -> 0",
			"counted: count -> 0",
			"twice: TWICE -> 0",
			"shadow: OK -> 0",
			"local: OK -> 0",
		}},
		{"kotlin", "src/main/kotlin/calc/Calc.kt", `const val OK = 0
val EMPTY = ""
val BIG = 0L
const val FAILED = 1
var count = 0
const val LIMIT = 0
open class Base {
    open val LIMIT = 0
}
fun done(): Int { return OK }
fun label(): String { return EMPTY }
fun big(): Long { return BIG }
fun fail(): Int { return FAILED }
fun counted(): Int { return count }
fun limit(): Int { return LIMIT }
fun shadow(OK: Int): Int { return OK }
fun local(): Int {
    val OK = 2
    return OK
}
`, []string{
			"fail: FAILED -> 0",
			"counted: count -> 0",
			"limit: LIMIT -> 0",
			"shadow: OK -> 0",
			"local: OK -> 0",
		}},
	}
	names := []string{"done", "label", "hex", "wide", "big", "nothing", "rate", "fail", "counted", "moved", "twice",
		"limit", "shadow", "local"}
	runZeroConstantCases(t, cases, names)
}

// viewpoint: behaviour
// target: lang.Mutants
// A number constant is zero however its literal spells zero: a fraction, an exponent, another
// base, underscores, a type suffix. A literal whose value is not zero, or that is not a number of
// the declared type (a TypeScript bigint, a Python complex), is broken.
func TestMutantsReadZeroInEverySpellingOfANumber(t *testing.T) {
	source := func(declare, function string, spellings []string) string {
		var b strings.Builder
		for i, s := range spellings {
			fmt.Fprintf(&b, declare+"\n", i, s)
		}
		for i := range spellings {
			fmt.Fprintf(&b, function+"\n", i, i)
		}
		return b.String()
	}
	ts := []string{"0.0", ".0", "0e5", "0E+2", "0b0", "0o0", "0x00", "0.0e-3", "0x10", "0n", "1e0", "10", "0.5"}
	py := []string{"0.", "0e0", "0.0", "0j", "1.0", "0"}
	rust := []string{"0f64", "0_0.0", "0.0e0f64", "1f64"}
	kt := []string{"0L", "0x0L", "0b0L", "1L"}
	cases := []struct {
		lang, path, src string
		want            []string
	}{
		{"typescript", "src/calc.ts", source("const C%d = %s;", "function f%d(): number { return C%d; }", ts), []string{
			"f8: C8 -> 0", "f9: C9 -> 0", "f10: C10 -> 0", "f11: C11 -> 0", "f12: C12 -> 0",
		}},
		{"python", "src/calc.py", source("C%d = %s", "def f%d() -> float:\n    return C%d\n", py), []string{
			"f3: C3 -> 0.0", "f4: C4 -> 0.0", "f5: C5 -> 0.0",
		}},
		{"rust", "src/calc.rs", source("const C%d: f64 = %s;", "fn f%d() -> f64 { C%d }", rust), []string{
			"f3: C3 -> 0.0",
		}},
		{"kotlin", "src/main/kotlin/calc/Calc.kt", source("const val C%d = %s", "fun f%d(): Long { return C%d }", kt),
			[]string{"f3: C3 -> 0L"}},
	}
	var names []string
	for i := range ts {
		names = append(names, fmt.Sprintf("f%d", i))
	}
	runZeroConstantCases(t, cases, names)
}

func runZeroConstantCases(t *testing.T, cases []struct {
	lang, path, src string
	want            []string
}, names []string) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.lang, func(t *testing.T) {
			_, ms := mutantsOf(t, c.lang, c.path, c.src, names...)
			var got []string
			for _, m := range ms {
				if m.Operator == returnZero {
					got = append(got, fmt.Sprintf("%s: %s -> %s", strings.TrimPrefix(m.Func, "calc."), m.Original, m.Replacement))
				}
			}
			expect(t, strings.Join(got, "\n"), strings.Join(c.want, "\n"))
		})
	}
}
