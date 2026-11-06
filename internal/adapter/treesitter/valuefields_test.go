//go:build cgo

package treesitter

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// viewpoint: behaviour
// target: the Java and Kotlin readers
// A test that reads a field of its class built from a product value reads the product: when the
// value changes, the test's result can change. A field built from something else, a local of the
// same name, and the same name read from another object are not reads of the product.
func TestFieldsBuiltFromProductValuesAreReads(t *testing.T) {
	reads := func(name string, files ...string) string {
		var out []string
		for _, ts := range analyze(t, name, tree(files...)).Tests {
			out = append(out, fmt.Sprintf("%s reads=%d", ts.Name, ts.Reads))
		}
		sort.Strings(out)
		return strings.Join(out, "\n")
	}
	t.Run("kotlin", func(t *testing.T) {
		expect(t, reads("kotlin",
			"src/main/kotlin/acme/Query.kt", "package acme\n\nval QUERY = \"SELECT 1\"\n",
			"src/test/kotlin/acme/QueryTest.kt", `package acme

import kotlin.test.Test
import kotlin.test.assertTrue

class QueryTest {
    private val sql = QUERY.trim()
    private val other = "SELECT 2"

    @Test
    fun field() { assertTrue(sql.contains("SELECT")) }

    @Test
    fun plain() { assertTrue(other.contains("SELECT")) }

    @Test
    fun shadowed() { val sql = "x"; assertTrue(sql.isNotEmpty()) }

    @Test
    fun elsewhere() { assertTrue(Holder().sql.isNotEmpty()) }

    @Test
    fun self() { assertTrue(this.sql.isNotEmpty()) }
}
`), "QueryTest.elsewhere reads=0\nQueryTest.field reads=1\nQueryTest.plain reads=0\nQueryTest.self reads=1\nQueryTest.shadowed reads=0")
	})
	t.Run("java", func(t *testing.T) {
		expect(t, reads("java",
			"src/main/java/acme/Query.java", "package acme;\n\npublic class Query {\n    public static final String TEXT = \"SELECT 1\";\n}\n",
			"src/test/java/acme/QueryTest.java", `package acme;

import static org.junit.jupiter.api.Assertions.assertTrue;

import org.junit.jupiter.api.Test;

class QueryTest {
    private final String sql = Query.TEXT.trim();
    private final String other = "SELECT 2";

    @Test
    void field() { assertTrue(sql.contains("SELECT")); }

    @Test
    void plain() { assertTrue(other.contains("SELECT")); }

    @Test
    void self() { assertTrue(this.sql.length() > 0); }

    @Test
    void elsewhere() { assertTrue(new Holder().sql.length() > 0); }
}
`), "QueryTest.elsewhere reads=0\nQueryTest.field reads=1\nQueryTest.plain reads=0\nQueryTest.self reads=1")
	})
}
