//go:build cgo

package adapter_test

import (
	"slices"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/adapter"
	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
)

// viewpoint: invariant
// target: adapter.Analyzer.AnalyzePair
// A head whose first .sql file makes a linker read the tests' strings gets its tests' strings,
// although every Kotlin file is base's, whose strings no linker read: whether a language collects
// strings is part of what its facts are remembered by. Kotlin reads only its own files, so nothing
// else it reads tells the two versions apart.
func TestTheFirstSQLFileInHeadGivesHeadItsTestsStrings(t *testing.T) {
	withSQL := fstest.MapFS{
		"kt/src/main/kotlin/Fee.kt": {Data: []byte("package shop\n\nfun fee(x: Int): Int = x + 1\n")},
		"kt/src/test/kotlin/FeeTest.kt": {Data: []byte("package shop\n\nimport kotlin.test.Test\nimport kotlin.test.assertEquals\n\n" +
			"class FeeTest {\n    @Test\n    fun fee() {\n        val q = \"SELECT billing.is_active(1)\"\n        assertEquals(2, fee(1))\n    }\n}\n")},
		"db/billing.sql": {Data: []byte("CREATE FUNCTION billing.is_active(id int) RETURNS boolean AS $$ SELECT true $$ LANGUAGE sql;\n")},
	}
	without := clone(withSQL)
	delete(without, "db/billing.sql")
	if !hasTexts(analyzeAlone(t, engine.Version{FS: withSQL}, config.Config{})) {
		t.Fatal("the fixture's tests hold no strings with a .sql file")
	}
	if hasTexts(analyzeAlone(t, engine.Version{FS: without}, config.Config{})) {
		t.Fatal("the fixture's tests hold strings without a .sql file")
	}
	samePair(t, without, withSQL, config.Config{}, config.Config{})
	samePair(t, withSQL, without, config.Config{}, config.Config{})
}

// viewpoint: behaviour
// target: adapter.Analyzer.AnalyzePair
// Between base's and head's analysis of a language, the session lets go of what head cannot use:
// the trees and facts of a file whose content head does not have. Head then holds its own tree of
// the changed file and base's tree of the unchanged one, which it was served.
func TestWhatHeadCannotUseIsLetGoBeforeHeadIsRead(t *testing.T) {
	base := fstest.MapFS{
		"web/src/cart.ts": {Data: []byte("export function total(xs: number[]): number { return xs.length }\n")},
		"web/src/tax.ts":  {Data: []byte("export function rate(x: number): number { return x * 2 }\n")},
	}
	head := clone(base)
	head["web/src/cart.ts"] = &fstest.MapFile{Data: []byte("export function total(xs: number[]): number { return xs.length + 0 }\n")}
	held, err := adapter.New().OneLanguageHeld(engine.Version{Label: "base", FS: base}, engine.Version{Label: "head", FS: head}, config.Config{}, "typescript")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"adapter/language", "treesitter/parse", "treesitter/parse"}
	if !slices.Equal(held.Remembered, want) {
		t.Errorf("the session holds %v, want %v", held.Remembered, want)
	}
	if st := held.Stats["treesitter/parse"]; st.Hits != 1 || st.Misses != 3 {
		t.Errorf("parse stats %+v, want the unchanged file served to head", st)
	}
}
