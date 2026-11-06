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
// The checks of an HTTP test library count as asserts though their names do not start with assert:
// each call after REST Assured's then(), and MockMvc's andExpect. A call named like a check that
// does not follow then() checks nothing.
func TestHTTPChecksCountAsAsserts(t *testing.T) {
	count := func(name string, files ...string) string {
		var out []string
		for _, ts := range analyze(t, name, tree(files...)).Tests {
			out = append(out, fmt.Sprintf("%s asserts=%d", ts.Name, ts.Asserts))
		}
		sort.Strings(out)
		return strings.Join(out, "\n")
	}
	t.Run("kotlin", func(t *testing.T) {
		expect(t, count("kotlin",
			"src/main/kotlin/acme/Api.kt", "package acme\n\nfun path(): String = \"/rows\"\n",
			"src/test/kotlin/acme/ApiTest.kt", `package acme

import io.restassured.RestAssured.given
import kotlin.test.Test

class ApiTest {
    @Test
    fun rows() {
        given().`+"`when`"+`().get(path())
            .then()
            .statusCode(200)
            .body("rows.size()", equalTo(2))
    }

    @Test
    fun unchecked() {
        given().`+"`when`"+`().get(path()).body("ignored")
    }

    @Test
    fun mvc() {
        mockMvc.perform(get(path())).andExpect(status().isOk())
    }
}
`), "ApiTest.mvc asserts=1\nApiTest.rows asserts=2\nApiTest.unchecked asserts=0")
	})
	t.Run("java", func(t *testing.T) {
		expect(t, count("java",
			"src/main/java/acme/Api.java", "package acme;\n\npublic class Api {\n    public static String path() { return \"/rows\"; }\n}\n",
			"src/test/java/acme/ApiTest.java", `package acme;

import static io.restassured.RestAssured.given;

import org.junit.jupiter.api.Test;

class ApiTest {
    @Test
    void rows() {
        given().when().get(Api.path()).then().statusCode(200).contentType("application/json");
    }
}
`), "ApiTest.rows asserts=2")
	})
}
