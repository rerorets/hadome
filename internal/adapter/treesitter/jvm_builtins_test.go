//go:build cgo

package treesitter

import (
	"testing"
)

// viewpoint: behaviour
// target: the Java and Kotlin readers
// A method called on a local the test built as a string or a standard collection, from a literal,
// a string method or a collection builder, is the language's own method: it is neither a call of the
// product nor an unresolved call. A method called on a local of unknown origin stays unresolved.
func TestCallsOnLocalStringsAndCollectionsAreTheLanguages(t *testing.T) {
	t.Run("kotlin", func(t *testing.T) {
		expect(t, calls(t, "kotlin",
			"src/main/kotlin/acme/Store.kt", ktStore,
			"src/test/kotlin/acme/QueryTest.kt", `package acme

import org.junit.jupiter.api.Test
import kotlin.test.assertTrue

class QueryTest {
    @Test
    fun literal() {
        val sql = "SELECT 1"
        assertTrue(sql.contains("SELECT"))
    }

    @Test
    fun built() {
        val sql = """
            select 1
        """.trimIndent().uppercase()
        val words = sql.split(" ")
        assertTrue(words.contains("SELECT"))
    }

    @Test
    fun collection() {
        val xs = mutableListOf(1, 2)
        assertTrue(xs.contains(1))
    }

    @Test
    fun typed() {
        val sql: String = loader()
        assertTrue(sql.contains("SELECT"))
    }

    @Test
    fun unknown() {
        val r = loader()
        assertTrue(r.contains(1))
    }

    @Test
    fun element() {
        val first = listOf(loader()).first()
        assertTrue(first.contains(1))
    }
}
`), `QueryTest.built calls=[] unresolved=0
QueryTest.collection calls=[] unresolved=0
QueryTest.element calls=[] unresolved=1
QueryTest.literal calls=[] unresolved=0
QueryTest.typed calls=[] unresolved=0
QueryTest.unknown calls=[] unresolved=1`)
	})
	t.Run("java", func(t *testing.T) {
		expect(t, calls(t, "java",
			"src/main/java/acme/Store.java", "package acme;\n\npublic class Store {}\n",
			"src/test/java/acme/QueryTest.java", `package acme;

import java.util.List;
import org.junit.jupiter.api.Test;

class QueryTest {
    @Test
    void literal() {
        var sql = "SELECT 1";
        sql.contains("SELECT");
    }

    @Test
    void typed() {
        String sql = build();
        List<String> words = List.of(sql);
        words.contains("SELECT");
    }

    @Test
    void unknown() {
        var r = loader();
        r.contains(1);
    }
}
`), `QueryTest.literal calls=[] unresolved=0
QueryTest.typed calls=[] unresolved=0
QueryTest.unknown calls=[] unresolved=1`)
	})
}
