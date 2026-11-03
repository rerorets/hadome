// Package treesitter reads TypeScript, Python, Rust, Java and Kotlin with tree-sitter. Building it
// needs cgo; without cgo, the languages' files are listed as unread instead.
//
// Design: docs/design/adapters-treesitter.md
package treesitter

import (
	"path"
	"slices"
)

// extensions are the file extensions of each language this package reads.
var extensions = map[string][]string{
	"typescript": {".ts", ".tsx", ".mts", ".cts", ".js", ".jsx", ".mjs", ".cjs"},
	"python":     {".py"},
	"rust":       {".rs"},
	"java":       {".java"},
	"kotlin":     {".kt", ".kts"},
}

// names lists the languages in a fixed order.
var names = []string{"typescript", "python", "rust", "java", "kotlin"}

func claims(lang, p string) bool { return slices.Contains(extensions[lang], path.Ext(p)) }
