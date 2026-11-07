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
		// Base has a configuration: adding the first one introduces hadome, which is never loosening.
		{Name: "a product's own form is applied", Base: module("store/store.go", store(appendFunc), ".hadome.yml", "schema: 1\n"),
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
		{Name: "a Go String() string method passes", Base: with("\n// Rows lists rows.\ntype Rows []string\n"),
			Head: with("\n// Rows lists rows.\ntype Rows []string\n\nfunc (r Rows) String() string { return \"\" }\n")},
		{Name: "a method with the same name but another shape fails", Base: with("\n// Rows lists rows.\ntype Rows []string\n"),
			Head:     with("\n// Rows lists rows.\ntype Rows []string\n\nfunc (r Rows) Len() int64 { return 0 }\n"),
			WantFail: []string{"comments/missing-doc"}},
		{Name: "a new Terraform variable without a description fails", Base: tf(""), Head: tf("variable \"name\" {}\n"),
			WantFail: []string{"comments/missing-doc"}},
		{Name: "a new Terraform output with a description passes", Base: tf(""),
			Head: tf("output \"id\" {\n  description = \"The network's ID.\"\n  value       = 1\n}\n")},
		{Name: "a new Ansible default that argument_specs does not describe fails", Base: role(""), Head: role("web_user: www\n"),
			WantFail: []string{"comments/missing-doc"}},
		{Name: "a new Ansible default it describes passes", Base: role(""), Head: role("web_port: 80\n")},
		{Name: "a new Ansible default kept to the role passes", Base: role(""), Head: role("_web_cache: /tmp\n")},
	}
}

// role is an Ansible role whose argument_specs describes web_port, with these defaults.
func role(defaults string) fstest.MapFS {
	return fstest.MapFS{
		"roles/web/defaults/main.yml": {Data: []byte("---\n" + defaults)},
		"roles/web/meta/argument_specs.yml": {Data: []byte(
			"argument_specs:\n  main:\n    options:\n      web_port:\n        type: int\n        description: The port the server listens on.\n")},
	}
}

// tf is a Terraform module with one documented variable, and more configuration.
func tf(more string) fstest.MapFS {
	return module("modules/network/main.tf", "variable \"cidr\" {\n  description = \"The address range.\"\n}\n\n"+more)
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
		{Name: "a file name only, of a file elsewhere, passes", Base: module("store/store.go", store(appendFunc), doc, "# Store\n"),
			Head: module("store/store.go", refers("store.md"), doc, "# Store\n")},
		{Name: "a file name only, of a file nowhere, fails", Base: module("store/store.go", store(appendFunc), doc, "# Store\n"),
			Head: module("store/store.go", refers("rows.md"), doc, "# Store\n"), WantFail: []string{"comments/broken-reference"}},
		{Name: "a file name only does not count a file in a skipped directory",
			Base: module("store/store.go", store(appendFunc), "node_modules/lib/rows.md", ""),
			Head: module("store/store.go", refers("rows.md"), "node_modules/lib/rows.md", ""), WantFail: []string{"comments/broken-reference"}},
		{Name: "a reference after a Japanese bracket is found", Base: module("store/store.go", store(appendFunc)),
			Head:     module("store/store.go", store("// Append adds a row（docs/design/stroe.md）.\nfunc Append() {}\n")),
			WantFail: []string{"comments/broken-reference"}},
	}
}
