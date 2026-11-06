//go:build cgo

package treesitter

import "testing"

// viewpoint: behaviour
// target: the Rust reader
// Code outside the library (an integration test under tests/) calls the library by the crate's
// name: [package] name with - read as _, or [lib] name when set. Such paths are the repository's,
// through a use declaration or written in full. Another crate's name stays external.
func TestIntegrationTestsCallTheLibraryByTheCrateName(t *testing.T) {
	lib := "/// Store keeps rows.\npub struct Store;\n\nimpl Store {\n    /// new makes one.\n    pub fn new() -> Self { Store }\n}\n\n/// double doubles n.\npub fn double(n: i32) -> i32 { n * 2 }\n"
	tests := `use my_store::Store;

#[test]
fn via_use() { let _s = Store::new(); }

#[test]
fn in_full() { let n = my_store::double(2); assert_eq!(n, 4); }

#[test]
fn other_crate() { let s = serde_json::to_string(&1).unwrap(); assert_eq!(s, "1"); }
`
	t.Run("package name", func(t *testing.T) {
		expect(t, calls(t, "rust",
			"Cargo.toml", "[package]\nname = \"my-store\"\nversion = \"0.1.0\"\n",
			"src/lib.rs", lib,
			"tests/store.rs", tests), `in_full calls=[src.double] unresolved=0
other_crate calls=[] unresolved=0
via_use calls=[src.Store.new] unresolved=0`)
	})
	t.Run("lib name", func(t *testing.T) {
		expect(t, calls(t, "rust",
			"Cargo.toml", "[package]\nname = \"store-server\"\n\n[lib]\nname = \"my_store\"\n",
			"src/lib.rs", lib,
			"tests/store.rs", tests), `in_full calls=[src.double] unresolved=0
other_crate calls=[] unresolved=0
via_use calls=[src.Store.new] unresolved=0`)
	})
}
