//go:build cgo

package treesitter

import (
	"fmt"
	"strings"
	"testing"

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
