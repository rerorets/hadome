package deps

import (
	"testing/fstest"

	"github.com/rerorets/hadome/internal/engine"
)

// app is a module with a diagram, the units store, domain and api, and extra files, keyed by path.
func app(diagram string, extra ...string) fstest.MapFS {
	t := fstest.MapFS{
		"go.mod":               {Data: []byte("module example.com/app\n")},
		".hadome.yml":          {Data: []byte("schema: 1\nrules:\n  deps:\n    diagram: docs/deps.md\n")},
		"docs/deps.md":         {Data: []byte("```mermaid\nflowchart LR\n" + diagram + "\n```\n")},
		"domain/domain.go":     {Data: []byte("package domain\n")},
		"store/store.go":       {Data: []byte("package store\n\nimport _ \"example.com/app/domain\"\n")},
		"api/api.go":           {Data: []byte("package api\n")},
		"api/v1/handlers/h.go": {Data: []byte("package handlers\n")},
	}
	for i := 0; i+1 < len(extra); i += 2 {
		t[extra[i]] = &fstest.MapFile{Data: []byte(extra[i+1])}
	}
	return t
}

const (
	drawn    = "  api[\"api/...\"] --> store\n  store --> domain"
	apiToDom = "package api\n\nimport _ \"example.com/app/domain\"\n"
)

func importCases() []engine.Case {
	withTest := "package api\n\nimport (\n\t\"testing\"\n\n\t_ \"example.com/app/domain\"\n)\n\nfunc TestX(t *testing.T) {}\n"
	inGroup := "package api\n\nimport _ \"example.com/app/api/v1/handlers\"\n"
	fromHandlers := "package handlers\n\nimport _ \"example.com/app/store\"\n"
	return []engine.Case{
		{Name: "an import along an arrow passes", Base: app(drawn), Head: app(drawn, "api/api.go", "package api\n\nimport _ \"example.com/app/store\"\n")},
		{Name: "an import with no arrow fails", Base: app(drawn), Head: app(drawn, "api/api.go", apiToDom), WantFail: []string{"deps/not-in-diagram"}},
		{Name: "an existing undrawn import passes", Base: app(drawn, "api/api.go", apiToDom), Head: app(drawn, "api/api.go", "\n"+apiToDom)},
		{Name: "an import within one node passes", Base: app(drawn), Head: app(drawn, "api/api.go", inGroup)},
		{Name: "a /... node covers what is under it", Base: app(drawn), Head: app(drawn, "api/v1/handlers/h.go", fromHandlers)},
		{Name: "an import from a unit in no node fails", Base: app(drawn),
			Head: app(drawn, "jobs/jobs.go", "package jobs\n\nimport _ \"example.com/app/domain\"\n"), WantFail: []string{"deps/not-in-diagram"}},
		{Name: "an import between two units in no node fails", Base: app(drawn),
			Head:     app(drawn, "jobs/jobs.go", "package jobs\n\nimport _ \"example.com/app/util\"\n", "util/util.go", "package util\n"),
			WantFail: []string{"deps/not-in-diagram"}},
		{Name: "an import in a test file passes", Base: app(drawn), Head: app(drawn, "api/api_test.go", withTest)},
		{Name: "drawing the arrow needs review instead", Base: app(drawn),
			Head: app(drawn+"\n  api --> domain", "api/api.go", apiToDom), WantReview: []string{engine.LoosenedID}},
	}
}

