//go:build cgo

package treesitter

import "testing"

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
