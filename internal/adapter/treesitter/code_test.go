// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

//go:build cgo

package treesitter

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/facts"
)

// symbolNamed returns the symbol whose ID ends in name, or fails the test.
func symbolNamed(t *testing.T, a *facts.Analysis, name string) facts.Symbol {
	t.Helper()
	var ids []string
	for _, s := range a.Symbols {
		ids = append(ids, s.ID)
		if s.ID == name || strings.HasSuffix(s.ID, "."+name) || strings.HasSuffix(s.ID, ":"+name) {
			return s
		}
	}
	t.Fatalf("no symbol %s among %v", name, ids)
	return facts.Symbol{}
}

// codeFiles adds what a language needs around one source file.
func codeFiles(lang, path, src string) []string {
	files := []string{path, src}
	if lang == "rust" {
		files = append(files, "Cargo.toml", "[package]\nname = \"app\"\n")
	}
	return files
}

func kindsOf(ls []facts.Literal) string {
	var out []string
	for _, l := range ls {
		out = append(out, fmt.Sprintf("%s@%d", l.Kind, l.Line))
	}
	return strings.Join(out, " ")
}

// viewpoint: behaviour
// target: treesitter.lang.Analyze
// In every language, a catch whose block is empty or holds only comments, and an except of every
// error whose body is only pass, drops the error; a catch that does anything with it does not, nor
// one whose error is named to say it is dropped on purpose (_, ignored), nor an except of a narrower
// error. In TypeScript an empty promise .catch(() => {}) drops it too, and in Rust an .ok() whose
// value is unused.
func TestDroppedErrorsInEveryLanguage(t *testing.T) {
	cases := []struct{ lang, path, src string }{
		{"typescript", "src/run.ts", `export function run(save: () => void, log: (e: unknown) => void) {
  try { save(); } catch (e) {}
  try { save(); } catch {
    // nothing to do
  }
  try { save(); } catch (e) { log(e); }
  fetch("/x").catch(() => {});
  fetch("/x").catch((e) => log(e));
  try { save(); } catch (_ignored) {}
}
`},
		{"python", "app/run.py", `def run(save, log):
    try:
        save()
    except:
        pass
    try:
        save()
    except Exception:
        # nothing to do
        ...
    try:
        save()
    except Exception as e:
        log(e)
    try:
        save()
    except KeyError:
        pass
    try:
        save()
    except (ValueError, Exception) as _:
        pass
`},
		{"java", "src/main/java/app/Run.java", `package app;

public class Run {
    public void run(Runnable save) {
        try { save.run(); } catch (RuntimeException e) {}
        try { save.run(); } catch (RuntimeException e) {
            // nothing to do
        }
        try { save.run(); } catch (RuntimeException e) { System.err.println(e); }
        try { save.run(); } catch (RuntimeException ignored) {}
    }
}
`},
		{"kotlin", "src/main/kotlin/app/Run.kt", `package app

fun run(save: () -> Unit) {
    try { save() } catch (e: Exception) {}
    try { save() } catch (e: Exception) {
        // nothing to do
    }
    try { save() } catch (e: Exception) { println(e) }
    try { save() } catch (_: Exception) {}
}
`},
		{"rust", "src/lib.rs", `pub fn run(tx: std::sync::mpsc::Sender<i32>) {
    tx.send(1).ok();
    let sent = tx.send(2).ok();
    if sent.is_none() {
        tx.send(3).ok();
    }
}
`},
	}
	want := map[string]string{"typescript": "2 3 7", "python": "4 8", "java": "5 6", "kotlin": "4 5", "rust": "2 5"}
	for _, c := range cases {
		t.Run(c.lang, func(t *testing.T) {
			s := symbolNamed(t, analyze(t, c.lang, tree(codeFiles(c.lang, c.path, c.src)...)), "run")
			var lines []string
			for _, w := range s.Swallows {
				lines = append(lines, fmt.Sprint(w.Line))
			}
			if got := strings.Join(lines, " "); got != want[c.lang] {
				t.Errorf("got dropped errors at lines %q, want %q: %v", got, want[c.lang], s.Swallows)
			}
		})
	}
}

