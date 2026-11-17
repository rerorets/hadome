//go:build cgo

package treesitter

import (
	"testing"
	"testing/fstest"
)

// viewpoint: behaviour
// target: treesitter.trace
// A file's code outside its functions has its own calls, each with the line of its first call:
// top-level statements, field initialisers and static values, and the objects made there. Calls in
// the functions' bodies, imports, declared names and Rust's test modules are not among them.
func TestCodeOutsideFunctionsHasItsOwnCalls(t *testing.T) {
	cases := []struct {
		lang, path string
		tree       fstest.MapFS
		want       string
	}{
		{"typescript", "src/state.ts", fstest.MapFS{
			"src/store.ts": {Data: []byte("export class Queue { push() { return 1; } }\nexport function open() { return 1; }\n")},
			"src/state.ts": {Data: []byte("import { Queue, open } from \"./store\";\n\nexport const queue = new Queue();\nqueue.push();\nopen();\n" +
				"export function later() { open(); }\n")},
		}, "Queue:3 open:5 push:4"},
		{"python", "app/state.py", fstest.MapFS{
			"app/__init__.py": {Data: []byte("")},
			"app/store.py":    {Data: []byte("class Queue:\n    def push(self):\n        return 1\n\ndef open_db():\n    return 1\n")},
			"app/state.py": {Data: []byte("from app.store import Queue, open_db\n\nqueue = Queue()\nqueue.push()\nopen_db()\n" +
				"\ndef later():\n    open_db()\n")},
		}, "Queue:3 open_db:5 push:4"},
		{"java", "src/main/java/app/web/State.java", fstest.MapFS{
			"src/main/java/app/Queue.java": {Data: []byte("package app;\npublic class Queue { public int push() { return 1; } }\n")},
			"src/main/java/app/web/State.java": {Data: []byte("package app.web;\nimport app.Queue;\npublic class State {\n" +
				"  private final Queue queue = new Queue();\n  private final int size = new Queue().push();\n" +
				"  public void later() { new Queue(); }\n}\n")},
		}, "Queue:4 push:5"},
		{"rust", "src/web.rs", fstest.MapFS{
			"Cargo.toml":   {Data: []byte("[package]\nname = \"app\"\n")},
			"src/lib.rs":   {Data: []byte("pub mod store;\npub mod web;\n")},
			"src/store.rs": {Data: []byte("pub const fn size() -> usize { 1 }\n")},
			"src/web.rs": {Data: []byte("use crate::store;\n\npub static SIZE: usize = store::size();\n\n" +
				"pub fn later() -> usize { store::size() }\n\n#[cfg(test)]\nmod tests {\n    fn t() { super::store::size(); }\n}\n")},
		}, "size:3"},
	}
	for _, c := range cases {
		t.Run(c.lang, func(t *testing.T) {
			a := analyze(t, c.lang, c.tree)
			for _, f := range a.Files {
				if f.Path == c.path {
					if got := callLinesOf(t, f.CallAt, f.Calls, f.Through); got != c.want {
						t.Errorf("got %q, want %q", got, c.want)
					}
					return
				}
			}
			t.Fatalf("no file %s", c.path)
		})
	}
}
