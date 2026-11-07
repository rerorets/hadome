//go:build cgo

package treesitter

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// hazardsOf renders each test's hazards as category@path:line, with the test's own path left out.
func hazardsOf(t *testing.T, name string, files ...string) string {
	t.Helper()
	var out []string
	for _, ts := range analyze(t, name, tree(files...)).Tests {
		var hz []string
		for _, h := range ts.Hazards {
			at := fmt.Sprint(h.Line)
			if h.Path != ts.Path {
				at = h.Path + ":" + at
			}
			hz = append(hz, string(h.Category)+"@"+at)
		}
		sort.Strings(hz)
		out = append(out, fmt.Sprintf("%s hazards=%v", ts.Name, hz))
	}
	sort.Strings(out)
	return strings.Join(out, "\n")
}

const ktStore = "package acme\n\nclass Store {\n    fun append(x: Int): Int = x + 1\n}\n"

// viewpoint: behaviour
// target: the Kotlin reader
// A test class that extends a test class of another file uses that class's fields and helpers as
// its own, and those of the class that one extends in turn. Names in the inherited helper resolve as
// its own file imports them.
func TestKotlinTestClassesInheritFieldsAndHelpers(t *testing.T) {
	expect(t, calls(t, "kotlin",
		"src/main/kotlin/acme/Store.kt", ktStore,
		"src/main/kotlin/acme/limits/Limit.kt", "package acme.limits\n\nfun cap(x: Int): Int = x\n",
		"src/test/kotlin/acme/BaseSupport.kt", `package acme

import acme.limits.cap

abstract class BaseSupport {
    protected fun capped(x: Int): Int = cap(x)
}
`,
		"src/test/kotlin/acme/StoreSupport.kt", `package acme

abstract class StoreSupport : BaseSupport() {
    protected val store = Store()

    protected fun appendOne(): Int = store.append(1)
}
`,
		"src/test/kotlin/acme/StoreTest.kt", `package acme

import org.junit.jupiter.api.Test
import kotlin.test.assertEquals

class StoreTest : StoreSupport() {
    @Test
    fun field() { assertEquals(2, store.append(1)) }

    @Test
    fun helper() { assertEquals(2, appendOne()) }

    @Test
    fun grandparent() { assertEquals(1, capped(1)) }
}
`), `StoreTest.field calls=[acme.Store.append] unresolved=0
StoreTest.grandparent calls=[acme.limits.cap] unresolved=0
StoreTest.helper calls=[acme.Store.append] unresolved=0`)
}

// viewpoint: behaviour
// target: the Java and Kotlin readers
// The tests of a JUnit @Nested class use the fields and helpers of the class around it.
func TestNestedTestClassesUseTheEnclosingClasssFields(t *testing.T) {
	t.Run("kotlin", func(t *testing.T) {
		expect(t, calls(t, "kotlin",
			"src/main/kotlin/acme/Store.kt", ktStore,
			"src/test/kotlin/acme/StoreTest.kt", `package acme

import org.junit.jupiter.api.Nested
import org.junit.jupiter.api.Test
import kotlin.test.assertEquals

class StoreTest {
    private lateinit var store: Store

    private fun appendOne(): Int = store.append(1)

    @Nested
    inner class Appending {
        @Test
        fun direct() { assertEquals(2, store.append(1)) }

        @Test
        fun helper() { assertEquals(2, appendOne()) }
    }
}
`), `StoreTest.Appending.direct calls=[acme.Store.append] unresolved=0
StoreTest.Appending.helper calls=[acme.Store.append] unresolved=0`)
	})
	t.Run("java", func(t *testing.T) {
		expect(t, calls(t, "java",
			"src/main/java/acme/Store.java", "package acme;\n\npublic class Store {\n    public int append(int x) { return x + 1; }\n}\n",
			"src/test/java/acme/StoreTest.java", `package acme;

import org.junit.jupiter.api.Nested;
import org.junit.jupiter.api.Test;

class StoreTest {
    private Store store;

    @Nested
    class Appending {
        @Test
        void direct() { store.append(1); }
    }
}
`), `StoreTest.Appending.direct calls=[acme.Store.append] unresolved=0`)
	})
}

