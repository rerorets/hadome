// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

//go:build cgo

package treesitter

import (
	"testing"

	"github.com/rerorets/hadome/internal/facts"
)

// viewpoint: behaviour
// target: the Kotlin reader
// The facts rules see from a Gradle-style project: packages as units, default-public declarations,
// top-level functions, and JUnit tests with their skips, MockK mocks and calls.
func TestKotlinFacts(t *testing.T) {
	a := analyze(t, "kotlin", tree(
		"src/main/kotlin/acme/store/Store.kt", `package acme.store

import java.io.File

/** Store keeps rows. */
class Store {
    fun add(row: String) {
        if (row.isNotEmpty() && row.length > 1) { rows.add(row) }
    }
    private fun hidden() {}
}

internal interface Repo {
    fun get()
    fun put()
}

fun createPost(userId: String, body: String): String = body
`,
		"src/test/kotlin/acme/store/StoreTest.kt", `package acme.store

import io.mockk.mockk
import org.junit.jupiter.api.Disabled
import org.junit.jupiter.api.Test

class StoreTest {
    @Test
    fun createsTwice() {
        createPost("u1", "a")
        createPost("u2", "a")
        val s = Store()
        s.add("x")
        Thread.sleep(10)
    }

    @Disabled("needs a database")
    @Test
    fun later() {
        for (i in 1..2) { createPost("u1", "b") }
        val thing = make()
        thing.run()
    }
}
`))
	expect(t, summary(a), `
file src/main/kotlin/acme/store/Store.kt source unit=acme.store at=src/main/kotlin/acme/store
file src/test/kotlin/acme/store/StoreTest.kt test unit=acme.store at=src/test/kotlin/acme/store
import acme.store -> java.io internal=false io=true
symbol function acme.store.createPost exported=true doc=false params=2 cx=1 methods=0 scoped=true
symbol interface acme.store.Repo exported=false doc=false params=0 cx=0 methods=2 scoped=false
symbol method acme.store.Repo.get exported=false doc=false params=0 cx=1 methods=0 scoped=false
symbol method acme.store.Repo.put exported=false doc=false params=0 cx=1 methods=0 scoped=false
symbol method acme.store.Store.add exported=true doc=false params=1 cx=3 methods=0 scoped=false
symbol method acme.store.Store.hidden exported=false doc=false params=0 cx=1 methods=0 scoped=false
symbol type acme.store.Store exported=true doc=true params=0 cx=0 methods=0 scoped=false
test "StoreTest.createsTwice" calls=[acme.store.Store×1/0 acme.store.Store.add×1/1 acme.store.createPost×2/2] hazards=[mock sleep] skip=false:"" unresolved=0
test "StoreTest.later" calls=[acme.store.createPost×2/1] hazards=[mock] skip=true:"needs a database" unresolved=1
`)
}

// viewpoint: behaviour
// target: the Kotlin reader
// Kotlin Multiplatform puts tests in a test or test@<platform> directory beside the sources, outside
// src/; such files are tests. A test directory inside src/ may be a package and stays product code.
func TestKotlinMultiplatformTestDirectoriesHoldTests(t *testing.T) {
	cls := func(pkg string) string { return "package " + pkg + "\n\nclass C\n" }
	a := analyze(t, "kotlin", tree(
		"client/tests/nonJvm/test/io/acme/ClientTest.kt", cls("io.acme"),
		"client/apache/jvm/test/io/acme/apache/ApacheTest.kt", cls("io.acme.apache"),
		"client/apache/jvm/src/io/acme/apache/Apache.kt", cls("io.acme.apache"),
		"app/test@jvm/AppTest.kt", cls("app"),
		"app/src/App.kt", cls("app"),
		"src/jvmTest/kotlin/acme/JvmTest.kt", cls("acme"),
		"src/main/kotlin/acme/test/Fixture.kt", cls("acme.test"),
		"lib/Lib.kt", cls("lib"),
	))
	want := map[string]bool{
		"client/tests/nonJvm/test/io/acme/ClientTest.kt":      true,
		"client/apache/jvm/test/io/acme/apache/ApacheTest.kt": true,
		"client/apache/jvm/src/io/acme/apache/Apache.kt":      false,
		"app/test@jvm/AppTest.kt":                             true,
		"app/src/App.kt":                                      false,
		"src/jvmTest/kotlin/acme/JvmTest.kt":                  true,
		"src/main/kotlin/acme/test/Fixture.kt":                false,
		"lib/Lib.kt":                                          false,
	}
	for _, f := range a.Files {
		if test, ok := want[f.Path]; ok && (f.Role == facts.RoleTest) != test {
			t.Errorf("%s: want test %v, got role %v", f.Path, test, f.Role)
		}
	}
	if len(a.Files) != len(want) {
		t.Errorf("want %d files, got %d", len(want), len(a.Files))
	}
}
