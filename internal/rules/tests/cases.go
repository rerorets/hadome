package tests

import (
	"strings"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/engine"
)

const goMod = "module example.com/app\n\ngo 1.24\n"

const storeSrc = `package store

// Store keeps rows.
type Store struct{ rows []string }

// New returns an empty store.
func New() *Store { return &Store{} }

// Append adds a row.
func Append(s *Store, row string) { s.rows = append(s.rows, row) }

// Len returns the number of rows.
func (s *Store) Len() int { return len(s.rows) }
`

// tree builds a small module: go.mod, the store package, and the given test file contents.
func tree(files map[string]string) fstest.MapFS {
	fsys := fstest.MapFS{"go.mod": {Data: []byte(goMod)}, "store/store.go": {Data: []byte(storeSrc)}}
	for p, s := range files {
		fsys[p] = &fstest.MapFile{Data: []byte(s)}
	}
	return fsys
}

func testFile(body string) map[string]string {
	return map[string]string{"store/store_test.go": `package store_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"example.com/app/store"
)

var _ = time.Second
var _ = assert.True

` + body}
}

const plainTest = `func TestAppend(t *testing.T) {
	s := store.New()
	store.Append(s, "a")
	if s.Len() != 1 {
		t.Fatal("want one row")
	}
}
`

func clockCases() []engine.Case {
	withClock := plainTest + "\nfunc TestStamp(t *testing.T) {\n\t_ = time.Now()\n\tstore.Append(store.New(), \"a\")\n}\n"
	return []engine.Case{
		{Name: "a new clock read fails", Base: tree(testFile(plainTest)), Head: tree(testFile(withClock)), WantFail: []string{"tests/no-clock"}},
		{Name: "an existing clock read that moved passes", Base: tree(testFile(withClock)), Head: tree(testFile("\n\n\n" + withClock))},
		{Name: "a clock read inside a string passes", Base: tree(testFile(plainTest)),
			Head: tree(testFile(plainTest + "\nfunc TestLog(t *testing.T) {\n\tt.Log(\"time.Now()\")\n\tstore.Append(store.New(), \"a\")\n}\n"))},
	}
}

const helperSleeps = "func pause() { time.Sleep(time.Millisecond) }\n\nfunc TestPause(t *testing.T) {\n\tstore.Append(store.New(), \"a\")\n"

func sleepCases() []engine.Case {
	withSleep := plainTest + "\nfunc TestWait(t *testing.T) {\n\ttime.Sleep(time.Millisecond)\n\tstore.Append(store.New(), \"a\")\n}\n"
	sleepInSource := tree(testFile(plainTest))
	sleepInSource["store/wait.go"] = &fstest.MapFile{Data: []byte("package store\n\nimport \"time\"\n\nfunc wait() { time.Sleep(time.Millisecond) }\n")}
	return []engine.Case{
		{Name: "a new sleep fails", Base: tree(testFile(plainTest)), Head: tree(testFile(withSleep)), WantFail: []string{"tests/no-sleep"}},
		{Name: "a sleep in non-test code is not this rule's business", Base: tree(testFile(plainTest)), Head: sleepInSource},
		{Name: "calling a helper that sleeps once more is not a new sleep", Base: tree(testFile(helperSleeps + "\tpause()\n}\n")),
			Head: tree(testFile(helperSleeps + "\tpause()\n\tpause()\n}\n"))},
	}
}

func alwaysTrueCases() []engine.Case {
	with := func(assertion string) string {
		return plainTest + "\nfunc TestMore(t *testing.T) {\n\tgot := store.New().Len()\n\t" + assertion + "\n}\n"
	}
	return []engine.Case{
		{Name: "assert.True(t, true) fails", Base: tree(testFile(plainTest)), Head: tree(testFile(with("assert.True(t, true)"))), WantFail: []string{"tests/always-true"}},
		{Name: "comparing a value with itself fails", Base: tree(testFile(plainTest)), Head: tree(testFile(with("assert.Equal(t, got, got)"))), WantFail: []string{"tests/always-true"}},
		{Name: "comparing two different values passes", Base: tree(testFile(plainTest)), Head: tree(testFile(with("assert.Equal(t, 0, got)")))},
	}
}