func staleCases() []engine.Case {
	return []engine.Case{
		{Name: "a node naming a missing directory fails", Base: app(drawn), Head: app(drawn + "\n  store --> util[\"utils\"]"),
			WantFail: []string{"deps/diagram-stale"}, WantReview: []string{engine.LoosenedID}},
		{Name: "a node naming an existing unit passes", Base: app(drawn), Head: app("  api[\"api/...\"] --> store\n  store --> domain[\"domain\"]")},
		{Name: "a node naming a directory of tests only fails", Base: app(drawn),
			Head:     app(drawn+"\n  e2e --> api", "e2e/e2e_test.go", "package e2e\n"),
			WantFail: []string{"deps/diagram-stale"}, WantReview: []string{engine.LoosenedID}},
		{Name: "moving a unit away from its node fails", Base: app(drawn),
			Head: func() fstest.MapFS {
				t := app(drawn)
				delete(t, "domain/domain.go")
				t["core/domain.go"] = &fstest.MapFile{Data: []byte("package core\n")}
				return t
			}(),
			WantFail: []string{"deps/diagram-stale"}},
		{Name: "a planned box naming a missing directory passes", Base: app(drawn), Head: app(drawn + "\n  util[\"utils\"]:::planned")},
		{Name: "a box a class statement marks planned passes too", Base: app(drawn),
			Head: app(drawn + "\n  class util planned\n  util[\"utils\"]")},
		{Name: "a planned box naming a unit fails", Base: app(drawn + "\n  jobs[\"jobs\"]:::planned"),
			Head:     app(drawn+"\n  jobs[\"jobs\"]:::planned", "jobs/jobs.go", "package jobs\n"),
			WantFail: []string{"deps/diagram-stale"}},
		{Name: "marking an existing box planned needs review", Base: app(drawn + "\n  util[\"utils\"]"),
			Head: app(drawn + "\n  util[\"utils\"]:::planned"), WantReview: []string{engine.LoosenedID}},
		{Name: "a node of the call diagram naming a missing directory fails", Base: callApp(callsDrawn),
			Head: callApp(callsDrawn + "\n  store --> util[\"utils\"]"), WantFail: []string{"deps/diagram-stale"}, WantReview: []string{engine.LoosenedID}},
	}
}

// callApp is a module with a call diagram, the units domain, store and api, where store calls
// domain, and extra files, keyed by path.
func callApp(diagram string, extra ...string) fstest.MapFS {
	t := fstest.MapFS{
		"go.mod":               {Data: []byte("module example.com/app\n")},
		".hadome.yml":          {Data: []byte("schema: 1\nrules:\n  deps:\n    calls: docs/calls.md\n")},
		"docs/calls.md":        {Data: []byte("```mermaid\nflowchart LR\n" + diagram + "\n```\n")},
		"domain/domain.go":     {Data: []byte("package domain\n\nfunc Get() {}\n")},
		"store/store.go":       {Data: []byte("package store\n\nimport \"example.com/app/domain\"\n\nfunc Save() { domain.Get() }\n")},
		"api/api.go":           {Data: []byte("package api\n")},
		"api/v1/handlers/h.go": {Data: []byte("package handlers\n\nfunc List() {}\n")},
	}
	for i := 0; i+1 < len(extra); i += 2 {
		t[extra[i]] = &fstest.MapFile{Data: []byte(extra[i+1])}
	}
	return t
}

const (
	callsDrawn = "  api[\"api/...\"] --> store\n  store --> domain"
	apiCallDom = "package api\n\nimport \"example.com/app/domain\"\n\nfunc Show() { domain.Get() }\n"
)

func callCases() []engine.Case {
	toStore := "package api\n\nimport \"example.com/app/store\"\n\nfunc Create() { store.Save() }\n"
	inGroup := "package api\n\nimport \"example.com/app/api/v1/handlers\"\n\nfunc Index() { handlers.List() }\n"
	wire := "package wire\n\nimport \"example.com/app/domain\"\n\nfunc Build() { domain.Get() }\n"
	toWire := "package api\n\nimport \"example.com/app/wire\"\n\nfunc Start() { wire.Build() }\n"
	withWire := callsDrawn + "\n  w[\"wire\"]:::wiring"
	bothDiagrams := func(t fstest.MapFS) fstest.MapFS {
		t[".hadome.yml"] = &fstest.MapFile{Data: []byte("schema: 1\nrules:\n  deps:\n    diagram: docs/deps.md\n    calls: docs/calls.md\n")}
		t["docs/deps.md"] = &fstest.MapFile{Data: []byte("```mermaid\nflowchart LR\n" + callsDrawn + "\n  api --> domain\n```\n")}
		return t
	}
	out := []engine.Case{
		{Name: "a direct call along an arrow passes", Base: callApp(callsDrawn), Head: callApp(callsDrawn, "api/api.go", toStore)},
		{Name: "a direct call with no arrow fails", Base: callApp(callsDrawn), Head: callApp(callsDrawn, "api/api.go", apiCallDom),
			WantFail: []string{"deps/call-not-in-diagram"}},
		{Name: "a call outside functions with no arrow fails", Base: callApp(callsDrawn),
			Head:     callApp(callsDrawn, "api/api.go", "package api\n\nimport \"example.com/app/domain\"\n\nvar current = domain.Get()\n"),
			WantFail: []string{"deps/call-not-in-diagram"}},
		{Name: "an existing undrawn call passes", Base: callApp(callsDrawn, "api/api.go", apiCallDom),
			Head: callApp(callsDrawn, "api/api.go", "\n"+apiCallDom)},
		{Name: "a call within one node passes", Base: callApp(callsDrawn), Head: callApp(callsDrawn, "api/api.go", inGroup)},
		{Name: "a call from a wiring node passes", Base: callApp(withWire), Head: callApp(withWire, "wire/wire.go", wire)},
		{Name: "an undrawn call to a wiring node fails", Base: callApp(withWire, "wire/wire.go", wire),
			Head: callApp(withWire, "wire/wire.go", wire, "api/api.go", toWire), WantFail: []string{"deps/call-not-in-diagram"}},
		{Name: "a call with an import arrow but no call arrow fails", Base: bothDiagrams(callApp(callsDrawn)),
			Head: bothDiagrams(callApp(callsDrawn, "api/api.go", apiCallDom)), WantFail: []string{"deps/call-not-in-diagram"}},
		{Name: "drawing the arrow needs review instead", Base: callApp(callsDrawn),
			Head: callApp(callsDrawn+"\n  api --> domain", "api/api.go", apiCallDom), WantReview: []string{engine.LoosenedID}},
		{Name: "marking wiring needs review", Base: callApp(callsDrawn),
			Head: callApp(callsDrawn+"\n  class api wiring", "api/api.go", apiCallDom), WantReview: []string{engine.LoosenedID}},
	}
	return append(out, goInterfaceCases()...)
}