// viewpoint: behaviour
// target: treesitter.lang.Analyze
// In every language, a function's body finds a URL with a host, a duration built from a number,
// a port, and a literal given to a name the configuration lists; a path without a host, a URL in a
// sentence and a string with something interpolated are none of them.
func TestConfigurationValuesInEveryLanguage(t *testing.T) {
	cases := []struct{ lang, path, src, want string }{
		{"typescript", "src/client.ts", `export function connect(name: string) {
  const a = fetch("https://api.shop.internal/v1/orders");
  const b = fetch("/v1/orders");
  const c = fetch(` + "`https://${name}/v1`" + `);
  const d = new Error("see https://example.com/docs");
  const e = { timeout: 5000, pageSize: 50, label: "x" };
  app.listen(8080);
  return [a, b, c, d, e];
}
`, "url@2 duration@6 named@6 port@7"},
		{"python", "app/client.py", `def connect(name):
    a = get("https://api.shop.internal/v1/orders")
    b = get("/v1/orders")
    c = get(f"https://{name}/v1")
    d = timedelta(seconds=30)
    e = query(page_size=50, label="x")
    app.run(port=8080)
    return a, b, c, d, e
`, "url@2 duration@5 named@6 port@7"},
		{"java", "src/main/java/app/Client.java", `package app;

public class Client {
    int pageSize;
    public Object connect(String name) {
        Object a = get("https://api.shop.internal/v1/orders");
        Object b = get("/v1/orders");
        Object c = java.time.Duration.ofSeconds(30);
        Object d = Duration.ofSeconds(30);
        this.pageSize = 50;
        Object e = new ServerSocket(8080);
        return a;
    }
}
`, "url@6 duration@8 duration@9 named@10 port@11"},
		{"kotlin", "src/main/kotlin/app/Client.kt", `package app

fun connect(name: String): Any {
    val a = get("https://api.shop.internal/v1/orders")
    val b = get("/v1/orders")
    val c = get("https://${name}/v1")
    val d = 30.seconds
    val e = query(pageSize = 50, label = "x")
    val f = embeddedServer(Netty, port = 8080)
    return a
}
`, "url@4 duration@7 named@8 port@9"},
		{"rust", "src/lib.rs", `const BASE: &str = "https://api.shop.internal";

pub fn connect(name: &str) -> Client {
    const LOCAL: &str = "https://api.shop.internal/v2";
    let a = get("https://api.shop.internal/v1/orders");
    let b = get("/v1/orders");
    let d = Duration::from_secs(30);
    Client { page_size: 50, label: "x" }
}
`, "url@5 duration@7 named@8"},
	}
	c := config.Analysis{ConfigNames: []string{"page_size"}}
	for _, tc := range cases {
		t.Run(tc.lang, func(t *testing.T) {
			s := symbolNamed(t, analyzeWith(t, tc.lang, tree(codeFiles(tc.lang, tc.path, tc.src)...), c), "connect")
			if got := kindsOf(s.Literals); got != tc.want {
				t.Errorf("got %q, want %q: %v", got, tc.want, s.Literals)
			}
		})
	}
}

