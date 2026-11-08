//go:build cgo

package engine_test

// treeSitterFiles maps a path in a language read through tree-sitter to the first line of a file.
var treeSitterFiles = map[string]string{
	"src/store.ts":         "export const rows = 1\n",
	"app/store.py":         "rows = 1\n",
	"src/store.rs":         "pub const ROWS: i32 = 1;\n",
	"src/main/java/A.java": "package app;\n",
	"src/main/kotlin/A.kt": "package app\n",
}
