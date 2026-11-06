//go:build cgo

package treesitter

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// viewpoint: behaviour
// target: the Java, Kotlin and TypeScript readers
// A test drives the application when it calls the entry of an HTTP test client its file imports:
// REST Assured's given(), MockMvc's perform(), supertest's request(). A function of the same name
// from anywhere else does not.
func TestHTTPTestClientsDriveTheApplication(t *testing.T) {
	drives := func(name string, files ...string) string {
		var out []string
		for _, ts := range analyze(t, name, tree(files...)).Tests {
			out = append(out, fmt.Sprintf("%s drives=%v", ts.Name, ts.Drives))
		}
		sort.Strings(out)
		return strings.Join(out, "\n")
	}
	t.Run("kotlin", func(t *testing.T) {
		expect(t, drives("kotlin",
			"src/main/kotlin/acme/App.kt", "package acme\n\nclass App\n",
			"src/test/kotlin/acme/AppTest.kt", `package acme

import io.restassured.RestAssured.given
import kotlin.test.Test

class AppTest {
    @Test
    fun robots() { given().get("/robots.txt").then().statusCode(200) }
}
`,
			"src/test/kotlin/acme/OtherTest.kt", `package acme

import kotlin.test.Test

class OtherTest {
    @Test
    fun local() { given() }

    private fun given() {}
}
`), "AppTest.robots drives=true\nOtherTest.local drives=false")
	})
	t.Run("java", func(t *testing.T) {
		expect(t, drives("java",
			"src/main/java/acme/App.java", "package acme;\n\npublic class App {}\n",
			"src/test/java/acme/AppTest.java", `package acme;

import org.junit.jupiter.api.Test;
import org.springframework.test.web.servlet.MockMvc;

class AppTest {
    private MockMvc mvc;

    @Test
    void rows() throws Exception { mvc.perform(get("/rows")).andExpect(status().isOk()); }
}
`), "AppTest.rows drives=true")
	})
	t.Run("typescript", func(t *testing.T) {
		expect(t, drives("typescript",
			"src/app.ts", "export const port = 3000\n",
			"src/app.test.ts", `import request from 'supertest'

it('serves', async () => { await request('http://localhost:3000').get('/rows').expect(200) })
`), "serves drives=true")
	})
}