// viewpoint: behaviour
// target: the Java and Kotlin readers
// What runs before each test (@BeforeEach, @BeforeAll, JUnit 4's @Before, Kotlin's init blocks, and
// the setup of an enclosing or extended class) reads the clock or sleeps for every test of the
// class. Those hazards are each test's, found where the setup has them. Kotlin's timing functions
// read the clock too.
func TestSetupHazardsAreEachTestsHazards(t *testing.T) {
	t.Run("kotlin", func(t *testing.T) {
		expect(t, hazardsOf(t, "kotlin",
			"src/main/kotlin/acme/Store.kt", ktStore,
			"src/test/kotlin/acme/ClockSupport.kt", `package acme

import org.junit.jupiter.api.BeforeEach

abstract class ClockSupport {
    @BeforeEach
    fun pause() {
        Thread.sleep(5)
    }
}
`,
			"src/test/kotlin/acme/StoreTest.kt", `package acme

import java.time.Instant
import kotlin.system.measureTimeMillis
import org.junit.jupiter.api.BeforeAll
import org.junit.jupiter.api.BeforeEach
import org.junit.jupiter.api.Nested
import org.junit.jupiter.api.Test

class StoreTest : ClockSupport() {
    private var started = Instant.EPOCH

    init {
        started = Instant.now()
    }

    @BeforeEach
    fun setup() {
        Thread.sleep(10)
    }

    @Test
    fun appends() { Store().append(1) }

    @Test
    fun timed() { measureTimeMillis { Store().append(1) } }

    @Nested
    inner class Inner {
        @Test
        fun nested() { Store().append(1) }
    }

    companion object {
        @JvmStatic
        @BeforeAll
        fun once() { System.currentTimeMillis() }
    }
}

class PlainTest {
    @Test
    fun alone() { Store().append(1) }
}
`), `PlainTest.alone hazards=[]
StoreTest.Inner.nested hazards=[clock@14 clock@37 sleep@19 sleep@src/test/kotlin/acme/ClockSupport.kt:8]
StoreTest.appends hazards=[clock@14 clock@37 sleep@19 sleep@src/test/kotlin/acme/ClockSupport.kt:8]
StoreTest.timed hazards=[clock@14 clock@26 clock@37 sleep@19 sleep@src/test/kotlin/acme/ClockSupport.kt:8]`)
	})
	t.Run("java", func(t *testing.T) {
		expect(t, hazardsOf(t, "java",
			"src/main/java/acme/Store.java", "package acme;\n\npublic class Store {\n    public int append(int x) { return x + 1; }\n}\n",
			"src/test/java/acme/StoreTest.java", `package acme;

import java.time.Instant;
import org.junit.Before;
import org.junit.Test;

public class StoreTest {
    private Instant started;

    @Before
    public void setUp() {
        started = Instant.now();
    }

    @Test
    public void appends() { new Store().append(1); }
}
`), `StoreTest.appends hazards=[clock@12]`)
	})
}

// viewpoint: behaviour
// target: the Java and Kotlin readers
// Quarkus's @InjectMock and @InjectSpy fields are mocks, as Mockito's @Mock fields are.
func TestQuarkusMockAnnotationsAreMocks(t *testing.T) {
	expect(t, hazardsOf(t, "java",
		"src/main/java/acme/Store.java", "package acme;\n\npublic class Store {\n    public int append(int x) { return x + 1; }\n}\n",
		"src/test/java/acme/StoreTest.java", `package acme;

import io.quarkus.test.InjectMock;
import io.quarkus.test.junit.mockito.InjectSpy;
import org.junit.jupiter.api.Test;

class StoreTest {
    @InjectMock
    Store mocked;

    @InjectSpy
    Store spied;

    @Test
    void appends() { new Store().append(1); }
}
`), `StoreTest.appends hazards=[mock@11 mock@8]`)
}

