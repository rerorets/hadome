//go:build cgo

package treesitter

import (
	"fmt"
	"strings"
	"testing"
)

// viewpoint: behaviour
// target: the Java and Kotlin readers
// A local's declared type decides what it is, as in TypeScript: a type of the language, of the
// standard library by its package, or imported from outside is outside, as is a container of them
// and its elements; one of the product's classes or generic classes is that class; a nullable type
// is the type; a container of the product's class is outside, its elements of that class; var, a
// name nothing declares and a class of the language that holds anything say nothing, so the value
// decides, here unknown.
func TestJvmDeclaredTypesDecideWhatALocalIs(t *testing.T) {
	t.Run("java", func(t *testing.T) {
		types := [][2]string{{"string", "String"}, {"integer", "int"}, {"store", "Store"}, {"stores", "List<Store>"},
			{"strings", "List<String>"}, {"array", "Store[]"}, {"mapStore", "Map<String, Store>"},
			{"mapInt", "Map<String, Integer>"}, {"optional", "Optional<Store>"}, {"inferred", "var"},
			{"qualified", "java.util.List<Store>"}, {"thing", "Thing"}, {"object", "Object"}, {"box", "Box<Store>"},
			{"missing", "Missing"}, {"other", "com.other.Thing"}}
		var b strings.Builder
		for _, nt := range types {
			fmt.Fprintf(&b, "    @Test void %s() { %s x = load(); x.add(1); }\n", nt[0], nt[1])
			fmt.Fprintf(&b, "    @Test void %sElement() { %s x = load(); for (var e : x) { e.add(1); } }\n", nt[0], nt[1])
		}
		got := kinds(t, "java",
			"src/main/java/acme/Store.java", "package acme;\n\npublic class Store {\n    public int add(int n) { return n; }\n}\n",
			"src/main/java/acme/Box.java", "package acme;\n\npublic class Box<T> {\n    public int add(int n) { return n; }\n}\n",
			"src/test/java/acme/TypesTest.java", `package acme;

import java.util.List;
import java.util.Map;
import java.util.Optional;
import com.ext.Thing;
import org.junit.jupiter.api.Test;

class TypesTest {
    static <T> T load() { return null; }
`+b.String()+"}\n")
		expectEach(t, got, map[string]string{
			"TypesTest.string": "calls=[] unresolved=0", "TypesTest.stringElement": "calls=[] unresolved=0",
			"TypesTest.integer": "calls=[] unresolved=0", "TypesTest.store": "calls=[acme.Store.add] unresolved=0",
			"TypesTest.storeElement": "calls=[] unresolved=1",
			"TypesTest.stores":       "calls=[] unresolved=0", "TypesTest.storesElement": "calls=[acme.Store.add] unresolved=0",
			"TypesTest.stringsElement": "calls=[] unresolved=0", "TypesTest.arrayElement": "calls=[acme.Store.add] unresolved=0",
			"TypesTest.mapStore": "calls=[] unresolved=0", "TypesTest.mapStoreElement": "calls=[] unresolved=1",
			"TypesTest.mapIntElement": "calls=[] unresolved=0", "TypesTest.optionalElement": "calls=[acme.Store.add] unresolved=0",
			"TypesTest.inferred": "calls=[] unresolved=1", "TypesTest.qualified": "calls=[] unresolved=0",
			"TypesTest.qualifiedElement": "calls=[acme.Store.add] unresolved=0", "TypesTest.thing": "calls=[] unresolved=0",
			"TypesTest.object": "calls=[] unresolved=1", "TypesTest.box": "calls=[acme.Box.add] unresolved=0",
			"TypesTest.missing": "calls=[] unresolved=1", "TypesTest.other": "calls=[] unresolved=1",
		})
	})
	t.Run("kotlin", func(t *testing.T) {
		types := [][2]string{{"string", "String"}, {"integer", "Int"}, {"store", "Store"}, {"nullable", "Store?"},
			{"stores", "List<Store>"}, {"strings", "List<String>"}, {"array", "Array<Store>"},
			{"mapStore", "Map<String, Store>"}, {"pair", "Pair<String, Int>"}, {"qualified", "kotlin.collections.List<Store>"},
			{"thing", "Thing"}, {"any", "Any"}, {"box", "Box<Store>"}, {"function", "(String) -> Unit"},
			{"missing", "Missing"}}
		var b strings.Builder
		for _, nt := range types {
			fmt.Fprintf(&b, "    @Test fun %s() { val x: %s = load(); x.add(1) }\n", nt[0], nt[1])
			fmt.Fprintf(&b, "    @Test fun %sElement() { val x: %s = load(); for (e in x) { e.add(1) } }\n", nt[0], nt[1])
		}
		got := kinds(t, "kotlin",
			"src/main/kotlin/acme/Store.kt", "package acme\n\nclass Store {\n    fun add(n: Int): Int = n\n}\n\nclass Box<T> {\n    fun add(n: Int): Int = n\n}\n",
			"src/test/kotlin/acme/TypesTest.kt", `package acme

import com.ext.Thing
import org.junit.jupiter.api.Test

fun <T> load(): T = TODO()

class TypesTest {
`+b.String()+"}\n")
		expectEach(t, got, map[string]string{
			"TypesTest.string": "calls=[] unresolved=0", "TypesTest.stringElement": "calls=[] unresolved=0",
			"TypesTest.integer": "calls=[] unresolved=0", "TypesTest.store": "calls=[acme.Store.add] unresolved=0",
			"TypesTest.nullable": "calls=[acme.Store.add] unresolved=0",
			"TypesTest.stores":   "calls=[] unresolved=0", "TypesTest.storesElement": "calls=[acme.Store.add] unresolved=0",
			"TypesTest.stringsElement": "calls=[] unresolved=0", "TypesTest.arrayElement": "calls=[acme.Store.add] unresolved=0",
			"TypesTest.mapStore": "calls=[] unresolved=0", "TypesTest.mapStoreElement": "calls=[] unresolved=1",
			"TypesTest.pair": "calls=[] unresolved=0", "TypesTest.qualified": "calls=[] unresolved=0",
			"TypesTest.thing": "calls=[] unresolved=0", "TypesTest.any": "calls=[] unresolved=1",
			"TypesTest.box": "calls=[acme.Box.add] unresolved=0", "TypesTest.function": "calls=[] unresolved=0",
			"TypesTest.missing": "calls=[] unresolved=1",
		})
	})
}

