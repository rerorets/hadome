package golang

import (
	"strings"
	"testing"

	"github.com/rerorets/hadome/internal/config"
)

// viewpoint: behaviour
// target: golang.walker.stringExpr
// A joined string is at its first literal's line, with one hole for values joined next to each
// other and the holes of a joined variable moved to where the variable is joined. Numbers joined
// with + are no string. A chain of constants is followed only as far as the bound on chains.
func TestGoJoinedStrings(t *testing.T) {
	var chain strings.Builder
	chain.WriteString("const c0 = \"SELECT deep\"\n")
	for i := 1; i <= 20; i++ {
		chain.WriteString("const c" + itoa(i) + " = c" + itoa(i-1) + "\n")
	}
	got := textsOf(t, config.Analysis{}, textsProduct, `package store

import "testing"

func run(sql string) {}

var tail = "WHERE id = " + id

var id = idOf()

func idOf() string { return "" }

const short2 = short1
const short1 = "SELECT short"

`+chain.String()+`
func TestJoins(t *testing.T) {
	a, b := idOf(), idOf()
	run(a +
		"SELECT 1 " + a + b + "FROM t")
	run("SELECT 2 " + tail)
	_ = 1 + 2
	run(c20)
	run(short2)
}
`)
	var mine []string
	for _, g := range got {
		if !strings.Contains(g, "store_test.go:11 ") { // the empty string idOf returns
			mine = append(mine, g)
		}
	}
	checkTexts(t, mine, []string{
		`TestJoins store/store_test.go:14 "SELECT short" holes=[] sink= dynamic=false ×1`,
		`TestJoins store/store_test.go:41 "SELECT 1 FROM t" holes=[0 9] sink= dynamic=false ×1`,
		`TestJoins store/store_test.go:42 "SELECT 2 WHERE id = " holes=[20] sink= dynamic=false ×1`,
	})
}

func itoa(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return string(rune('0'+i/10)) + string(rune('0'+i%10))
}
