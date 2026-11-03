package comments

import (
	"testing/fstest"

	"github.com/rerorets/hadome/internal/engine"
)

// module is a Go module with the given files, keyed by path.
func module(files ...string) fstest.MapFS {
	t := fstest.MapFS{"go.mod": {Data: []byte("module example.com/app\n")}}
	for i := 0; i+1 < len(files); i += 2 {
		t[files[i]] = &fstest.MapFile{Data: []byte(files[i+1])}
	}
	return t
}

// store is the store package with body after its package clause.
func store(body string) string { return "// Package store keeps rows.\npackage store\n\n" + body }

const appendFunc = "// Append adds a row.\nfunc Append() {}\n"

func todoCases() []engine.Case {
	with := func(c string) fstest.MapFS { return module("store/store.go", store(appendFunc+c)) }
	return []engine.Case{
		{Name: "a new bare TODO fails", Base: with(""), Head: with("\n// TODO: handle errors\n"), WantFail: []string{"comments/todo-format"}},
		{Name: "a TODO with the form passes", Base: with(""), Head: with("\n// TODO(stage 2): handle errors\n")},
		{Name: "a FIXME fails", Base: with(""), Head: with("\n// FIXME later\n"), WantFail: []string{"comments/todo-format"}},
		{Name: "an existing bare TODO that moved passes", Base: with("\n// TODO: handle errors\n"),
			Head: with("\n\n\n// TODO: handle errors\n")},
		{Name: "TODO in the middle of a sentence passes", Base: with(""), Head: with("\n// The TODO list lives elsewhere.\n")},
		{Name: "a product's own form is applied", Base: with(""),
			Head: module("store/store.go", store(appendFunc+"\n// TODO(later): x\n"), ".hadome.yml",
				"schema: 1\nrules:\n  comments:\n    todo: '^TODO\\(stage[0-9]+\\): \\S'\n"),
			WantFail: []string{"comments/todo-format"}, WantReview: []string{engine.LoosenedID}},
	}
}

func docCases() []engine.Case {
	with := func(body string) fstest.MapFS { return module("store/store.go", store(appendFunc+body)) }
	return []engine.Case{
		{Name: "a new exported function without a doc comment fails", Base: with(""), Head: with("\nfunc Remove() {}\n"),
			WantFail: []string{"comments/missing-doc"}},
		{Name: "an unexported one passes", Base: with(""), Head: with("\nfunc remove() {}\n")},
		{Name: "a method on an unexported type passes", Base: with(""), Head: with("\ntype rows []string\n\nfunc (r rows) Len() int { return len(r) }\n")},
		{Name: "an exported type without one fails", Base: with(""), Head: with("\ntype Rows []string\n"),
			WantFail: []string{"comments/missing-doc"}},
		{Name: "a test file is not checked", Base: with(""),
			Head: module("store/store.go", store(appendFunc), "store/store_test.go", "package store\n\nfunc Helper() {}\n")},
	}
}

func referenceCases() []engine.Case {
	doc := "docs/design/store.md"
	refers := func(ref string) string { return store("// Append adds a row. See " + ref + ".\nfunc Append() {}\n") }
	return []engine.Case{
		{Name: "a reference to a missing file fails", Base: module("store/store.go", store(appendFunc)),
			Head: module("store/store.go", refers("docs/design/stroe.md")), WantFail: []string{"comments/broken-reference"}},
		{Name: "a reference to an existing file passes", Base: module("store/store.go", store(appendFunc), doc, "# Store\n"),
			Head: module("store/store.go", refers("docs/design/store.md#rows"), doc, "# Store\n")},
		{Name: "a relative reference resolves from the file", Base: module("store/store.go", store(appendFunc), "store/README.md", ""),
			Head: module("store/store.go", refers("./README.md"), "store/README.md", "")},
		{Name: "deleting a document that a comment names fails", Base: module("store/store.go", refers(doc), doc, "# Store\n"),
			Head: module("store/store.go", refers(doc)), WantFail: []string{"comments/broken-reference"}},
		{Name: "a directive is not a reference", Base: module("store/store.go", store(appendFunc)),
			Head: module("store/store.go", store(appendFunc+"\n//go:embed rows.md\nvar rows string\n"))},
		{Name: "a URL is not a reference", Base: module("store/store.go", store(appendFunc)),
			Head: module("store/store.go", refers("https://example.com/guide.md"))},
	}
}
