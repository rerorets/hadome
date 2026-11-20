// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

//go:build cgo

package treesitter

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// calls renders each test's call targets and how many calls it could not identify.
func calls(t *testing.T, name string, files ...string) string {
	t.Helper()
	var out []string
	for _, ts := range analyze(t, name, tree(files...)).Tests {
		var targets []string
		for _, c := range ts.Calls {
			targets = append(targets, c.Target)
		}
		out = append(out, fmt.Sprintf("%s calls=%v unresolved=%d", ts.Name, targets, ts.Unresolved))
	}
	sort.Strings(out)
	return strings.Join(out, "\n")
}

// viewpoint: behaviour
// target: the Java and Kotlin readers
// A call through a field of the test class reaches the product when the field is built by a
// product constructor, or is declared with a product type and has no initializer (lateinit, injected,
// a constructor property). A field built by anything else, such as a mock or another value, stays
// unidentified, and so does a constructor parameter that is not a property.
func TestCallsThroughTestClassFieldsReachTheProduct(t *testing.T) {
	t.Run("kotlin", func(t *testing.T) {
		expect(t, calls(t, "kotlin",
			"src/main/kotlin/acme/store/Builder.kt", "package acme.store\n\nclass Builder {\n    fun parse(s: String): Int = s.length\n}\n",
			"src/test/kotlin/acme/store/BuilderTest.kt", `package acme.store

import io.mockk.mockk
import kotlin.test.Test
import kotlin.test.assertEquals

class BuilderTest(private val injected: Builder) {
    private val built = Builder()
    private lateinit var late: Builder
    private val mocked: Builder = mockk()

    @Test
    fun viaBuilt() { assertEquals(1, built.parse("x")) }

    @Test
    fun viaLateinit() { assertEquals(1, late.parse("x")) }

    @Test
    fun viaConstructor() { assertEquals(1, injected.parse("x")) }

    @Test
    fun viaMock() { assertEquals(1, mocked.parse("x")) }

    @Test
    fun viaShared() { assertEquals(1, aliased.parse("x")) }

    private val aliased: Builder = shared
}

// plain is a constructor parameter, not a property: no member reaches it.
class PlainTest(plain: Builder) {
    @Test
    fun viaPlain() { assertEquals(1, plain.parse("x")) }
}
`), `BuilderTest.viaBuilt calls=[acme.store.Builder.parse] unresolved=0
BuilderTest.viaConstructor calls=[acme.store.Builder.parse] unresolved=0
BuilderTest.viaLateinit calls=[acme.store.Builder.parse] unresolved=0
BuilderTest.viaMock calls=[] unresolved=0
BuilderTest.viaShared calls=[] unresolved=0
PlainTest.viaPlain calls=[] unresolved=0`)
	})
	t.Run("java", func(t *testing.T) {
		expect(t, calls(t, "java",
			"src/main/java/acme/store/Builder.java", "package acme.store;\n\npublic class Builder {\n    public int parse(String s) { return s.length(); }\n}\n",
			"src/test/java/acme/store/BuilderTest.java", `package acme.store;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.mockito.Mockito.mock;

import org.junit.jupiter.api.Test;

class BuilderTest {
    private final Builder built = new Builder();
    private Builder injected;
    private final Builder mocked = mock(Builder.class);

    @Test
    void viaBuilt() { assertEquals(1, built.parse("x")); }

    @Test
    void viaInjected() { assertEquals(1, injected.parse("x")); }

    @Test
    void viaMock() { assertEquals(1, mocked.parse("x")); }
}
`), `BuilderTest.viaBuilt calls=[acme.store.Builder.parse] unresolved=0
BuilderTest.viaInjected calls=[acme.store.Builder.parse] unresolved=0
BuilderTest.viaMock calls=[] unresolved=0`)
	})
}

