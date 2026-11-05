//go:build cgo

package treesitter

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// viewpoint: failure handling
// target: the Java and Kotlin readers
// A comment between a call's arguments is not an argument: reading such a call does not stop the
// analysis, and the arguments around the comment are the ones compared. A Kotlin named argument is
// its value, not its name.
func TestCommentsBetweenArgumentsAreNotArguments(t *testing.T) {
	describe := func(name string, files ...string) string {
		var out []string
		for _, ts := range analyze(t, name, tree(files...)).Tests {
			var hz []string
			for _, h := range ts.Hazards {
				hz = append(hz, string(h.Category))
			}
			var calls []string
			for _, c := range ts.Calls {
				calls = append(calls, c.Target)
			}
			out = append(out, fmt.Sprintf("%s calls=%v hazards=%v asserts=%d", ts.Name, calls, hz, ts.Asserts))
		}
		sort.Strings(out)
		return strings.Join(out, "\n")
	}
	t.Run("kotlin", func(t *testing.T) {
		expect(t, describe("kotlin",
			"src/main/kotlin/acme/store/Store.kt", "package acme.store\n\nfun size(): Int = 1\n",
			"src/test/kotlin/acme/store/StoreTest.kt", `package acme.store

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class StoreTest {
    @Test
    fun commented() {
        assertEquals(
            // the size after one append
            1,
            size(),
        )
    }

    @Test
    fun named() {
        assertEquals(expected = 1, actual = 1)
    }

    @Test
    fun constant() {
        assertTrue(
            // always holds
            true,
        )
    }
}
`), `StoreTest.commented calls=[acme.store.size] hazards=[] asserts=1
StoreTest.constant calls=[] hazards=[always-true] asserts=1
StoreTest.named calls=[] hazards=[always-true] asserts=1`)
	})
	t.Run("java", func(t *testing.T) {
		expect(t, describe("java",
			"src/main/java/acme/store/Store.java", "package acme.store;\n\npublic class Store {\n    public static int size() { return 1; }\n}\n",
			"src/test/java/acme/store/StoreTest.java", `package acme.store;

import static org.junit.jupiter.api.Assertions.assertTrue;

import org.junit.jupiter.api.Test;

class StoreTest {
    @Test
    void constant() {
        assertTrue(/* always holds */ true);
    }
}
`), `StoreTest.constant calls=[] hazards=[always-true] asserts=1`)
	})
}
