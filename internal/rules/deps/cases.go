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
	}
}