// viewpoint: behaviour
// target: the Kotlin reader
// A call with type arguments and a lambda (assertThrows<E> { … }, mockk<T> { … }) is a call
// like any other, although this grammar reads it as two comparisons: it checks, mocks or calls the
// product as its name says, and the type argument is no value the test reads. A call with a trailing
// lambda after its arguments counts once.
func TestKotlinTypedCallsWithLambdas(t *testing.T) {
	t.Run("asserts", func(t *testing.T) {
		expect(t, asserts(t, "kotlin", tree(
			"src/main/kotlin/acme/Store.kt", ktStore,
			"src/test/kotlin/acme/StoreTest.kt", `package acme

import org.junit.jupiter.api.Test
import org.junit.jupiter.api.assertThrows

class StoreTest {
    @Test
    fun braces() { assertThrows<IllegalStateException> { Store().append(0) } }

    @Test
    fun parenthesized() { org.junit.jupiter.api.assertThrows<IllegalStateException>({ Store().append(0) }) }

    @Test
    fun withMessage() { assertThrows<IllegalStateException>("fails") { Store().append(0) } }
}
`)), `StoreTest.braces asserts=1 unknown=false
StoreTest.parenthesized asserts=1 unknown=false
StoreTest.withMessage asserts=1 unknown=false`)
	})
	t.Run("calls and reads", func(t *testing.T) {
		expect(t, reads(t, "kotlin", tree(
			"src/main/kotlin/acme/Store.kt", ktStore+"\nfun <T> build(make: () -> T): T = make()\n",
			"src/test/kotlin/acme/StoreTest.kt", `package acme

import org.junit.jupiter.api.Test
import org.junit.jupiter.api.assertThrows

class StoreTest {
    @Test
    fun typed() { assertThrows<IllegalStateException> { Store().append(0) } }

    @Test
    fun product() { build<Store> { Store() } }
}
`)), "StoreTest.product reads=0 calls=2\nStoreTest.typed reads=0 calls=2")
	})
	t.Run("mocks", func(t *testing.T) {
		expect(t, hazardsOf(t, "kotlin",
			"src/main/kotlin/acme/Store.kt", ktStore,
			"src/test/kotlin/acme/StoreTest.kt", `package acme

import io.mockk.mockk
import org.junit.jupiter.api.Test

class StoreTest {
    @Test
    fun mocked() {
        val store = mockk<Store> { every { append(1) } returns 2 }
        store.append(1)
    }
}
`), "StoreTest.mocked hazards=[mock@3 mock@9]")
	})
}

// viewpoint: behaviour
// target: the Kotlin reader
// MockK's checks count as asserts, including those of suspending calls (coVerify and its kin) and
// confirmVerified.
func TestMockKChecksAreAsserts(t *testing.T) {
	expect(t, asserts(t, "kotlin", tree(
		"src/main/kotlin/acme/Store.kt", ktStore,
		"src/test/kotlin/acme/StoreTest.kt", `package acme

import io.mockk.coVerify
import io.mockk.coVerifyOrder
import io.mockk.confirmVerified
import io.mockk.mockk
import io.mockk.verifyAll
import org.junit.jupiter.api.Test

class StoreTest {
    private val store = mockk<Store>(relaxed = true)

    @Test
    fun suspending() { coVerify { store.append(1) } }

    @Test
    fun ordered() { coVerifyOrder { store.append(1) } }

    @Test
    fun all() { verifyAll { store.append(1) } }

    @Test
    fun confirmed() { confirmVerified(store) }

    @Test
    fun none() { store.append(1) }
}
`)), `StoreTest.all asserts=1 unknown=false
StoreTest.confirmed asserts=1 unknown=false
StoreTest.none asserts=0 unknown=false
StoreTest.ordered asserts=1 unknown=false
StoreTest.suspending asserts=1 unknown=false`)
}

