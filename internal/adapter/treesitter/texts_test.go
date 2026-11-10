//go:build cgo

package treesitter

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/config"
)

// texts renders the strings each test hands over, one line per string: the test, where the string
// is, its value, its holes, sql when it was passed to a call that takes SQL, dynamic when what was
// passed there is not a string, and how many times it is used.
func texts(t *testing.T, name string, tr fstest.MapFS, c config.Analysis) string {
	t.Helper()
	var out []string
	for _, ts := range analyzeWith(t, name, tr, c).Tests {
		for _, x := range ts.Texts {
			line := fmt.Sprintf("%s %s:%d %q", ts.Name, x.Path, x.Line, x.Value)
			if len(x.Holes) > 0 {
				line += fmt.Sprintf(" holes=%v", x.Holes)
			}
			if x.Sink != "" {
				line += " " + x.Sink
			}
			if x.Dynamic {
				line += " dynamic"
			}
			out = append(out, line+fmt.Sprintf(" x%d", x.Times))
		}
	}
	sort.Strings(out)
	return strings.Join(out, "\n")
}

// viewpoint: behaviour
// target: the TypeScript reader's test strings
// A test hands over the strings it holds: literals in its body, in beforeEach and in the helpers it
// follows, and the constants of its file it reads. Literals joined with + or written as a template
// are one string with holes where values go in. Strings passed to a call that takes SQL are marked,
// and a value that is not a string passed there is handed over as dynamic.
func TestTypeScriptTexts(t *testing.T) {
	got := texts(t, "typescript", tree(
		"src/store.ts", "export const label = 'not a test string';\n",
		"src/store.test.ts", `import { label } from './store';

const SQL = 'SELECT billing.is_active(' + '1)';
const NOT_READ = 'never read';

function insertRow(pool) {
  return pool.query("INSERT INTO accounts VALUES (1)");
}

describe('store', () => {
  beforeEach(() => {
    setup("TRUNCATE accounts");
  });

  it('reads', async () => {
    await insertRow(pool);
    const q = 'SELECT 1';
    await pool.query(q);
    await pool.query(SQL);
    await pool.query(buildSql(id));
    await db.$queryRaw`+"`SELECT * FROM t WHERE id = ${id}`"+`;
    const name = "a\n" + label + "b";
    for (const x of xs) { note('in loop'); }
    expect(label).toBe('x');
  });
});
`), config.Analysis{})
	want := `store reads src/store.test.ts:12 "TRUNCATE accounts" x1
store reads src/store.test.ts:17 "SELECT 1" sql x1
store reads src/store.test.ts:20 "" sql dynamic x1
store reads src/store.test.ts:21 "SELECT * FROM t WHERE id = " holes=[27] sql x1
store reads src/store.test.ts:22 "a\nb" holes=[2] x1
store reads src/store.test.ts:23 "in loop" x2
store reads src/store.test.ts:24 "x" x1
store reads src/store.test.ts:3 "SELECT billing.is_active(1)" sql x1
store reads src/store.test.ts:7 "INSERT INTO accounts VALUES (1)" sql x1`
	expect(t, got, want)
}

// viewpoint: behaviour
// target: the readers' test strings and rules.tests.sql-sinks
// A call configuration names as taking SQL marks the strings passed to it, by its last name or
// by the callee as written, and a value that is not a string passed to it is dynamic.
func TestTextsConfiguredSinks(t *testing.T) {
	c := config.Analysis{SQLSinks: map[string][]string{"typescript": {"runSql", "repo.raw"}}}
	got := texts(t, "typescript", tree("src/a.test.ts", `it('runs', () => {
  helpers.runSql("SELECT 1");
  repo.raw(text);
  other.raw("SELECT 2");
});
`), c)
	want := `runs src/a.test.ts:2 "SELECT 1" sql x1
runs src/a.test.ts:3 "" sql dynamic x1
runs src/a.test.ts:4 "SELECT 2" sql x1`
	expect(t, got, want)
	got = texts(t, "typescript", tree("src/a.test.ts", `it('runs', () => {
  helpers.runSql("SELECT 1");
  repo.raw(text);
});
`), config.Analysis{})
	expect(t, got, `runs src/a.test.ts:2 "SELECT 1" x1`)
}

