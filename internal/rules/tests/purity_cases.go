package tests

import (
	"testing/fstest"

	"github.com/rerorets/hadome/internal/engine"
)

const mockedTest = `package store_test

import (
	"testing"

	"github.com/golang/mock/gomock"

	"example.com/app/store"
)

func TestMocked(t *testing.T) {
	_ = gomock.NewController(t)
	store.Append(store.New(), "a")
}
`

// unitTree is the store module plus extra files, keyed by path. With mocked, the store's test
// uses a mock.
func unitTree(mocked bool, extra map[string]string) fstest.MapFS {
	files := testFile(plainTest)
	if mocked {
		files["store/mocked_test.go"] = mockedTest
	}
	for p, s := range extra {
		files[p] = s
	}
	return tree(files)
}

// Files that change which units are pure.
var (
	storeSaves     = map[string]string{"store/save.go": "package store\n\nimport \"database/sql\"\n\nvar _ *sql.DB\n"}
	storeUsesDB    = map[string]string{"store/save.go": "package store\n\nimport \"example.com/app/db\"\n\nvar _ = db.Open\n"}
	pureDB         = "package db\n\n// Open opens nothing.\nfunc Open() {}\n"
	impureDB       = "package db\n\nimport \"database/sql\"\n\n// Open opens a database.\nfunc Open() { _, _ = sql.Open(\"x\", \"\") }\n"
	configuredDBIO = "schema: 1\nrules:\n  tests:\n    io: [example.com/app/db]\n"
)

func plus(m map[string]string, kv ...string) map[string]string {
	out := map[string]string{}
	for k, v := range m {
		out[k] = v
	}
	for i := 0; i+1 < len(kv); i += 2 {
		out[kv[i]] = kv[i+1]
	}
	return out
}

// languageNoMockCases and languagePurityCases are cases in languages only a cgo build reads; see
// purity_cases_cgo.go.
var languageNoMockCases, languagePurityCases []engine.Case

func noMockCases() []engine.Case {
	return append([]engine.Case{
		{Name: "a mock in a test of a pure unit fails", Base: unitTree(false, nil), Head: unitTree(true, nil), WantFail: []string{"tests/no-mock"}},
		{Name: "the same mock in a test of a unit that is not pure passes",
			Base: unitTree(false, storeSaves), Head: unitTree(true, storeSaves)},
		{Name: "a unit made impure by a configured io entry passes",
			Base: unitTree(false, plus(storeUsesDB, "db/db.go", pureDB, ".hadome.yml", configuredDBIO)),
			Head: unitTree(true, plus(storeUsesDB, "db/db.go", pureDB, ".hadome.yml", configuredDBIO))},
		{Name: "a unit importing a unit of the repository that is not pure passes",
			Base: unitTree(false, plus(storeUsesDB, "db/db.go", impureDB)), Head: unitTree(true, plus(storeUsesDB, "db/db.go", impureDB))},
	}, languageNoMockCases...)
}

func purityCases() []engine.Case {
	calc := map[string]string{"calc/calc.go": "package calc\n\n// Add adds.\nfunc Add(a, b int) int { return a + b }\n"}
	return append([]engine.Case{
		{Name: "a pure unit gaining a database import needs review", Base: unitTree(false, nil), Head: unitTree(false, storeSaves),
			WantReview: []string{"tests/purity-lost"}},
		{Name: "a unit that was never pure does not", Base: unitTree(false, storeSaves), Head: unitTree(false, plus(storeSaves, "store/more.go", "package store\n"))},
		{Name: "a pure unit becoming impure through a unit it imports needs review",
			Base: unitTree(false, plus(storeUsesDB, "db/db.go", pureDB)), Head: unitTree(false, plus(storeUsesDB, "db/db.go", impureDB)),
			WantReview: []string{"tests/purity-lost", "tests/purity-lost"}},
		{Name: "deleting a pure unit passes", Base: unitTree(false, calc), Head: unitTree(false, nil)},
		{Name: "adding an io entry for what a unit already imports does not need review",
			Base: unitTree(false, plus(storeUsesDB, "db/db.go", pureDB)),
			Head: unitTree(false, plus(storeUsesDB, "db/db.go", pureDB, ".hadome.yml", configuredDBIO))},
	}, languagePurityCases...)
}
