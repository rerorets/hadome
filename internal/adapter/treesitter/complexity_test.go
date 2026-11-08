//go:build cgo

package treesitter

import (
	"fmt"
	"strings"
	"testing"
)

// viewpoint: behaviour
// target: treesitter.lang.Analyze
// In every language a nested function (arrow function, function expression, lambda, closure,
// nested def, anonymous function, method of an anonymous class) is counted on its own: the
// function that holds it counts only its own branches, and each nested function, also one nested
// in another, is listed with its line and complexity.
func TestNestedComplexity(t *testing.T) {
	// Each outer function has two branches of its own (cx 3), a nested function with three (cx 4),
	// and inside that a function with one (cx 2).
	cases := []struct{ lang, path, src, want string }{
		{"typescript", "src/calc.ts", `export function outer(x: number) {
  if (x > 0) { x++; }
  if (x > 1) { x++; }
  const pick = (y: number) => {
    if (y === 0) return 0;
    if (y === 1) return 1;
    if (y === 2) return 2;
    return [y].map(function (z) { return z > 0 ? z : 0; })[0];
  };
  return pick(x);
}
`, "cx=3 nested=[{4 4} {8 2}]"},
		{"python", "app/calc.py", `def outer(x):
    if x > 0:
        x += 1
    if x > 1:
        x += 1
    def pick(y):
        if y == 0:
            return 0
        if y == 1:
            return 1
        if y == 2:
            return 2
        return (lambda z: z if z > 0 else 0)(y)
    return pick(x)
`, "cx=3 nested=[{6 4} {13 2}]"},
		{"rust", "src/lib.rs", `pub fn outer(x: i32) -> i32 {
    let mut x = x;
    if x > 0 { x += 1; }
    if x > 1 { x += 1; }
    let pick = |y: i32| {
        if y == 0 { return 0; }
        if y == 1 { return 1; }
        if y == 2 { return 2; }
        let f = |z: i32| if z > 0 { z } else { 0 };
        f(y)
    };
    pick(x)
}
`, "cx=3 nested=[{5 4} {9 2}]"},
		{"java", "src/main/java/com/acme/store/Calc.java", `package com.acme.store;

public class Calc {
    public int outer(int x) {
        if (x > 0) { x++; }
        if (x > 1) { x++; }
        java.util.function.IntUnaryOperator pick = y -> {
            if (y == 0) { return 0; }
            if (y == 1) { return 1; }
            if (y == 2) { return 2; }
            return new java.util.function.IntUnaryOperator() {
                public int applyAsInt(int z) { return z > 0 ? z : 0; }
            }.applyAsInt(y);
        };
        return pick.applyAsInt(x);
    }
}
`, "cx=3 nested=[{7 4} {12 2}]"},
		{"kotlin", "src/main/kotlin/acme/store/Calc.kt", `package acme.store

fun outer(x: Int): Int {
    var v = x
    if (v > 0) { v++ }
    if (v > 1) { v++ }
    val pick = { y: Int ->
        val f = fun(z: Int): Int { return if (z > 0) z else 0 }
        if (y == 0) 0 else if (y == 1) 1 else if (y == 2) 2 else f(y)
    }
    return pick(v)
}
`, "cx=3 nested=[{7 4} {8 2}]"},
	}
	for _, c := range cases {
		t.Run(c.lang, func(t *testing.T) {
			files := []string{c.path, c.src}
			if c.lang == "rust" {
				files = append(files, "Cargo.toml", "[package]\nname = \"app\"\n")
			}
			var ids []string
			found := false
			for _, s := range analyze(t, c.lang, tree(files...)).Symbols {
				ids = append(ids, s.ID)
				if s.ID != "outer" && !strings.HasSuffix(s.ID, ".outer") && !strings.HasSuffix(s.ID, ":outer") {
					continue
				}
				found = true
				if got := fmt.Sprintf("cx=%d nested=%v", s.Complexity, s.Nested); got != c.want {
					t.Errorf("%s: got %s, want %s", s.ID, got, c.want)
				}
			}
			if !found {
				t.Errorf("no symbol outer among %v", ids)
			}
		})
	}
}