// viewpoint: behaviour
// target: the TypeScript reader's test strings
// A call that takes SQL given another call that takes SQL, db.query(sql`…`), hands over the inner
// string only: the outer call's argument is not a value hadome cannot read.
func TestTextsNestedSinks(t *testing.T) {
	got := texts(t, "typescript", tree("src/a.test.ts", "it('runs', async () => {\n  await pool.query(sql`SELECT 1`);\n});\n"), config.Analysis{})
	expect(t, got, `runs src/a.test.ts:2 "SELECT 1" sql x1`)
}

// viewpoint: behaviour
// target: the TypeScript reader's test strings
// A string in a helper of a test support file that the test follows is the test's, at the place it
// is written, with the support file's constants.
func TestTextsInSupportFiles(t *testing.T) {
	got := texts(t, "typescript", tree(
		"test/helpers.ts", `const SEED = 'INSERT INTO accounts VALUES (1)';

export async function seed(pool) {
  await pool.query(SEED);
}
`,
		"src/a.test.ts", `import { seed } from '../test/helpers';

it('seeds', async () => {
  await seed(pool);
});
`), config.Analysis{})
	expect(t, got, `seeds test/helpers.ts:1 "INSERT INTO accounts VALUES (1)" sql x1`)
}

// viewpoint: behaviour
// target: the Python reader's test strings
// Python tests hand over strings, adjacent strings as one, f-strings with holes, module constants
// they read, and strings passed to DB-API calls marked as SQL.
func TestPythonTexts(t *testing.T) {
	got := texts(t, "python", tree(
		"tests/test_store.py", `QUERY = ("SELECT billing.is_active(1)"
         " AS ok")
UNUSED = "never"


def helper(cur):
    cur.execute("DELETE FROM accounts")


def test_reads(cur):
    helper(cur)
    cur.execute(QUERY)
    cur.execute(f"SELECT * FROM t WHERE id = {account_id}")
    sql = "SELECT 2"
    cur.execute(sql)
    cur.execute(build(account_id))
    raw = r"a\nb" + name
    for i in range(3):
        print("loop")
`), config.Analysis{})
	want := `test_reads tests/test_store.py:1 "SELECT billing.is_active(1) AS ok" sql x1
test_reads tests/test_store.py:13 "SELECT * FROM t WHERE id = " holes=[27] sql x1
test_reads tests/test_store.py:14 "SELECT 2" sql x1
test_reads tests/test_store.py:16 "" sql dynamic x1
test_reads tests/test_store.py:17 "a\\nb" holes=[4] x1
test_reads tests/test_store.py:19 "loop" x2
test_reads tests/test_store.py:7 "DELETE FROM accounts" sql x1`
	expect(t, got, want)
}

// viewpoint: behaviour
// target: the Python reader's test strings
// Where a name stands decides whether it reads a constant, for a name right below a loop too, whose
// place the walk hands down: the target of a for declares a name and reads nothing, the iterable
// reads the constant, once per run of the loop.
func TestPythonTextsOfNamesBelowALoop(t *testing.T) {
	got := texts(t, "python", tree(
		"tests/test_store.py", `QUERY = "SELECT 1"
ROWS = "abc"


def test_loops(cur):
    for QUERY in rows:
        pass
    for row in ROWS:
        pass
`), config.Analysis{})
	want := `test_loops tests/test_store.py:2 "abc" x2`
	expect(t, got, want)
}

// viewpoint: behaviour
// target: the TypeScript reader's test strings
// A test whose body is an expression, not a block, holds the strings of that expression: a
// constant it reads, and a literal joined into one string.
func TestTypeScriptTextsOfAnExpressionBody(t *testing.T) {
	got := texts(t, "typescript", tree(
		"src/store.test.ts", `const QUERY = "SELECT 1"

it('reads', () => QUERY)
it('joins', () => "SELECT " + "2")
`), config.Analysis{})
	want := `joins src/store.test.ts:4 "SELECT 2" x1
reads src/store.test.ts:1 "SELECT 1" x1`
	expect(t, got, want)
}