// viewpoint: behaviour
// target: treesitter.lang.Analyze
// In every language, a URL that is compared with a value or given to a call that compares text, and
// a duration added to a time or given to a call that moves one, are recognised or calculated with,
// not settings; a URL joined with + and a duration given to anything else still count.
func TestValuesComparedOrCalculatedWithInEveryLanguage(t *testing.T) {
	cases := []struct{ lang, path, src, want string }{
		{"typescript", "src/client.ts", `export function connect(u: string, now: number) {
  if (u.startsWith("https://git.shop.internal/")) return 1;
  if (u === "https://git.shop.internal/") return 2;
  const a = fetch("https://api.shop.internal" + u);
  if (u === /* old host */ "https://old.shop.internal/") return 3;
  if (u.startsWith?.("https://git.shop.internal/")) return 4;
  const b = fetch("{api}/v1".replace("{api}", "https://api.shop.internal"));
  return a;
}
`, "url@4 url@7"},
		{"python", "app/client.py", `def connect(u, now):
    if u.startswith("https://git.shop.internal/"):
        return now - timedelta(days=6)
    if u == "https://git.shop.internal/":
        return now + timedelta(hours=1)
    if "https://a.shop.internal" < u < "https://z.shop.internal":
        return now
    return wait(timedelta(seconds=30) + timedelta(seconds=5))
`, "duration@8 duration@8"},
		{"java", "src/main/java/app/Client.java", `package app;

public class Client {
    public Object connect(String u, Instant now) {
        if (u.startsWith("https://git.shop.internal/")) return now.minus(Duration.ofDays(7));
        Object s = new java.net.ServerSocket(config.port(), 50);
        Object a = new InetSocketAddress("db.shop.internal", 5432);
        return client.timeout(Duration.ofSeconds(30));
    }
}
`, "port@7 duration@8"},
		{"kotlin", "src/main/kotlin/app/Client.kt", `package app

fun connect(u: String, now: Instant): Any {
    if (u.startsWith("https://git.shop.internal/")) return now + 7.days
    return withTimeout(30.seconds) { get(u) }
}
`, "duration@5"},
		{"rust", "src/lib.rs", `pub fn connect(u: &str, now: Instant) -> Option<Instant> {
    let rest = u.strip_prefix("https://git.shop.internal/")?;
    let path = u.trim_start_matches("https://git.shop.internal/");
    if u == "https://git.shop.internal/" {
        return now.checked_add(Duration::from_secs(30));
    }
    Some(now - Duration::from_secs(60))
}
`, "duration@5"},
	}
	for _, tc := range cases {
		t.Run(tc.lang, func(t *testing.T) {
			s := symbolNamed(t, analyzeWith(t, tc.lang, tree(codeFiles(tc.lang, tc.path, tc.src)...), config.Analysis{}), "connect")
			if got := kindsOf(s.Literals); got != tc.want {
				t.Errorf("got %q, want %q: %v", got, tc.want, s.Literals)
			}
		})
	}
}

// viewpoint: behaviour
// target: treesitter.lang.Analyze
// In every language, two functions that differ only in their names and the names of their
// parameters and local variables have the same Body; a different literal, or a different member
// read, makes another one.
func TestBodiesIgnoreLocalNamesInEveryLanguage(t *testing.T) {
	cases := []struct{ lang, path, src string }{
		{"typescript", "src/round.ts", `export function a(total: number) { const scale = 10; return Math.round(total * scale) / scale + this.total; }
export function b(sum: number) { const f = 10; return Math.round(sum * f) / f + this.total; }
export function c(sum: number) { const f = 100; return Math.round(sum * f) / f + this.total; }
export function d(sum: number) { const f = 10; return Math.round(sum * f) / f + this.sum; }
`},
		{"python", "app/round.py", `def a(self, total):
    scale = 10
    return round(total * scale) / scale + self.total
def b(self, s):
    f = 10
    return round(s * f) / f + self.total
def c(self, s):
    f = 100
    return round(s * f) / f + self.total
def d(self, s):
    f = 10
    return round(s * f) / f + self.s
`},
		{"java", "src/main/java/app/Round.java", `package app;

public class Round {
    double total;
    public double a(double total) { double scale = 10; return Math.round(total * scale) / scale + this.total; }
    public double b(double sum) { double f = 10; return Math.round(sum * f) / f + this.total; }
    public double c(double sum) { double f = 100; return Math.round(sum * f) / f + this.total; }
    public double d(double sum) { double f = 10; return Math.round(sum * f) / f + this.sum; }
}
`},
		{"kotlin", "src/main/kotlin/app/Round.kt", `package app

class Round(val total: Double, val sum: Double) {
    fun a(total: Double): Double { val scale = 10; return Math.round(total * scale) / scale + this.total }
    fun b(sum: Double): Double { val f = 10; return Math.round(sum * f) / f + this.total }
    fun c(sum: Double): Double { val f = 100; return Math.round(sum * f) / f + this.total }
    fun d(sum: Double): Double { val f = 10; return Math.round(sum * f) / f + this.sum }
}
`},
		{"rust", "src/lib.rs", `pub struct R { total: f64, sum: f64 }
impl R {
    pub fn a(&self, total: f64) -> f64 { let scale = 10.0; (total * scale).round() / scale + self.total }
    pub fn b(&self, sum: f64) -> f64 { let f = 10.0; (sum * f).round() / f + self.total }
    pub fn c(&self, sum: f64) -> f64 { let f = 100.0; (sum * f).round() / f + self.total }
    pub fn d(&self, sum: f64) -> f64 { let f = 10.0; (sum * f).round() / f + self.sum }
}
`},
	}
	for _, c := range cases {
		t.Run(c.lang, func(t *testing.T) {
			an := analyze(t, c.lang, tree(codeFiles(c.lang, c.path, c.src)...))
			a, b, cc, d := symbolNamed(t, an, "a"), symbolNamed(t, an, "b"), symbolNamed(t, an, "c"), symbolNamed(t, an, "d")
			if a.Body == "" || a.BodyTokens < 10 {
				t.Fatalf("a has no fingerprint: %q, %d tokens", a.Body, a.BodyTokens)
			}
			if a.Body != b.Body {
				t.Errorf("a and b differ only in local names but have different bodies")
			}
			if a.Body == cc.Body {
				t.Errorf("c has another literal but the same body as a")
			}
			if a.Body == d.Body {
				t.Errorf("d reads another member but has the same body as a")
			}
		})
	}
}

