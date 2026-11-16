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
	return append(out, interfaceCases()...)
}

// javaCalls is a Java project with a call diagram: the controller declares Finder and holds one,
// and the service implements it. Extra files are keyed by path.
func javaCalls(diagram string, extra ...string) fstest.MapFS {
	const src = "src/main/java/com/ex/"
	t := fstest.MapFS{
		".hadome.yml":                  {Data: []byte("schema: 1\nrules:\n  deps:\n    calls: docs/calls.md\n")},
		"docs/calls.md":                {Data: []byte("```mermaid\nflowchart LR\n  c[\"" + src + "controller\"]\n  s[\"" + src + "service\"]\n  r[\"" + src + "repo\"]\n" + diagram + "\n```\n")},
		src + "controller/Finder.java": {Data: []byte("package com.ex.controller;\n\npublic interface Finder {\n    String find(String id);\n}\n")},
		src + "controller/UserController.java": {Data: []byte("package com.ex.controller;\n\npublic class UserController {\n" +
			"    private final Finder finder;\n    public UserController(Finder finder) { this.finder = finder; }\n}\n")},
		src + "service/UserService.java": {Data: []byte("package com.ex.service;\n\nimport com.ex.controller.Finder;\n\n" +
			"public class UserService implements Finder {\n    public String find(String id) { return id; }\n}\n")},
	}
	for i := 0; i+1 < len(extra); i += 2 {
		t[extra[i]] = &fstest.MapFile{Data: []byte(extra[i+1])}
	}
	return t
}

func interfaceCases() []engine.Case {
	const (
		controller = "src/main/java/com/ex/controller/UserController.java"
		calling    = "package com.ex.controller;\n\npublic class UserController {\n    private final Finder finder;\n" +
			"    public UserController(Finder finder) { this.finder = finder; }\n    public String show(String id) { return finder.find(id); }\n}\n"
		memory = "package com.ex.repo;\n\nimport com.ex.controller.Finder;\n\n" +
			"public class MemoryFinder implements Finder {\n    public String find(String id) { return \"\"; }\n}\n"
		fake = "package com.ex.controller;\n\npublic class FakeFinder implements Finder {\n    public String find(String id) { return \"\"; }\n}\n"
	)
	toService := "  c --> s"
	return []engine.Case{
		{Name: "a call through an interface needs an arrow to the implementation's unit", Base: javaCalls(""),
			Head: javaCalls("", controller, calling), WantFail: []string{"deps/call-not-in-diagram"}},
		{Name: "a call through an interface along an arrow to the implementation passes", Base: javaCalls(toService),
			Head: javaCalls(toService, controller, calling)},
		{Name: "with implementations in two units, an arrow to one alone fails", Base: javaCalls(toService),
			Head:     javaCalls(toService, controller, calling, "src/main/java/com/ex/repo/MemoryFinder.java", memory),
			WantFail: []string{"deps/call-not-in-diagram"}},
		{Name: "an implementation in a test file does not count", Base: javaCalls(toService),
			Head: javaCalls(toService, controller, calling, "src/test/java/com/ex/repo/MemoryFinder.java", memory)},
		{Name: "an implementation in the caller's own unit passes", Base: javaCalls(toService),
			Head: javaCalls(toService, controller, calling, "src/main/java/com/ex/controller/FakeFinder.java", fake)},
	}
}
