//go:build cgo

package treesitter

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// viewpoint: behaviour
// target: the Rust reader
// The facts rules see from a crate: modules as units, use paths resolved through crate and super,
// pub and documented items, tests in #[cfg(test)] modules and under tests/, with ignore reasons.
func TestRustFacts(t *testing.T) {
	a := analyze(t, "rust", tree(
		"Cargo.toml", "[package]\nname = \"app\"\n",
		"src/lib.rs", "pub mod store;\nuse std::fs;\n",
		"src/store.rs", `use crate::util::trim;
use std::net::TcpStream;

/// Store keeps rows.
#[derive(Debug)]
pub struct Store { rows: Vec<String> }

pub trait Repo {
    fn get(&self);
    fn put(&self);
}

impl Store {
    pub fn new() -> Self { Store { rows: vec![] } }
    pub fn add(&mut self, row: &str) { if !row.is_empty() && row.len() > 1 { self.rows.push(row.into()) } }
    fn hidden(&self) {}
}

/// Creates a post.
pub fn create_post(user_id: &str, body: &str) -> usize { match body.len() { 0 => 0, _ => 1 } }

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn creates_twice() {
        create_post("u1", "a");
        create_post("u2", "a");
        let mut s = Store::new();
        s.add("x");
        std::thread::sleep(d);
        assert!(true);
    }

    #[test]
    #[ignore = "needs a database"]
    fn later() {
        for i in 0..2 { create_post("u1", "b"); }
        let thing = make();
        thing.run();
    }
}
`,
		"src/util.rs", "pub fn trim(s: &str) -> &str { s }\n",
		"tests/api.rs", `use app::store;
use crate::store::Store;

#[test]
#[ignore]
fn external() { crate::store::create_post("u1", "c"); }
`))
	expect(t, summary(a), `
file src/lib.rs source unit=src at=src
file src/store.rs source unit=src/store at=src/store
file src/util.rs source unit=src/util at=src/util
file tests/api.rs test unit=tests/api at=tests/api
import src -> std::fs internal=false io=true
import src/store -> src/util internal=true io=false
import src/store -> std::net internal=false io=true
symbol function src/store.create_post exported=true doc=true params=2 cx=2 methods=0 scoped=true
symbol function src/util.trim exported=true doc=false params=1 cx=1 methods=0 scoped=false
symbol interface src/store.Repo exported=true doc=false params=0 cx=0 methods=2 scoped=false
symbol method src/store.Repo.get exported=true doc=false params=0 cx=1 methods=0 scoped=false
symbol method src/store.Repo.put exported=true doc=false params=0 cx=1 methods=0 scoped=false
symbol method src/store.Store.add exported=true doc=false params=1 cx=3 methods=0 scoped=false
symbol method src/store.Store.hidden exported=false doc=false params=0 cx=1 methods=0 scoped=false
symbol method src/store.Store.new exported=true doc=false params=0 cx=1 methods=0 scoped=false
symbol type src/store.Store exported=true doc=true params=0 cx=0 methods=0 scoped=false
test "external" calls=[src/store.create_post×1/1] hazards=[] skip=true:"" unresolved=0
test "tests::creates_twice" calls=[src/store.Store.add×1/1 src/store.Store.new×1/0 src/store.create_post×2/2] hazards=[sleep always-true] skip=false:"" unresolved=0
test "tests::later" calls=[src/store.create_post×2/1] hazards=[] skip=true:"needs a database" unresolved=1
`)
}

// viewpoint: behaviour
// target: the Rust reader
// A type that mockall's #[automock] generates is a mock wherever a test uses it, although the test
// imports nothing that says mock. A type that only happens to start with Mock is not.
func TestRustAutomock(t *testing.T) {
	a := analyze(t, "rust", tree(
		"Cargo.toml", "[package]\nname = \"app\"\n",
		"src/lib.rs", `#[cfg_attr(test, mockall::automock)]
pub trait Store {
    fn get(&self) -> i32;
}

pub struct MockClock;

impl MockClock {
    pub fn new() -> Self { MockClock }
}

pub fn read(s: &dyn Store) -> i32 { s.get() }

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn generated() {
        let s = MockStore::new();
        assert_eq!(read(&s), 0);
    }

    #[test]
    fn handwritten() {
        let c = MockClock::new();
        let _ = c;
    }
}
`))
	got := map[string]string{}
	for _, ts := range a.Tests {
		var hs []string
		for _, h := range ts.Hazards {
			hs = append(hs, string(h.Category))
		}
		got[ts.Name] = strings.Join(hs, " ")
	}
	if got["tests::generated"] != "mock" || got["tests::handwritten"] != "" {
		t.Errorf("hazards: %v", got)
	}
}

// viewpoint: behaviour
// target: the Rust reader
// A function marked with a case of the test-case crate is a test, by its bare or full path, and so
// is one marked #[test_matrix] or #[rstest::rstest]. A result after => is checked when the case
// runs, so it counts as an assert. A case ending in => ignore or => inconclusive does not run: the
// test is skipped in some cases, with the reason in ignore["…"], and skipped when no case runs.
func TestRustTestCase(t *testing.T) {
	a := analyze(t, "rust", tree(
		"Cargo.toml", "[package]\nname = \"calc\"\n",
		"src/lib.rs", `pub fn double(a: i32) -> i32 { a * 2 }

#[cfg(test)]
mod tests {
    use super::*;
    use test_case::{test_case, test_matrix};

    #[test_case(1 ; "one")]
    #[test_case(2)]
    fn bare(a: i32) { let d = double(a); assert!(d > a); }

    #[test_case::test_case(1 => 2 ; "one")]
    #[test_case(2 => 4)]
    fn full(a: i32) -> i32 { double(a) }

    #[test_matrix([1, 2], [3, 4])]
    fn matrix(a: i32, b: i32) { double(a + b); }

    #[rstest::rstest]
    #[case(1)]
    fn rstest_full(#[case] a: i32) { double(a); }

    #[test_case(1 => 2)]
    #[test_case(2 => ignore["slow on large inputs"] 4)]
    fn some_ignored(a: i32) -> i32 { double(a) }

    #[test_case(1 => inconclusive 2)]
    fn none_runs(a: i32) -> i32 { double(a) }

    fn helper(a: i32) -> i32 { double(a) }
}
`))
	var got []string
	for _, ts := range a.Tests {
		skip := ""
		switch {
		case ts.Skip.Skipped:
			skip = fmt.Sprintf(" skip=%q", ts.Skip.Reason)
		case ts.Skip.Conditional:
			skip = fmt.Sprintf(" skipIf=%q", ts.Skip.Reason)
		}
		got = append(got, fmt.Sprintf("%s calls=%d asserts=%d%s", ts.Name, len(ts.Calls), ts.Asserts, skip))
	}
	sort.Strings(got)
	expect(t, strings.Join(got, "\n"), `
tests::bare calls=1 asserts=1
tests::full calls=1 asserts=2
tests::matrix calls=1 asserts=0
tests::none_runs calls=1 asserts=0 skip=""
tests::rstest_full calls=1 asserts=0
tests::some_ignored calls=1 asserts=1 skipIf="slow on large inputs"`)
}
