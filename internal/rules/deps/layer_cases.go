package deps

import (
	"testing/fstest"

	"github.com/rerorets/hadome/internal/engine"
)

// layered is goApp with the layers domain and adapters, where store is an adapter: store imports
// domain, from the outside in.
func layered(layers string, extra ...string) fstest.MapFS {
	t := goApp(extra...)
	t[".hadome.yml"] = &fstest.MapFile{Data: []byte("schema: 1\nrules:\n  deps:\n    layers:\n" + layers)}
	return t
}

const (
	twoLayers = "      - name: domain\n        paths: [domain/**]\n      - name: adapters\n        paths: [store/**, api/**]\n"
	domToAPI  = "package domain\n\nimport _ \"example.com/app/api\"\n"
)

func layerCases() []engine.Case {
	inTest := "package domain\n\nimport (\n\t\"testing\"\n\n\t_ \"example.com/app/api\"\n)\n\nfunc TestX(t *testing.T) {}\n"
	nested := "      - name: core\n        paths: [domain/rules/**]\n      - name: domain\n        paths: [domain/**]\n" +
		"      - name: adapters\n        paths: [store/**, api/**]\n"
	return []engine.Case{
		{Name: "an import from the outside in passes", Base: layered(twoLayers),
			Head: layered(twoLayers, "api/api.go", "package api\n\nimport _ \"example.com/app/domain\"\n")},
		{Name: "a new import from the inside out fails", Base: layered(twoLayers), Head: layered(twoLayers, "domain/domain.go", domToAPI),
			WantFail: []string{"deps/layer"}},
		{Name: "an existing import from the inside out passes", Base: layered(twoLayers, "domain/domain.go", domToAPI),
			Head: layered(twoLayers, "domain/domain.go", "\n"+domToAPI)},
		{Name: "an import within one layer passes", Base: layered(twoLayers),
			Head: layered(twoLayers, "api/api.go", "package api\n\nimport _ \"example.com/app/store\"\n")},
		{Name: "an import to a unit in no layer passes", Base: layered(twoLayers),
			Head: layered(twoLayers, "domain/domain.go", "package domain\n\nimport _ \"example.com/app/util\"\n", "util/util.go", "package util\n")},
		{Name: "an import from a unit in no layer passes", Base: layered(twoLayers),
			Head: layered(twoLayers, "util/util.go", "package util\n\nimport _ \"example.com/app/api\"\n")},
		{Name: "a unit matching two layers is in the one written first", Base: layered(nested),
			Head:     layered(nested, "domain/rules/rules.go", "package rules\n\nimport _ \"example.com/app/domain\"\n"),
			WantFail: []string{"deps/layer"}},
		{Name: "an import in a test file passes", Base: layered(twoLayers), Head: layered(twoLayers, "domain/domain_test.go", inTest)},
		{Name: "writing layers for the first time needs no review", Base: goApp(".hadome.yml", "schema: 1\n"),
			Head: layered(twoLayers)},
		{Name: "changing layers needs review", Base: layered(twoLayers),
			Head: layered("      - name: domain\n        paths: [domain/**]\n"), WantReview: []string{engine.LoosenedID}},
	}
}
