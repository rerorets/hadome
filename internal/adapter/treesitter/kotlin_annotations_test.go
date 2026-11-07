//go:build cgo

package treesitter

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// symbolLines renders each symbol as kind, ID, its lines and whether it has a doc, sorted.
func symbolLines(t *testing.T, name string, files ...string) string {
	t.Helper()
	var out []string
	for _, s := range analyze(t, name, tree(files...)).Symbols {
		out = append(out, fmt.Sprintf("%s %s lines=%d-%d doc=%t", s.Kind, s.ID, s.Line, s.EndLine, s.Doc))
	}
	sort.Strings(out)
	return strings.Join(out, "\n")
}

// viewpoint: behaviour
// target: the Kotlin reader
// Annotations with arguments on lines of their own before a top-level declaration leave the
// declaration and those after it as they are: each is found at its own lines, and a KDoc comment
// above the annotations documents it. Two or more such annotations are what the grammar otherwise
// reads as an expression that takes the declaration in.
func TestKotlinDeclarationsAfterAnnotationsWithArguments(t *testing.T) {
	expect(t, symbolLines(t, "kotlin", "src/main/kotlin/acme/Store.kt", `package acme

/** Keeps rows. */
@Suppress("unused")
@JvmInline("x")
class Store {
    /** Appends. */
    fun append(x: Int): Int = x + 1
}

@Suppress("unused")
@Deprecated("old")
@JvmName("y")
fun helper(): Int = 1

/** Counts. */
@Suppress("unused") @Deprecated("old")
fun count(): Int = 2

/** Stays. */
class After {
    fun stay() = 3
}
`), `
function acme.count lines=18-18 doc=true
function acme.helper lines=14-14 doc=false
method acme.After.stay lines=22-22 doc=false
method acme.Store.append lines=8-8 doc=true
type acme.After lines=21-23 doc=true
type acme.Store lines=6-9 doc=true`)
}

// viewpoint: behaviour
// target: the Kotlin reader
// A test class with several annotations with arguments on lines of their own is read as one: its
// tests are found at their lines, a @Disabled among the class's annotations skips them with its
// reason, and a @Nested class and a test method with such annotations are read the same way. So is
// a single such annotation after a declaration that ends in an expression. A class's annotations
// stay its own: they do not reach the first class declared inside the next class.
func TestKotlinTestClassesAfterAnnotationsWithArguments(t *testing.T) {
	var got []string
	for _, ts := range analyze(t, "kotlin", tree(
		"src/main/kotlin/acme/Store.kt", ktStore,
		"src/test/kotlin/acme/StoreTest.kt", `package acme

import org.junit.jupiter.api.Disabled
import org.junit.jupiter.api.DisplayName
import org.junit.jupiter.api.Nested
import org.junit.jupiter.api.Tag
import org.junit.jupiter.api.Test
import kotlin.test.assertEquals

@DisplayName("store")
@Tag("fast")
class StoreTest {
    @Test
    fun append() { assertEquals(2, Store().append(1)) }

    @Disabled("flaky")
    @Tag("slow")
    @Test
    fun later() { assertEquals(3, Store().append(2)) }

    @Nested @DisplayName("inner")
    @Tag("slow")
    inner class Inner {
        @Test
        fun nested() { assertEquals(4, Store().append(3)) }
    }
}
`,
		"src/test/kotlin/acme/SkippedTest.kt", `package acme

import org.junit.jupiter.api.Disabled
import org.junit.jupiter.api.Nested
import org.junit.jupiter.api.Tag
import org.junit.jupiter.api.Test
import kotlin.test.assertEquals

@Disabled("needs a database")
@Tag("slow")
class SkippedTest {
    @Test
    fun skipped() { assertEquals(2, Store().append(1)) }
}

class RunsTest {
    @Nested
    inner class Inner {
        @Test
        fun runs() { assertEquals(2, Store().append(1)) }
    }
}
`,
		"src/test/kotlin/acme/QueueTest.kt", `package acme

import org.junit.jupiter.api.Disabled
import org.junit.jupiter.api.Test
import kotlin.test.assertEquals

fun two() = 2

/** Waits for the queue. */
@Disabled("needs a queue")
class QueueTest {
    @Test
    fun alone() { assertEquals(2, Store().append(two() - 1)) }
}
`)).Tests {
		got = append(got, fmt.Sprintf("%s line=%d calls=%d skip=%t:%q", ts.Name, ts.Line, len(ts.Calls), ts.Skip.Skipped, ts.Skip.Reason))
	}
	sort.Strings(got)
	expect(t, strings.Join(got, "\n"), `
QueueTest.alone line=12 calls=2 skip=true:"needs a queue"
RunsTest.Inner.runs line=19 calls=2 skip=false:""
SkippedTest.skipped line=12 calls=2 skip=true:"needs a database"
StoreTest.Inner.nested line=24 calls=2 skip=false:""
StoreTest.append line=13 calls=2 skip=false:""
StoreTest.later line=16 calls=2 skip=true:"flaky"`)
}
