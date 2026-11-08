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
