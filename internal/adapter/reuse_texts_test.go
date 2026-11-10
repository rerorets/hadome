//go:build cgo

package adapter_test

import (
	"testing"
	"testing/fstest"

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