// goInterfaceCases are the cases of calls through an interface: api declares Finder and calls it on
// a parameter, and other units implement it.
func goInterfaceCases() []engine.Case {
	const (
		port    = "package api\n\ntype Finder interface{ Find(id string) string }\n"
		calling = port + "\nfunc Show(f Finder) string { return f.Find(\"x\") }\n"
		impl    = "type Repo struct{}\n\nfunc (Repo) Find(id string) string { return id }\n"
		asking  = "package api\n\nimport \"errors\"\n\ntype Finder interface{ Find(id string) string }\n\n" +
			"func Show(err error) string {\n\tvar f Finder\n\tif errors.As(err, &f) {\n\t\treturn f.Find(\"x\")\n\t}\n\treturn \"\"\n}\n"
		askingRepo = "package api\n\nimport (\n\t\"errors\"\n\n\t\"example.com/app/domain\"\n)\n\n" +
			"func Show(err error) string {\n\tvar r domain.Repo\n\tif errors.As(err, &r) {\n\t\treturn r.Find(\"x\")\n\t}\n\treturn \"\"\n}\n"
	)
	inDomain, inStore := "package domain\n\n"+impl, "package store\n\n"+impl
	return []engine.Case{
		{Name: "a call through an interface needs an arrow to the implementation's unit",
			Base:     callApp(callsDrawn, "api/api.go", port, "domain/repo.go", inDomain),
			Head:     callApp(callsDrawn, "api/api.go", calling, "domain/repo.go", inDomain),
			WantFail: []string{"deps/call-not-in-diagram"}},
		{Name: "a call through an interface along an arrow to the implementation passes",
			Base: callApp(callsDrawn, "api/api.go", port, "store/repo.go", inStore),
			Head: callApp(callsDrawn, "api/api.go", calling, "store/repo.go", inStore)},
		{Name: "with implementations in two units, an arrow to one alone fails",
			Base:     callApp(callsDrawn, "api/api.go", port, "store/repo.go", inStore, "domain/repo.go", inDomain),
			Head:     callApp(callsDrawn, "api/api.go", calling, "store/repo.go", inStore, "domain/repo.go", inDomain),
			WantFail: []string{"deps/call-not-in-diagram"}},
		{Name: "an implementation in a test file does not count",
			Base: callApp(callsDrawn, "api/api.go", port, "domain/repo_test.go", inDomain),
			Head: callApp(callsDrawn, "api/api.go", calling, "domain/repo_test.go", inDomain)},
		{Name: "a call on a value asked for an interface is left to the call that returned it",
			Base: callApp(callsDrawn, "api/api.go", port, "domain/repo.go", inDomain),
			Head: callApp(callsDrawn, "api/api.go", asking, "domain/repo.go", inDomain)},
		{Name: "a call on a value asked for a type of another unit with no arrow fails",
			Base:     callApp(callsDrawn, "domain/repo.go", inDomain),
			Head:     callApp(callsDrawn, "api/api.go", askingRepo, "domain/repo.go", inDomain),
			WantFail: []string{"deps/call-not-in-diagram"}},
	}
}