func skipCases() []engine.Case {
	with := func(skip string) string {
		return plainTest + "\nfunc TestLater(t *testing.T) {\n\t" + skip + "\n\tstore.Append(store.New(), \"a\")\n}\n"
	}
	return []engine.Case{
		{Name: "a new bare skip fails", Base: tree(testFile(plainTest)), Head: tree(testFile(with("t.Skip()"))), WantFail: []string{"tests/skip-without-reason"}},
		{Name: "a skip with a reason passes", Base: tree(testFile(plainTest)), Head: tree(testFile(with(`t.Skip("needs a running database")`)))},
		{Name: "an empty reason fails", Base: tree(testFile(plainTest)), Head: tree(testFile(with(`t.Skip("")`))), WantFail: []string{"tests/skip-without-reason"}},
		{Name: "a conditional skip without a reason fails", Base: tree(testFile(plainTest)),
			Head: tree(testFile(with("if testing.Short() {\n\t\tt.Skip()\n\t}"))), WantFail: []string{"tests/skip-without-reason"}},
	}
}

func scopeCases() []engine.Case {
	lenOnly := "func TestLen(t *testing.T) {\n\tif store.New().Len() != 0 {\n\t\tt.Fatal(\"want empty\")\n\t}\n}\n"
	twoTests := plainTest + "\nfunc TestAppendAgain(t *testing.T) {\n\ts := store.New()\n\tstore.Append(s, \"b\")\n}\n"
	renamed := map[string]string{"store/rows_test.go": testFile(plainTest)["store/store_test.go"]}
	noAppend := tree(testFile(lenOnly))
	noAppend["store/store.go"] = &fstest.MapFile{Data: []byte("package store\n\ntype Store struct{}\n\nfunc New() *Store { return &Store{} }\n\nfunc (s *Store) Len() int { return 0 }\n")}
	return []engine.Case{
		{Name: "deleting the only test of a target fails", Base: tree(testFile(plainTest)), Head: tree(testFile(lenOnly)), WantFail: []string{"tests/scope-shrunk"}},
		{Name: "merging two tests of one target passes", Base: tree(testFile(twoTests)), Head: tree(testFile(plainTest))},
		{Name: "deleting a target and its test passes", Base: tree(testFile(plainTest)), Head: noAppend},
		{Name: "renaming a test file passes", Base: tree(testFile(plainTest)), Head: tree(renamed)},
		{Name: "a test that skips only on one platform still guards", Base: tree(testFile(plainTest)),
			Head: tree(testFile(strings.Replace(plainTest, "\ts := store.New()", "\tif runtime.GOOS == \"plan9\" {\n\t\tt.Skip(\"no plan9 support\")\n\t}\n\ts := store.New()", 1)))},
	}
}

func unknownCases() []engine.Case {
	opaque := plainTest + "\nfunc TestOpaque(t *testing.T) {\n\tf := pick()\n\tf.Run()\n}\n"
	return []engine.Case{
		{Name: "a new test with no identifiable target needs review", Base: tree(testFile(plainTest)), Head: tree(testFile(opaque)), WantReview: []string{"tests/target-unknown"}},
		{Name: "a test calling the package API passes", Base: tree(testFile(plainTest)), Head: tree(testFile(plainTest + "\nfunc TestNew(t *testing.T) {\n\t_ = store.New()\n}\n"))},
		{Name: "a test reaching the API through a helper passes", Base: tree(testFile(plainTest)),
			Head: tree(testFile(plainTest + "\nfunc build() *store.Store { return store.New() }\n\nfunc TestHelper(t *testing.T) {\n\tif build().Len() != 0 {\n\t\tt.Fatal(\"want empty\")\n\t}\n}\n"))},
	}
}
