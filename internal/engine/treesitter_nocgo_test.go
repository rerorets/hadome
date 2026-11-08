//go:build !cgo

package engine_test

// treeSitterFiles is empty without cgo: hadome reads these languages only through tree-sitter.
var treeSitterFiles = map[string]string{}