// viewpoint: behaviour
// target: treesitter.lang.Analyze
// A Rust module compiled only for tests, #[cfg(test)] or #[cfg(all(test, …))], is test code: the
// code/ rules read nothing in it. A module under #[cfg(any(test, …))] is compiled without tests too,
// so it is product code.
func TestRustTestModulesAreNotProductCode(t *testing.T) {
	src := `pub fn keep() {
    std::fs::remove_file("a").ok();
}

#[cfg(test)]
mod tests {
    fn clean() { std::fs::remove_file("b").ok(); }
}

#[cfg(all(test, target_os = "macos"))]
mod mac {
    fn clean() { std::fs::remove_file("c").ok(); }
}

#[cfg(any(test, feature = "tools"))]
mod tools {
    pub fn clean() { std::fs::remove_file("d").ok(); }
}
`
	a := analyzeWith(t, "rust", tree(codeFiles("rust", "src/lib.rs", src)...), config.Analysis{})
	var got []string
	for _, s := range a.Symbols {
		for _, w := range s.Swallows {
			got = append(got, fmt.Sprint(w.Line))
		}
	}
	sort.Strings(got)
	if strings.Join(got, " ") != "17 2" {
		t.Errorf("got dropped errors at lines %v, want 2 and 17 only", got)
	}
}

// viewpoint: behaviour
// target: treesitter.lang.Analyze
// A .catch that drops an error is placed at .catch, not at the start of the chain before it, and
// its evidence starts there.
func TestDroppedPromiseErrorIsPlacedAtCatch(t *testing.T) {
	src := `export function load() {
  return fetchShops({
    page: 1,
  })
    .then(render)
    .catch(() => {});
}
`
	s := symbolNamed(t, analyzeWith(t, "typescript", tree(codeFiles("typescript", "src/load.ts", src)...), config.Analysis{}), "load")
	if len(s.Swallows) != 1 || s.Swallows[0].Line != 6 || s.Swallows[0].Evidence != ".catch(() => {})" {
		t.Errorf("got %v, want one dropped error at line 6 with evidence .catch(() => {})", s.Swallows)
	}
}

