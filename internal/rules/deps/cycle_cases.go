// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

package deps

import (
	"testing/fstest"

	"github.com/rerorets/hadome/internal/engine"
)

// languageCycleCases are cases in languages only a cgo build reads; see cycle_cases_cgo.go.
var languageCycleCases []engine.Case

// tree is a repository of the given files, keyed by path.
func tree(files ...string) fstest.MapFS {
	t := fstest.MapFS{}
	for i := 0; i+1 < len(files); i += 2 {
		t[files[i]] = &fstest.MapFile{Data: []byte(files[i+1])}
	}
	return t
}

// goApp is a Go module with the units domain, store, which imports domain, and api, and extra
// files, keyed by path.
func goApp(extra ...string) fstest.MapFS {
	return tree(append([]string{
		"go.mod", "module example.com/app\n",
		"domain/domain.go", "package domain\n",
		"store/store.go", "package store\n\nimport _ \"example.com/app/domain\"\n",
		"api/api.go", "package api\n",
	}, extra...)...)
}

func cycleCases() []engine.Case {
	imports := func(pkg string, paths ...string) string {
		s := "package " + pkg + "\n\nimport (\n"
		for _, p := range paths {
			s += "\t_ \"example.com/app/" + p + "\"\n"
		}
		return s + ")\n"
	}
	back := imports("domain", "store")
	inTest := "package domain\n\nimport (\n\t\"testing\"\n\n\t_ \"example.com/app/store\"\n)\n\nfunc TestX(t *testing.T) {}\n"
	withDiagram := func(t fstest.MapFS) fstest.MapFS {
		t[".hadome.yml"] = &fstest.MapFile{Data: []byte("schema: 1\nrules:\n  deps:\n    diagram: docs/deps.md\n")}
		t["docs/deps.md"] = &fstest.MapFile{Data: []byte("```mermaid\nflowchart LR\n  all[\"...\"]\n```\n")}
		return t
	}
	allow := func(t fstest.MapFS) fstest.MapFS {
		t[".hadome.yml"] = &fstest.MapFile{Data: []byte("schema: 1\nrules:\n  deps:\n    cycles:\n      allow: [\"store <-> domain/**\"]\n")}
		return t
	}
	out := []engine.Case{
		{Name: "a new import closing a cycle fails", Base: goApp(), Head: goApp("domain/domain.go", back),
			WantFail: []string{"deps/cycle"}},
		{Name: "a new import closing a cycle through three units fails", Base: goApp("domain/domain.go", imports("domain", "api")),
			Head:     goApp("domain/domain.go", imports("domain", "api"), "api/api.go", imports("api", "store")),
			WantFail: []string{"deps/cycle"}},
		{Name: "an import of a cycle already in base passes", Base: goApp("domain/domain.go", back),
			Head: goApp("domain/domain.go", back, "domain/more.go", back, "store/more.go", imports("store", "domain"))},
		{Name: "a new import closing no cycle passes", Base: goApp(), Head: goApp("api/api.go", imports("api", "store", "domain"))},
		{Name: "an import in a test file closing a cycle passes", Base: goApp(), Head: goApp("domain/domain_test.go", inTest)},
		{Name: "a cycle within one box of the diagram fails", Base: withDiagram(goApp()), Head: withDiagram(goApp("domain/domain.go", back)),
			WantFail: []string{"deps/cycle"}},
		{Name: "a cycle between an allowed pair passes", Base: allow(goApp()), Head: allow(goApp("domain/domain.go", back))},
		{Name: "allowing a pair needs review", Base: goApp(".hadome.yml", "schema: 1\n"), Head: allow(goApp("domain/domain.go", back)),
			WantReview: []string{engine.LoosenedID}},
	}
	return append(append(out, iacCycleCases()...), languageCycleCases...)
}

// iacCycleCases are the cases of Terraform modules and Ansible roles, whose local module calls and
// role dependencies are imports between units too.
func iacCycleCases() []engine.Case {
	callB := "module \"b\" {\n  source = \"../b\"\n}\n"
	vpc := "resource \"aws_vpc\" \"main\" {\n  cidr_block = \"10.0.0.0/16\"\n}\n"
	install := "- name: Install\n  ansible.builtin.package:\n    name: nginx\n"
	return []engine.Case{
		{Name: "a Terraform module call closing a cycle fails",
			Base:     tree("modules/a/main.tf", callB, "modules/b/main.tf", vpc),
			Head:     tree("modules/a/main.tf", callB, "modules/b/main.tf", vpc+"\nmodule \"a\" {\n  source = \"../a\"\n}\n"),
			WantFail: []string{"deps/cycle"}},
		{Name: "an Ansible role dependency closing a cycle fails",
			Base: tree("roles/web/meta/main.yml", "dependencies:\n  - db\n", "roles/web/tasks/main.yml", install,
				"roles/db/tasks/main.yml", install),
			Head: tree("roles/web/meta/main.yml", "dependencies:\n  - db\n", "roles/web/tasks/main.yml", install,
				"roles/db/meta/main.yml", "dependencies:\n  - web\n", "roles/db/tasks/main.yml", install),
			WantFail: []string{"deps/cycle"}},
	}
}
