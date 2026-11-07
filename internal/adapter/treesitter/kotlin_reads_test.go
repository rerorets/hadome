//go:build cgo

package treesitter

import "testing"

// viewpoint: behaviour
// target: the Kotlin reader
// A product value substituted into a string template is read: "IN ($COUNTED)" and
// "${Kind.ORDERS.table}" change when the product's value does, and so does a field of the test
// class built from such a string. An escaped \$ is a dollar sign, and
// a name that is not the product's is no read.
func TestKotlinStringTemplatesReadProductValues(t *testing.T) {
	expect(t, reads(t, "kotlin", tree(
		"src/main/kotlin/acme/Kinds.kt", `package acme

const val COUNTED = "'A', 'B'"

enum class Kind(val table: String) { ORDERS("orders") }
`,
		"src/test/kotlin/acme/KindTest.kt", `package acme

import org.junit.jupiter.api.Test
import kotlin.test.assertTrue

class KindTest {
    private val query = "IN ($COUNTED)"

    @Test
    fun field() { assertTrue(query.isNotEmpty()) }

    @Test
    fun simple() { assertTrue("x IN ('A', 'B')".contains("IN ($COUNTED)")) }

    @Test
    fun braced() { assertTrue("orders".startsWith("${Kind.ORDERS.table}")) }

    @Test
    fun both() { assertTrue("$COUNTED/${Kind.ORDERS.table}".isNotEmpty()) }

    @Test
    fun nested() { assertTrue("${"$COUNTED".length}".isNotEmpty()) }

    @Test
    fun escaped() { assertTrue("\$COUNTED".isNotEmpty()) }

    @Test
    fun other() { val name = "a"; assertTrue("$name and $COUNTEDX".isNotEmpty()) }
}
`)), `
KindTest.both reads=2 calls=0
KindTest.braced reads=1 calls=0
KindTest.escaped reads=0 calls=0
KindTest.field reads=1 calls=0
KindTest.nested reads=1 calls=0
KindTest.other reads=0 calls=0
KindTest.simple reads=1 calls=0`)
}

// viewpoint: behaviour
// target: the Kotlin reader
// A test that reads a field it inherits from a test class of another file, built from a product
// value, reads the product, as it does when the field is its own class's. So does a test of a class
// nested in it.
func TestKotlinInheritedValueFieldsAreReads(t *testing.T) {
	expect(t, reads(t, "kotlin", tree(
		"src/main/kotlin/acme/Query.kt", "package acme\n\nval QUERY = \"SELECT 1\"\n",
		"src/test/kotlin/acme/QuerySupport.kt", `package acme

abstract class QuerySupport {
    protected val sql = QUERY.trim()
    protected val other = "SELECT 2"
}
`,
		"src/test/kotlin/acme/QueryTest.kt", `package acme

import kotlin.test.Test
import kotlin.test.assertTrue
import org.junit.jupiter.api.Nested

class QueryTest : QuerySupport() {
    @Test
    fun field() { assertTrue(sql.contains("SELECT")) }

    @Test
    fun plain() { assertTrue(other.contains("SELECT")) }

    @Test
    fun shadowed() { val sql = "x"; assertTrue(sql.isNotEmpty()) }

    @Nested
    inner class Inner {
        @Test
        fun nested() { assertTrue(sql.contains("SELECT")) }
    }
}
`)), `
QueryTest.Inner.nested reads=1 calls=0
QueryTest.field reads=1 calls=0
QueryTest.plain reads=0 calls=0
QueryTest.shadowed reads=0 calls=0`)
}
