//go:build cgo

package treesitter

import (
	"testing"
)

// viewpoint: behaviour
// target: the Java and Kotlin readers
// A spy runs the real methods of the object it wraps, so a call on a field annotated as a spy
// (@InjectSpy, @Spy, @SpyK) calls the product's method. A call on a mock field calls nothing of the
// product.
func TestCallsOnSpiesReachTheProduct(t *testing.T) {
	t.Run("kotlin", func(t *testing.T) {
		expect(t, calls(t, "kotlin",
			"src/main/kotlin/acme/Store.kt", ktStore,
			"src/test/kotlin/acme/StoreTest.kt", `package acme

import io.mockk.impl.annotations.SpyK
import io.quarkus.test.InjectMock
import io.quarkus.test.junit.mockito.InjectSpy
import org.junit.jupiter.api.Test

class StoreTest {
    @InjectSpy
    lateinit var store: Store

    @SpyK
    var spied = Store()

    @InjectMock
    lateinit var mocked: Store

    @Test
    fun injected() { store.append(1) }

    @Test
    fun built() { spied.append(1) }

    @Test
    fun mocked() { mocked.append(1) }
}
`), `StoreTest.built calls=[acme.Store.append] unresolved=0
StoreTest.injected calls=[acme.Store.append] unresolved=0
StoreTest.mocked calls=[] unresolved=0`)
	})
	t.Run("java", func(t *testing.T) {
		expect(t, calls(t, "java",
			"src/main/java/acme/Store.java", "package acme;\n\npublic class Store {\n    public int append(int x) { return x + 1; }\n}\n",
			"src/test/java/acme/StoreTest.java", `package acme;

import org.junit.jupiter.api.Test;
import org.mockito.Mock;
import org.mockito.Spy;

class StoreTest {
    @Spy
    Store store = new Store();

    @Mock
    Store mocked;

    @Test
    void spied() { store.append(1); }

    @Test
    void mocked() { mocked.append(1); }
}
`), `StoreTest.mocked calls=[] unresolved=0
StoreTest.spied calls=[acme.Store.append] unresolved=0`)
	})
}

// viewpoint: behaviour
// target: the Java and Kotlin readers
// Mockito's @InjectMocks builds a real instance of the field's class and injects the mocks into it,
// so a call on the field calls the product's method, whether the field is declared or built.
func TestCallsOnInjectMocksFieldsReachTheProduct(t *testing.T) {
	t.Run("kotlin", func(t *testing.T) {
		expect(t, calls(t, "kotlin",
			"src/main/kotlin/acme/Store.kt", ktStore,
			"src/test/kotlin/acme/StoreTest.kt", `package acme

import org.junit.jupiter.api.Test
import org.mockito.InjectMocks

class StoreTest {
    @InjectMocks
    lateinit var store: Store

    @InjectMocks
    var built = Store()

    @Test
    fun injected() { store.append(1) }

    @Test
    fun built() { built.append(1) }
}
`), `StoreTest.built calls=[acme.Store.append] unresolved=0
StoreTest.injected calls=[acme.Store.append] unresolved=0`)
	})
	t.Run("java", func(t *testing.T) {
		expect(t, calls(t, "java",
			"src/main/java/acme/Store.java", "package acme;\n\npublic class Store {\n    public int append(int x) { return x + 1; }\n}\n",
			"src/test/java/acme/StoreTest.java", `package acme;

import org.junit.jupiter.api.Test;
import org.mockito.InjectMocks;

class StoreTest {
    @InjectMocks
    Store store;

    @InjectMocks
    Store built = new Store();

    @Test
    void injected() { store.append(1); }

    @Test
    void built() { built.append(1); }
}
`), `StoreTest.built calls=[acme.Store.append] unresolved=0
StoreTest.injected calls=[acme.Store.append] unresolved=0`)
	})
}

// viewpoint: behaviour
// target: the Java and Kotlin readers
// An @InjectMocks field, and the import of its annotation, is no mock: the field holds the real
// product. The @Mock fields injected into it are mocks.
func TestInjectMocksFieldsAreNoMock(t *testing.T) {
	const javaCalc = "package com.acme.calc;\n\n/** Calc adds. */\npublic class Calc {\n  /** Adds. */\n  public int add(int a, int b) { return a + b; }\n}\n"
	const ktCalc = "package com.acme.calc\n\n/** Calc adds. */\nclass Calc {\n    /** Adds. */\n    fun add(a: Int, b: Int) = a + b\n}\n"
	for _, withMock := range []bool{false, true} {
		javaMock, ktMock := "", ""
		if withMock {
			javaMock = "import org.mockito.Mock;\n\nclass CalcTest {\n  @Mock\n  Clock clock;\n\n"
			ktMock = "import org.mockito.Mock\n\nclass CalcTest {\n    @Mock\n    lateinit var clock: Clock\n\n"
		} else {
			javaMock = "\nclass CalcTest {\n"
			ktMock = "\nclass CalcTest {\n"
		}
		for _, l := range []struct{ lang, prodPath, prod, testPath, test string }{
			{"java", "src/main/java/com/acme/calc/Calc.java", javaCalc, "src/test/java/com/acme/calc/CalcTest.java",
				"package com.acme.calc;\n\nimport org.junit.jupiter.api.Test;\nimport org.mockito.InjectMocks;\n" + javaMock +
					"  @InjectMocks\n  Calc calc;\n\n  @Test\n  void adds() {\n    calc.add(1, 2);\n  }\n}\n"},
			{"kotlin", "src/main/kotlin/com/acme/calc/Calc.kt", ktCalc, "src/test/kotlin/com/acme/calc/CalcTest.kt",
				"package com.acme.calc\n\nimport org.junit.jupiter.api.Test\nimport org.mockito.InjectMocks\n" + ktMock +
					"    @InjectMocks\n    lateinit var calc: Calc\n\n    @Test\n    fun adds() {\n        calc.add(1, 2)\n    }\n}\n"},
		} {
			a := analyze(t, l.lang, tree(l.prodPath, l.prod, l.testPath, l.test))
			if got := mocked(t, a); got != withMock {
				t.Errorf("%s with a @Mock field %t: want a mock hazard %t, got %t", l.lang, withMock, withMock, got)
			}
		}
	}
}