// viewpoint: behaviour
// target: the Java reader
// A wildcard import of a package of the product brings in each of its classes, so a test in another
// package that types a local with one names its methods; a wildcard import of a class's members
// brings in no class.
func TestJavaWildcardImportsBringInThePackagesClasses(t *testing.T) {
	expectEach(t, kinds(t, "java",
		"src/main/java/acme/Store.java", "package acme;\n\npublic class Store {\n    public int add(int n) { return n; }\n}\n",
		"src/main/java/acme/Box.java", "package acme;\n\npublic class Box {\n    public static int size() { return 0; }\n}\n",
		"src/test/java/other/WildTest.java", `package other;

import acme.*;
import org.junit.jupiter.api.Test;

class WildTest {
    static <T> T load() { return null; }
    @Test void typed() { Store s = load(); s.add(1); }
}
`,
		"src/test/java/third/MembersTest.java", `package third;

import static acme.Box.*;
import org.junit.jupiter.api.Test;

class MembersTest {
    static <T> T load() { return null; }
    @Test void typed() { Store s = load(); s.add(1); }
}
`), map[string]string{
		"WildTest.typed":    "calls=[acme.Store.add] unresolved=0",
		"MembersTest.typed": "calls=[] unresolved=1",
	})
}

// viewpoint: behaviour
// target: the Java reader
// A local declared with var is what its value makes it: an object new makes of the product's class
// is that class, one of a class of the standard library or of outside is outside, and an element an
// index takes out of an array is the array's element type.
func TestJavaValuesDecideWhatALocalIs(t *testing.T) {
	expectEach(t, kinds(t, "java",
		"src/main/java/acme/Store.java", "package acme;\n\npublic class Store {\n    public int add(int n) { return n; }\n}\n",
		"src/test/java/acme/ValuesTest.java", `package acme;

import java.util.HashMap;
import com.ext.Thing;
import org.junit.jupiter.api.Test;

class ValuesTest {
    static <T> T load() { return null; }
    @Test void made() { var s = new Store(); s.add(1); }
    @Test void library() { var m = new HashMap<String, Integer>(); m.put("a", 1); }
    @Test void outside() { var x = new Thing(); x.go(); }
    @Test void indexed() { Store[] all = load(); var s = all[0]; s.add(1); }
}
`), map[string]string{
		"ValuesTest.made":    "calls=[acme.Store acme.Store.add] unresolved=0",
		"ValuesTest.library": "calls=[] unresolved=0",
		"ValuesTest.outside": "calls=[] unresolved=0",
		"ValuesTest.indexed": "calls=[acme.Store.add] unresolved=0",
	})
}
