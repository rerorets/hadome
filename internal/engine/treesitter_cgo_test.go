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

// treeSitterTop holds markers at and below the top of files in languages read through tree-sitter.
var treeSitterTop = []topCase{
	{"app/run.py", "#!/usr/bin/env python3\n# " + marker + "\nrows = 1\n", true},
	{"app/doc.py", "\"\"\"Rows.\"\"\"\n# " + marker + "\nrows = 1\n", false},
	{"src/license.ts", "/**\n * Copyright A.\n */\n// " + marker + "\nexport const rows = 1\n", true},
	{"src/main/kotlin/B.kt", "package app\n// " + marker + "\n", false},
}
