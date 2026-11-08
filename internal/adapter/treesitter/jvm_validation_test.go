//go:build cgo

package treesitter

import (
	"testing"
)

// viewpoint: behaviour
// target: the Java and Kotlin readers
// A REST Assured check chained on a helper declared to return a ValidatableResponse checks the
// response, whatever the helper's body ends with: then() followed by also(::log), or a call of
// another helper. A helper declared to return the request checks nothing.
func TestRestAssuredChecksThroughADeclaredValidatableResponse(t *testing.T) {
	t.Run("kotlin", func(t *testing.T) {
		expect(t, asserts(t, "kotlin", tree(
			"src/main/kotlin/acme/Store.kt", ktStore,
			"src/test/kotlin/acme/ApiSupport.kt", `package acme

import io.restassured.RestAssured.given
import io.restassured.response.ValidatableResponse
import io.restassured.specification.RequestSpecification

abstract class ApiSupport {
    protected fun list(): ValidatableResponse = given().get("/items").then().also(::log)

    protected fun request(): RequestSpecification = given().contentType("application/json").also(::log)

    protected fun ValidatableResponse.again(): RequestSpecification = given().also(::log)

    private fun log(x: Any) = println(x)
}
`,
			"src/test/kotlin/acme/ApiTest.kt", `package acme

import org.hamcrest.Matchers.equalTo
import org.junit.jupiter.api.Test

class ApiTest : ApiSupport() {
    @Test
    fun checked() { list().statusCode(200).body("size()", equalTo(0)) }

    @Test
    fun unchecked() { request().body("{}").post("/items") }

    @Test
    fun extension() { with(list()) { again().body("{}").post("/items") } }
}
`)), "ApiTest.checked asserts=2 unknown=false\nApiTest.extension asserts=0 unknown=false\nApiTest.unchecked asserts=0 unknown=false")
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
        ValidatableResponse r = given().get("/items").then();
        return r;
    }

    @Test
    void checked() { list().statusCode(200); }
}
`)), "ApiTest.checked asserts=1 unknown=false")
	})
}
