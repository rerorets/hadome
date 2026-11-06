//go:build cgo

package treesitter

import "testing"

// viewpoint: behaviour
// target: the Kotlin reader
// A test class's companion object holds fields and helpers its tests use by their bare names, as
// the class's own members are.
func TestCompanionMembersAreTheTestClasssToo(t *testing.T) {
	expect(t, calls(t, "kotlin",
		"src/main/kotlin/acme/Store.kt", "package acme\n\nclass Store {\n    fun add(row: String): Int = row.length\n}\n\nfun double(n: Int): Int = n * 2\n",
		"src/test/kotlin/acme/StoreTest.kt", `package acme

import org.junit.jupiter.api.Test
import kotlin.test.assertEquals

class StoreTest {
    companion object {
        val shared = Store()

        fun twice(n: Int): Int = double(n)
    }

    @Test
    fun field() { assertEquals(1, shared.add("x")) }

    @Test
    fun helper() { assertEquals(4, twice(2)) }
}
`), "StoreTest.field calls=[acme.Store.add] unresolved=0\nStoreTest.helper calls=[acme.double] unresolved=0")
}