// viewpoint: behaviour
// target: the Java and Kotlin readers
// A REST Assured chain that starts at a helper whose body ends in then() checks the response, as
// one written after then() does. The same calls on a request, before then(), check nothing.
func TestRestAssuredChecksThroughAHelper(t *testing.T) {
	t.Run("kotlin", func(t *testing.T) {
		expect(t, asserts(t, "kotlin", tree(
			"src/main/kotlin/acme/Store.kt", ktStore,
			"src/test/kotlin/acme/ApiTest.kt", `package acme

import io.restassured.RestAssured.given
import io.restassured.response.ValidatableResponse
import org.hamcrest.Matchers.equalTo
import org.junit.jupiter.api.Test

class ApiTest {
    private fun list(): ValidatableResponse = given().get("/items").then()

    private fun request() = given().contentType("application/json")

    @Test
    fun checked() { list().statusCode(200).body("size()", equalTo(0)) }

    @Test
    fun unchecked() { request().body("{}").post("/items") }
}
`)), "ApiTest.checked asserts=2 unknown=false\nApiTest.unchecked asserts=0 unknown=false")
	})
	t.Run("java", func(t *testing.T) {
		expect(t, asserts(t, "java", tree(
			"src/main/java/acme/Store.java", "package acme;\n\npublic class Store {}\n",
			"src/test/java/acme/ApiTest.java", `package acme;

import static io.restassured.RestAssured.given;

import io.restassured.response.ValidatableResponse;
import org.junit.jupiter.api.Test;

class ApiTest {
    private ValidatableResponse list() {
        return given().get("/items").then();
    }

    @Test
    void checked() { list().statusCode(200); }
}
`)), "ApiTest.checked asserts=1 unknown=false")
	})
}

// viewpoint: behaviour
// target: the Java and Kotlin readers
// A call on a member of a product class reaches the product: on an enum constant it calls the enum
// class's method, and on a nested class that class's method. On any other member, such as a property
// of an object, the class of the value is unknown, so the test reads the product instead.
func TestCallsOnMembersOfProductClasses(t *testing.T) {
	t.Run("kotlin", func(t *testing.T) {
		expect(t, calls(t, "kotlin",
			"src/main/kotlin/acme/Kind.kt", `package acme

enum class Kind(val table: String) {
    ORDERS("orders");

    fun toTable(): String = table
}

class Outer {
    class Inner {
        fun build(): Int = 1
    }
}
`,
			"src/test/kotlin/acme/KindTest.kt", `package acme

import org.junit.jupiter.api.Test

class KindTest {
    @Test
    fun entry() { Kind.ORDERS.toTable() }

    @Test
    fun nested() { Outer.Inner.build() }
}
`), "KindTest.entry calls=[acme.Kind.toTable] unresolved=0\nKindTest.nested calls=[acme.Outer.Inner.build] unresolved=0")
	})
	t.Run("kotlin reads", func(t *testing.T) {
		expect(t, reads(t, "kotlin", tree(
			"src/main/kotlin/acme/Settings.kt", "package acme\n\nobject Settings {\n    val NAME = \"orders\"\n}\n",
			"src/test/kotlin/acme/SettingsTest.kt", `package acme

import org.junit.jupiter.api.Test

class SettingsTest {
    @Test
    fun property() { Settings.NAME.trim() }
}
`)), "SettingsTest.property reads=1 calls=0")
	})
	t.Run("java", func(t *testing.T) {
		expect(t, calls(t, "java",
			"src/main/java/acme/Kind.java", `package acme;

public enum Kind {
    ORDERS;

    public String toTable() { return "orders"; }
}
`,
			"src/test/java/acme/KindTest.java", `package acme;

import org.junit.jupiter.api.Test;

class KindTest {
    @Test
    void entry() { Kind.ORDERS.toTable(); }
}
`), "KindTest.entry calls=[acme.Kind.toTable] unresolved=0")
	})
}

