package tests

import (
	"io/fs"
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
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"example.com/app/store"
)

var _ = time.Second
var _ = assert.True
var _ = strings.ToUpper

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
		{Name: "deleting the only Molecule scenario of an Ansible role fails", Base: ansibleRole(true), Head: ansibleRole(false),
			WantFail: []string{"tests/scope-shrunk"}},
		{Name: "deleting the role with its scenario passes", Base: ansibleRole(true), Head: fstest.MapFS{}},
	}
}

// ansibleRole is the Ansible role web, with a Molecule scenario that applies it and checks the result
// when scenario is true.
func ansibleRole(scenario bool) fstest.MapFS {
	t := fstest.MapFS{"roles/web/tasks/main.yml": {Data: []byte("- name: Install the server\n  ansible.builtin.package:\n    name: nginx\n")}}
	if scenario {
		t["roles/web/molecule/default/converge.yml"] = &fstest.MapFile{Data: []byte("- hosts: all\n  roles:\n    - web\n")}
		t["roles/web/molecule/default/verify.yml"] = &fstest.MapFile{Data: []byte(
			"- hosts: all\n  tasks:\n    - ansible.builtin.assert:\n        that: ansible_facts.packages.nginx is defined\n")}
	}
	return t
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

func guardsNothingCases() []engine.Case {
	add := func(test string) engine.Case {
		return engine.Case{Base: tree(testFile(plainTest)), Head: tree(testFile(plainTest + "\n" + test))}
	}
	stdlibOnly := add("func TestUpper(t *testing.T) {\n\tif strings.ToUpper(\"a\") != \"A\" {\n\t\tt.Fatal(\"want A\")\n\t}\n}\n")
	stdlibOnly.Name, stdlibOnly.WantFail = "a new test that calls no code of the repository fails", []string{"tests/guards-nothing"}
	helper := add("func build() *store.Store { return store.New() }\n\nfunc TestBuild(t *testing.T) {\n\tif build() == nil {\n\t\tt.Fatal(\"want a store\")\n\t}\n}\n")
	helper.Name = "a test that reaches the code through a helper passes"
	partly := add("func TestPartly(t *testing.T) {\n\tf := pick()\n\tf.Run()\n\t_ = strings.ToUpper(\"a\")\n}\n")
	partly.Name = "a test with a call hadome cannot identify passes"
	reads := add("func TestLimit(t *testing.T) {\n\tif store.Limit < 1 {\n\t\tt.Fatal(\"want a positive limit\")\n\t}\n}\n")
	reads.Head.(fstest.MapFS)["store/limit.go"] = &fstest.MapFile{Data: []byte("package store\n\n// Limit caps the rows.\nconst Limit = 10\n")}
	reads.Name = "a test that reads a product constant passes"
	skipped := add("func TestLater(t *testing.T) {\n\tt.Skip(\"needs a running database\")\n\t_ = strings.ToUpper(\"a\")\n}\n")
	skipped.Name = "a skipped test passes"
	existing := engine.Case{Name: "a test already on the base branch is not reported",
		Base: tree(testFile(plainTest + "\nfunc TestUpper(t *testing.T) {\n\t_ = strings.ToUpper(\"a\")\n}\n")),
		Head: tree(testFile("\n" + plainTest + "\nfunc TestUpper(t *testing.T) {\n\t_ = strings.ToUpper(\"a\")\n}\n"))}
	return []engine.Case{stdlibOnly, helper, partly, reads, skipped, existing}
}

func duplicateCases() []engine.Case {
	copyOf := func(name string) string { return strings.Replace(plainTest, "TestAppend", name, 1) }
	pair := plainTest + "\n" + copyOf("TestAppendAgain")
	added := engine.Case{Name: "adding a copy of an existing test fails", WantFail: []string{"tests/duplicate"},
		Base: tree(testFile(plainTest)), Head: tree(testFile(pair))}
	disguised := engine.Case{Name: "a copy with only its name, comments and layout changed fails", WantFail: []string{"tests/duplicate"},
		Base: tree(testFile(plainTest)),
		Head: tree(testFile(plainTest + "\n// TestOneRow checks one row.\nfunc TestOneRow(t *testing.T) {\n\ts := store.New() // empty\n\tstore.Append(s, \"a\")\n\tif s.Len() != 1 { t.Fatal(\"want one row\") }\n}\n"))}
	literal := engine.Case{Name: "a test with one literal changed passes",
		Base: tree(testFile(plainTest)), Head: tree(testFile(plainTest + "\n" + strings.Replace(copyOf("TestAppendB"), `"a"`, `"b"`, 1)))}
	removed := engine.Case{Name: "deleting one of a pair of copies passes",
		Base: tree(testFile(pair)), Head: tree(testFile(plainTest))}
	renamed := engine.Case{Name: "renaming a copy does not make it new",
		Base: tree(testFile(pair)), Head: tree(testFile(plainTest + "\n" + copyOf("TestAppendTwice")))}
	third := engine.Case{Name: "a third copy of a pair already on the base branch fails", WantFail: []string{"tests/duplicate"},
		Base: tree(testFile(pair)), Head: tree(testFile(pair + "\n" + copyOf("TestAppendThird")))}
	return []engine.Case{added, disguised, literal, removed, renamed, third}
}

func noAssertionCases() []engine.Case {
	add := func(test string) engine.Case {
		return engine.Case{Base: tree(testFile(plainTest)), Head: tree(testFile(plainTest + "\n" + test))}
	}
	none := add("func TestAppendOnly(t *testing.T) {\n\tstore.Append(store.New(), \"a\")\n}\n")
	none.Name, none.WantFail = "a new test with no assertion fails", []string{"tests/no-assertion"}
	helper := add("func wantLen(t *testing.T, s *store.Store, n int) {\n\tif s.Len() != n {\n\t\tt.Errorf(\"want %d rows\", n)\n\t}\n}\n\n" +
		"func TestAppendOne(t *testing.T) {\n\ts := store.New()\n\tstore.Append(s, \"a\")\n\twantLen(t, s, 1)\n}\n")
	helper.Name = "a test that asserts in a helper passes"
	handed := add("func TestAppendChecked(t *testing.T) {\n\ts := store.New()\n\tstore.Append(s, \"a\")\n\tcheck.Rows(t, s)\n}\n")
	handed.Name = "a test that hands t to a function hadome cannot read passes"
	configured := add("func TestAppendEqual(t *testing.T) {\n\ts := store.New()\n\tstore.Append(s, \"a\")\n\tcheck.Equal(s.Len(), 1)\n}\n")
	configured.Name = "a test that calls a configured assertion passes"
	for _, c := range []*engine.Case{&handed, &configured} {
		for _, side := range []fs.FS{c.Base, c.Head} {
			side.(fstest.MapFS)["check/check.go"] = &fstest.MapFile{Data: []byte(checkSrc)}
			side.(fstest.MapFS)[".hadome.yml"] = &fstest.MapFile{Data: []byte("schema: 1\nrules:\n  tests:\n    assertions: [example.com/app/check.Equal]\n")}
		}
		c.Head.(fstest.MapFS)["store/store_test.go"].Data = []byte(strings.Replace(string(c.Head.(fstest.MapFS)["store/store_test.go"].Data),
			"\"example.com/app/store\"", "\"example.com/app/check\"\n\t\"example.com/app/store\"", 1))
	}
	skipped := add("func TestAppendLater(t *testing.T) {\n\tt.Skip(\"needs a running database\")\n\tstore.Append(store.New(), \"a\")\n}\n")
	skipped.Name = "a skipped test passes"
	existing := engine.Case{Name: "a test already on the base branch is not reported",
		Base: tree(testFile(plainTest + "\nfunc TestAppendOnly(t *testing.T) {\n\tstore.Append(store.New(), \"a\")\n}\n")),
		Head: tree(testFile("\n" + plainTest + "\nfunc TestAppendOnly(t *testing.T) {\n\tstore.Append(store.New(), \"a\")\n}\n"))}
	return []engine.Case{none, helper, handed, configured, skipped, existing}
}

const checkSrc = `package check

import "testing"

// Rows checks the rows of a store.
func Rows(t *testing.T, s any) {}

// Equal panics unless a equals b.
func Equal(a, b any) {}
`
