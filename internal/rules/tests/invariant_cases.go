package tests

import (
	"testing/fstest"

	"github.com/rerorets/hadome/internal/engine"
)

// rowsSrc adds operations with the shapes invariants care about: a pair, and a read scoped by user.
const rowsSrc = `package store

// Export writes the rows out.
func Export(s *Store) []string { return s.rows }

// Import reads rows back.
func Import(rows []string) *Store { return &Store{rows: rows} }

// Rows returns the rows one user may see.
func Rows(s *Store, user string) []string { return s.rows }
`

// invariantTree is a module whose configuration declares the given invariants and whose test file
// holds body. An empty yml means no configuration file.
func invariantTree(yml, body string) fstest.MapFS {
	files := testFile(body)
	files["store/rows.go"] = rowsSrc
	if yml != "" {
		files[".hadome.yml"] = "schema: 1\ninvariants:\n" + yml
	}
	return tree(files)
}

const (
	idempotentAppend = "  - type: idempotent\n    target: store.Append\n"
	appendTwice      = "func TestAppendTwice(t *testing.T) {\n\ts := store.New()\n\tstore.Append(s, \"a\")\n\tstore.Append(s, \"a\")\n}\n"
	appendOnce       = "func TestAppendOnce(t *testing.T) {\n\tstore.Append(store.New(), \"a\")\n}\n"
)

func invariantCases() []engine.Case {
	moved := "func TestRetry(t *testing.T) {\n\ts := store.New()\n\tfor range 2 {\n\t\tstore.Append(s, \"a\")\n\t}\n}\n"
	skipped := "func TestAppendTwice(t *testing.T) {\n\tt.Skip(\"flaky\")\n\ts := store.New()\n\tstore.Append(s, \"a\")\n\tstore.Append(s, \"a\")\n}\n"
	viaHelper := "func add(s *store.Store) { store.Append(s, \"a\") }\n\nfunc TestAppendTwice(t *testing.T) {\n\ts := store.New()\n\tadd(s)\n\tadd(s)\n}\n"
	withoutInvariants := invariantTree("", appendOnce)
	withoutInvariants[".hadome.yml"] = &fstest.MapFile{Data: []byte("schema: 1\n")}
	return append([]engine.Case{
		{Name: "a new invariant whose only test calls the target once fails (idempotent needs two calls)",
			Base: invariantTree("", appendOnce), Head: invariantTree(idempotentAppend, appendOnce), WantFail: []string{"tests/invariant-unguarded"}},
		{Name: "an invariant already unguarded in base does not block",
			Base: invariantTree(idempotentAppend, appendOnce), Head: invariantTree(idempotentAppend, appendOnce+"\n"+plainTest)},
		{Name: "a new invariant whose test calls the target twice passes",
			Base: invariantTree("", appendTwice), Head: invariantTree(idempotentAppend, appendTwice)},
		{Name: "deleting the only guard fails",
			Base: invariantTree(idempotentAppend, appendTwice), Head: invariantTree(idempotentAppend, appendOnce), WantFail: []string{"tests/invariant-unguarded"}},
		{Name: "moving the guard to another test passes",
			Base: invariantTree(idempotentAppend, appendTwice), Head: invariantTree(idempotentAppend, moved)},
		{Name: "a helper called twice counts as two calls",
			Base: invariantTree(idempotentAppend, appendTwice), Head: invariantTree(idempotentAppend, viaHelper)},
		{Name: "skipping the only guard fails",
			Base: invariantTree(idempotentAppend, appendTwice), Head: invariantTree(idempotentAppend, skipped), WantFail: []string{"tests/invariant-unguarded"}},
		{Name: "removing the invariant needs review, not this rule",
			Base: invariantTree(idempotentAppend, appendTwice), Head: withoutInvariants, WantReview: []string{engine.LoosenedID}},
	}, shapeCases()...)
}

func shapeCases() []engine.Case {
	roundTrip := "  - type: round-trip\n    targets: [store.Export, store.Import]\n"
	isolated := "  - type: isolated\n    target: store.Rows\n"
	own := "  - id: rows-1\n    says: Rows are never lost\n"
	durable := "  - type: durable\n    target: store.Append\n"
	exportOnly := "func TestExport(t *testing.T) {\n\t_ = store.Export(store.New())\n}\n"
	both := "func TestRoundTrip(t *testing.T) {\n\t_ = store.Import(store.Export(store.New()))\n}\n"
	oneUser := "func TestRows(t *testing.T) {\n\ts := store.New()\n\t_ = store.Rows(s, \"alice\")\n\t_ = store.Rows(s, \"alice\")\n}\n"
	twoUsers := "func TestRows(t *testing.T) {\n\ts := store.New()\n\t_ = store.Rows(s, \"alice\")\n\t_ = store.Rows(s, \"bob\")\n}\n"
	declared := "// guards: rows-1\n" + appendOnce
	declaredDurable := "// guards: durable store.Append\n" + appendOnce
	return []engine.Case{
		{Name: "round-trip: one of the pair does not guard it", Base: invariantTree("", exportOnly),
			Head: invariantTree(roundTrip, exportOnly), WantFail: []string{"tests/invariant-unguarded"}},
		{Name: "round-trip: both operations guard it", Base: invariantTree("", both), Head: invariantTree(roundTrip, both)},
		{Name: "isolated: one user does not guard it", Base: invariantTree("", oneUser),
			Head: invariantTree(isolated, oneUser), WantFail: []string{"tests/invariant-unguarded"}},
		{Name: "isolated: two users guard it", Base: invariantTree("", twoUsers), Head: invariantTree(isolated, twoUsers)},
		{Name: "product-specific: calls alone do not guard it", Base: invariantTree("", appendTwice),
			Head: invariantTree(own, appendTwice), WantFail: []string{"tests/invariant-unguarded"}},
		{Name: "product-specific: guards: links it", Base: invariantTree("", declared), Head: invariantTree(own, declared)},
		{Name: "durable: a call without guards: does not guard it", Base: invariantTree("", appendTwice),
			Head: invariantTree(durable, appendTwice), WantFail: []string{"tests/invariant-unguarded"}},
		{Name: "durable: a call with guards: guards it", Base: invariantTree("", declaredDurable), Head: invariantTree(durable, declaredDurable)},
	}
}