// viewpoint: behaviour
// target: the Kotlin reader
// A field or local holds an object of the product's class when apply or also returns the object a
// constructor built, or when it holds a constant of a product enum, so calls through it reach the
// class. So does a call chained right on such an object.
func TestKotlinTypesFromApplyAndEnumConstants(t *testing.T) {
	expect(t, calls(t, "kotlin",
		"src/main/kotlin/acme/Queue.kt", `package acme

class Queue {
    var limit: Int = 1

    fun push(x: Int): Boolean = x <= limit
}

enum class Kind {
    DAILY;

    fun slot(s: String): Int = s.length
}
`,
		"src/test/kotlin/acme/QueueTest.kt", `package acme

import org.junit.jupiter.api.Test

class QueueTest {
    private val queue = Queue().apply { limit = 3 }

    @Test
    fun applied() { queue.push(2) }

    @Test
    fun also() {
        val q = Queue().also { it.limit = 2 }
        q.push(1)
    }

    @Test
    fun constant() {
        val kind = Kind.DAILY
        kind.slot("abc")
    }

    @Test
    fun chained() { Queue().apply { limit = 2 }.push(1) }
}
`), `QueueTest.also calls=[acme.Queue acme.Queue.push] unresolved=0
QueueTest.applied calls=[acme.Queue.push] unresolved=0
QueueTest.chained calls=[acme.Queue acme.Queue.push] unresolved=0
QueueTest.constant calls=[acme.Kind.slot] unresolved=0`)
}

// viewpoint: behaviour
// target: the Java and Kotlin readers
// Reading a property of a variable that holds a product object reads the product; calling a method
// on it is a call, not a read.
func TestPropertiesOfProductObjectsAreReads(t *testing.T) {
	t.Run("kotlin", func(t *testing.T) {
		expect(t, reads(t, "kotlin", tree(
			"src/main/kotlin/acme/Cache.kt", "package acme\n\nclass Cache {\n    var ttl: Long = 60\n\n    fun clear() {}\n}\n",
			"src/test/kotlin/acme/CacheTest.kt", `package acme

import org.junit.jupiter.api.Test
import kotlin.test.assertEquals

class CacheTest {
    private lateinit var cache: Cache

    @Test
    fun property() { assertEquals(60L, cache.ttl) }

    @Test
    fun chained() { cache.ttl.toString() }

    @Test
    fun method() { cache.clear() }
}
`)), "CacheTest.chained reads=1 calls=0\nCacheTest.method reads=0 calls=1\nCacheTest.property reads=1 calls=0")
	})
	t.Run("java", func(t *testing.T) {
		expect(t, reads(t, "java", tree(
			"src/main/java/acme/Cache.java", "package acme;\n\npublic class Cache {\n    public long ttl = 60;\n}\n",
			"src/test/java/acme/CacheTest.java", `package acme;

import org.junit.jupiter.api.Test;

class CacheTest {
    private Cache cache;

    @Test
    void property() { long t = cache.ttl; }
}
`)), "CacheTest.property reads=1 calls=0")
	})
}

// viewpoint: behaviour
// target: the Kotlin reader
// An annotation with arguments on its own line above a top-level class still belongs to the class,
// although this grammar splits it off: a reasoned @Disabled there skips the class's tests with that
// reason.
func TestKotlinClassAnnotationsOnTheirOwnLine(t *testing.T) {
	a := analyze(t, "kotlin", tree(
		"src/main/kotlin/acme/Store.kt", ktStore,
		"src/test/kotlin/acme/StoreTest.kt", `package acme

import org.junit.jupiter.api.Disabled
import org.junit.jupiter.api.Test

/** Appending. */
@Disabled("needs a database")
class StoreTest {
    @Test
    fun appends() { Store().append(1) }
}
`))
	var got []string
	for _, ts := range a.Tests {
		got = append(got, fmt.Sprintf("%s skipped=%t reason=%q", ts.Name, ts.Skip.Skipped, ts.Skip.Reason))
	}
	expect(t, strings.Join(got, "\n"), `StoreTest.appends skipped=true reason="needs a database"`)
}