// viewpoint: behaviour
// target: the Rust reader's test strings
// Rust tests hand over plain and raw strings, strings in macro arguments, and constants of the file.
func TestRustTexts(t *testing.T) {
	got := texts(t, "rust", tree(
		"Cargo.toml", "[package]\nname = \"app\"\n",
		"src/lib.rs", "pub fn f() {}\n",
		"tests/store.rs", `const SQL: &str = r#"SELECT "x""#;

#[test]
fn reads() {
    let q = "SELECT 1";
    sqlx::query(q);
    sqlx::query!("SELECT billing.is_active($1)", id);
    sqlx::query(SQL);
    sqlx::query(&format!("SELECT {}", t));
}
`), config.Analysis{})
	want := `reads tests/store.rs:1 "SELECT \"x\"" sql x1
reads tests/store.rs:5 "SELECT 1" sql x1
reads tests/store.rs:7 "SELECT billing.is_active($1)" sql x1
reads tests/store.rs:9 "" sql dynamic x1
reads tests/store.rs:9 "SELECT {}" x1`
	expect(t, got, want)
}

// viewpoint: behaviour
// target: the Java reader's test strings
// Java tests hand over strings and text blocks of their own body, and static final constants they
// read; the strings of @BeforeEach are not the test's, as its calls are not.
func TestJavaTexts(t *testing.T) {
	got := texts(t, "java", tree(
		"src/test/java/com/acme/StoreTest.java", `package com.acme;

import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;

class StoreTest {
  private static final String SQL = "SELECT billing.is_active(?)" + " AS ok";

  @BeforeEach void setUp() { conn.prepareStatement("TRUNCATE accounts"); }

  @Test void reads() throws Exception {
    conn.prepareStatement(SQL);
    String block = """
        SELECT 1
        """;
    conn.prepareStatement("SELECT * FROM t WHERE id = " + id);
    conn.prepareStatement(build(id));
    executor.execute(task);
    jdbc.query("SELECT 2", mapper);
  }
}
`), config.Analysis{})
	want := `StoreTest.reads src/test/java/com/acme/StoreTest.java:13 "\n        SELECT 1\n        " x1
StoreTest.reads src/test/java/com/acme/StoreTest.java:16 "SELECT * FROM t WHERE id = " holes=[27] sql x1
StoreTest.reads src/test/java/com/acme/StoreTest.java:17 "" sql dynamic x1
StoreTest.reads src/test/java/com/acme/StoreTest.java:19 "SELECT 2" sql x1
StoreTest.reads src/test/java/com/acme/StoreTest.java:7 "SELECT billing.is_active(?) AS ok" sql x1`
	expect(t, got, want)
}

// viewpoint: behaviour
// target: the Kotlin reader's test strings
// Kotlin tests hand over strings and raw strings with holes at $name and ${…}, and const vals and
// companion constants they read.
func TestKotlinTexts(t *testing.T) {
	got := texts(t, "kotlin", tree(
		"src/test/kotlin/com/acme/StoreTest.kt", `package com.acme

import org.junit.jupiter.api.Test

const val SQL = "SELECT billing.is_active(?)"

class StoreTest {
  companion object { const val DELETE = "DELETE FROM accounts" }

  @Test fun reads() {
    conn.prepareStatement(SQL)
    conn.prepareStatement(DELETE)
    val q = """SELECT * FROM t WHERE id = ${id.value}"""
    conn.prepareStatement(q)
    conn.prepareStatement("SELECT $col FROM t")
    conn.prepareStatement(build(id))
    repeat(2) { println("x") }
  }
}
`), config.Analysis{})
	want := `StoreTest.reads src/test/kotlin/com/acme/StoreTest.kt:13 "SELECT * FROM t WHERE id = " holes=[27] sql x1
StoreTest.reads src/test/kotlin/com/acme/StoreTest.kt:15 "SELECT  FROM t" holes=[7] sql x1
StoreTest.reads src/test/kotlin/com/acme/StoreTest.kt:16 "" sql dynamic x1
StoreTest.reads src/test/kotlin/com/acme/StoreTest.kt:17 "x" x1
StoreTest.reads src/test/kotlin/com/acme/StoreTest.kt:5 "SELECT billing.is_active(?)" sql x1
StoreTest.reads src/test/kotlin/com/acme/StoreTest.kt:8 "DELETE FROM accounts" sql x1`
	expect(t, got, want)
}

// viewpoint: behaviour
// target: the readers' test strings
// Strings of product code are never handed over: only tests hold texts.
func TestTextsOnlyInTests(t *testing.T) {
	a := analyze(t, "typescript", tree("src/store.ts", "export function f(pool) { return pool.query('SELECT 1'); }\n"))
	if len(a.Tests) != 0 {
		t.Fatalf("product code has no tests, got %d", len(a.Tests))
	}
}
