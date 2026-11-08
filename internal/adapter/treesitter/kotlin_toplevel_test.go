//go:build cgo

package treesitter

import (
	"testing"
)

// viewpoint: behaviour
// target: the Kotlin reader
// A Kotlin test follows a top-level function of the test sources as it follows a helper of its
// class: one of its own file, one of another file of its package, and one it imports from another
// package. What the function calls is what the test calls.
func TestKotlinTestsFollowTopLevelHelpers(t *testing.T) {
	expect(t, calls(t, "kotlin",
		"src/main/kotlin/acme/Store.kt", ktStore,
		"src/test/kotlin/acme/StoreSupport.kt", "package acme\n\ninternal fun appendOnce(x: Int) = Store().append(x)\n",
		"src/test/kotlin/acme/support/Twice.kt", "package acme.support\n\nimport acme.Store\n\nfun appendTwice(x: Int) = Store().append(x) + 1\n",
		"src/test/kotlin/acme/StoreTest.kt", `package acme

import acme.support.appendTwice
import org.junit.jupiter.api.Test
import kotlin.test.assertEquals

private fun appendHere(x: Int): Int = Store().append(x)

class StoreTest {
    @Test
    fun sameFile() { assertEquals(2, appendHere(1)) }

    @Test
    fun samePackage() { assertEquals(2, appendOnce(1)) }

    @Test
    fun imported() { assertEquals(3, appendTwice(1)) }
}
`), `StoreTest.imported calls=[acme.Store acme.Store.append] unresolved=0
StoreTest.sameFile calls=[acme.Store acme.Store.append] unresolved=0
StoreTest.samePackage calls=[acme.Store acme.Store.append] unresolved=0`)
}