// viewpoint: behaviour
// target: treesitter.lang.Analyze
// At the edges of the shapes: a function assigned to a name is read like any other; a duration or a
// port of zero is none, nor one built from a value that is not a number written out; Kotlin's
// arguments are read through their wrappers; a literal given to a name the configuration lists counts
// unless something is interpolated into it or it is no literal at all.
func TestValuesAtTheEdgesOfTheirShapesInEveryLanguage(t *testing.T) {
	cases := []struct{ lang, path, src, want string }{
		{"typescript", "src/client.ts", `export const connect = (name: string) => {
  const a = { bucket: "uploads" };
  const b = { bucket: ` + "`up-${name}`" + ` };
  const c = { bucket: name };
  const d = AbortSignal.timeout(0);
  return [a, b, c, d];
};
`, "named@2"},
		{"python", "app/client.py", `def connect(n):
    a = timedelta(0)
    b = timedelta(seconds=n)
    return a, b
`, ""},
		{"java", "src/main/java/app/Client.java", `package app;

public class Client {
    public Object connect(int n) {
        Object a = Duration.ofSeconds(0);
        Object b = Duration.ofSeconds(n);
        Object c = new ServerSocket(0);
        Object d = Duration.ofSeconds(30, n);
        return a;
    }
}
`, ""},
		{"kotlin", "src/main/kotlin/app/Client.kt", `package app

fun connect(n: Long): Any {
    val a = Duration.ofSeconds(30)
    val b = ServerSocket(8080)
    val c = Duration.ofSeconds(n)
    return a
}
`, "duration@4 port@5"},
		{"rust", "src/lib.rs", `pub fn connect(n: u64) -> Duration {
    let a = Duration::from_secs(0);
    Duration::from_secs(n)
}
`, ""},
	}
	c := config.Analysis{ConfigNames: []string{"bucket"}}
	for _, tc := range cases {
		t.Run(tc.lang, func(t *testing.T) {
			s := symbolNamed(t, analyzeWith(t, tc.lang, tree(codeFiles(tc.lang, tc.path, tc.src)...), c), "connect")
			if got := kindsOf(s.Literals); got != tc.want {
				t.Errorf("got %q, want %q: %v", got, tc.want, s.Literals)
			}
		})
	}
}

// viewpoint: behaviour
// target: treesitter.lang.Analyze
// What a declaration writes before its parameters is part of its body: Java methods that differ only
// in their result type, or TypeScript functions only in their type parameters, have different bodies.
func TestBodiesKeepWhatIsWrittenBeforeTheParameters(t *testing.T) {
	cases := []struct{ lang, path, src string }{
		{"java", "src/main/java/app/Round.java", `package app;

public class Round {
    Object a(Object x) { return convert(x, 10, "round", "half-up", 2, true); }
    String b(Object x) { return convert(x, 10, "round", "half-up", 2, true); }
}
`},
		{"typescript", "src/round.ts", `export function a<T>(x: T) { return convert(x, 10, "round", "half-up", 2, true); }
export function b<T extends string>(x: T) { return convert(x, 10, "round", "half-up", 2, true); }
`},
	}
	for _, tc := range cases {
		t.Run(tc.lang, func(t *testing.T) {
			an := analyzeWith(t, tc.lang, tree(codeFiles(tc.lang, tc.path, tc.src)...), config.Analysis{})
			a, b := symbolNamed(t, an, "a"), symbolNamed(t, an, "b")
			if a.Body == "" || a.Body == b.Body {
				t.Errorf("a and b differ before their parameters but have the same body %q", a.Body)
			}
		})
	}
}

// viewpoint: behaviour
// target: treesitter.lang.Analyze
// A catch whose body did not parse drops nothing that can be told.
func TestCatchWithoutABodyDropsNothing(t *testing.T) {
	src := `export function load() {
  try { run() } catch (e)
}
`
	s := symbolNamed(t, analyzeWith(t, "typescript", tree(codeFiles("typescript", "src/load.ts", src)...), config.Analysis{}), "load")
	if len(s.Swallows) != 0 {
		t.Errorf("got %v, want no dropped error", s.Swallows)
	}
}

// viewpoint: behaviour
// target: treesitter.lang.Analyze
// Python parameters with a type, a default, or both, and splat parameters, are local names: two
// functions that differ only in them have the same body.
func TestPythonParametersOfEveryKindAreLocalNames(t *testing.T) {
	src := `def a(total: float, scale=10, digits: int = 2, *rest, **opts):
    return round(total * scale, digits) + len(rest) + len(opts)
def b(s: float, f=10, d: int = 2, *r, **o):
    return round(s * f, d) + len(r) + len(o)
`
	an := analyzeWith(t, "python", tree(codeFiles("python", "app/round.py", src)...), config.Analysis{})
	if a, b := symbolNamed(t, an, "a"), symbolNamed(t, an, "b"); a.Body == "" || a.Body != b.Body {
		t.Errorf("a and b differ only in their parameters' names but have different bodies")
	}
}