// viewpoint: behaviour
// target: the Java and Kotlin readers
// A field the code run before each test assigns is read as it would be with the assigned value as
// its initializer: assigned a mock, a call through it reaches nothing of the product, even inside
// MockK's every { … } and verify { … } and in a nested class; assigned a product object, it reaches
// that object's class. A local of the setup that shares the field's name leaves the field as declared.
func TestFieldsAssignedBeforeEachTestAreReadAsTheirValue(t *testing.T) {
	t.Run("kotlin", func(t *testing.T) {
		expect(t, calls(t, "kotlin",
			"src/main/kotlin/acme/store/Builder.kt", "package acme.store\n\nopen class Builder {\n    open fun parse(s: String): Int = s.length\n}\n",
			"src/test/kotlin/acme/store/BuilderTest.kt", `package acme.store

import io.mockk.every
import io.mockk.mockk
import io.mockk.verify
import kotlin.test.BeforeTest
import kotlin.test.Test
import kotlin.test.assertEquals
import org.junit.jupiter.api.Nested

class BuilderTest {
    private lateinit var mocked: Builder
    private lateinit var typed: Builder
    private lateinit var self: Builder
    private lateinit var built: Builder
    private lateinit var shadowed: Builder

    @BeforeTest
    fun setUp() {
        mocked = mockk(relaxed = true)
        typed = mockk<Builder>()
        this.self = mockk()
        built = Builder()
        var shadowed = Builder()
        shadowed = mockk()
        every { mocked.parse(any()) } returns 1
    }

    @Test
    fun viaMock() {
        assertEquals(1, mocked.parse("x"))
        verify { mocked.parse("x") }
    }

    @Test
    fun viaTypedMock() { assertEquals(1, typed.parse("x")) }

    @Test
    fun viaThis() { assertEquals(1, self.parse("x")) }

    @Test
    fun viaBuilt() { assertEquals(1, built.parse("x")) }

    @Test
    fun viaShadowed() { assertEquals(1, shadowed.parse("x")) }

    @Nested
    inner class Inner {
        @Test
        fun viaOuterMock() { assertEquals(1, mocked.parse("x")) }
    }
}
`), `BuilderTest.Inner.viaOuterMock calls=[] unresolved=0
BuilderTest.viaBuilt calls=[acme.store.Builder.parse] unresolved=0
BuilderTest.viaMock calls=[] unresolved=0
BuilderTest.viaShadowed calls=[acme.store.Builder.parse] unresolved=0
BuilderTest.viaThis calls=[] unresolved=0
BuilderTest.viaTypedMock calls=[] unresolved=0`)
	})
	t.Run("java", func(t *testing.T) {
		expect(t, calls(t, "java",
			"src/main/java/acme/store/Builder.java", "package acme.store;\n\npublic class Builder {\n    public int parse(String s) { return s.length(); }\n}\n",
			"src/test/java/acme/store/BuilderTest.java", `package acme.store;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.mockito.Mockito.mock;

import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.mockito.Mockito;

class BuilderTest {
    private Builder mocked;
    private Builder self;
    private Builder built;
    private Builder shadowed;

    @BeforeEach
    void setUp() {
        mocked = mock(Builder.class);
        this.self = Mockito.mock(Builder.class);
        built = new Builder();
        Builder shadowed = new Builder();
        shadowed = mock(Builder.class);
    }

    @Test
    void viaMock() { assertEquals(1, mocked.parse("x")); }

    @Test
    void viaThis() { assertEquals(1, self.parse("x")); }

    @Test
    void viaBuilt() { assertEquals(1, built.parse("x")); }

    @Test
    void viaShadowed() { assertEquals(1, shadowed.parse("x")); }
}
`), `BuilderTest.viaBuilt calls=[acme.store.Builder.parse] unresolved=0
BuilderTest.viaMock calls=[] unresolved=0
BuilderTest.viaShadowed calls=[acme.store.Builder.parse] unresolved=0
BuilderTest.viaThis calls=[] unresolved=0`)
	})
}