// viewpoint: behaviour
// target: treesitter.lang.Analyze
// In a TypeScript destructuring pattern, the names it declares are local, but the key of a renamed
// property and the default value it gives are not: they keep the body apart.
func TestTypeScriptPatternsDeclareOnlyTheirNames(t *testing.T) {
	src := `export function a({ [field]: t = base }: Order, n: number) { return t * n + scale(t) + scale(n) + 1; }
export function b({ [field]: x = base }: Order, m: number) { return x * m + scale(x) + scale(m) + 1; }
export function c({ [other]: x = base }: Order, m: number) { return x * m + scale(x) + scale(m) + 1; }
export function d({ [field]: x = other }: Order, m: number) { return x * m + scale(x) + scale(m) + 1; }
`
	an := analyzeWith(t, "typescript", tree(codeFiles("typescript", "src/order.ts", src)...), config.Analysis{})
	body := func(name string) string { return symbolNamed(t, an, name).Body }
	if body("a") == "" || body("a") != body("b") {
		t.Errorf("a and b differ only in local names but have different bodies")
	}
	if body("a") == body("c") {
		t.Errorf("c destructures another key but has the body of a")
	}
	if body("a") == body("d") {
		t.Errorf("d gives another default value but has the body of a")
	}
}

// viewpoint: behaviour
// target: treesitter.lang.Analyze
// In a Rust pattern the variables it binds are local, but the path of a tuple struct or a struct it
// matches is not: matching another variant or struct keeps the body apart.
func TestRustPatternsDeclareOnlyTheirVariables(t *testing.T) {
	src := `pub fn a(o: Item) -> u64 { let Some(v) = o.first() else { return 0 }; let Point { x, y } = v; x * y + scale(x) + 1 }
pub fn b(p: Item) -> u64 { let Some(w) = p.first() else { return 0 }; let Point { x, y } = w; x * y + scale(x) + 1 }
pub fn c(p: Item) -> u64 { let Ok(w) = p.first() else { return 0 }; let Point { x, y } = w; x * y + scale(x) + 1 }
pub fn d(p: Item) -> u64 { let Some(w) = p.first() else { return 0 }; let Size { x, y } = w; x * y + scale(x) + 1 }
pub fn e(p: Item) -> u64 { let Some(w) = p.first() else { return 0 }; let shapes::Point { x, y } = w; x * y + scale(x) + 1 }
pub fn f(p: Item) -> u64 { let Some(w) = p.first() else { return 0 }; let sizes::Point { x, y } = w; x * y + scale(x) + 1 }
`
	an := analyzeWith(t, "rust", tree(codeFiles("rust", "src/lib.rs", src)...), config.Analysis{})
	body := func(name string) string { return symbolNamed(t, an, name).Body }
	if body("a") == "" || body("a") != body("b") {
		t.Errorf("a and b differ only in local names but have different bodies")
	}
	if body("a") == body("c") {
		t.Errorf("c matches another variant but has the body of a")
	}
	if body("a") == body("d") {
		t.Errorf("d matches another struct but has the body of a")
	}
	if body("e") == body("f") {
		t.Errorf("f matches a struct of another module but has the body of e")
	}
}

// viewpoint: behaviour
// target: treesitter.lang.Analyze
// The name a Python error is caught as is not the class caught: except KeyError as Exception catches
// a KeyError only, so passing on it drops nothing broad.
func TestPythonCatchesByClassNotByAlias(t *testing.T) {
	src := `def load(d):
    try:
        return d["k"]
    except KeyError as Exception:
        pass
`
	s := symbolNamed(t, analyzeWith(t, "python", tree(codeFiles("python", "app/load.py", src)...), config.Analysis{}), "load")
	if len(s.Swallows) != 0 {
		t.Errorf("got %v, want no dropped error", s.Swallows)
	}
}
