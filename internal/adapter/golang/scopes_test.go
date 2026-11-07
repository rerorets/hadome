package golang

import (
	"strings"
	"testing"
	"testing/fstest"
)

// viewpoint: behaviour
// target: golang.Adapter.Analyze
// A helper's parameters have the types they are declared with, read with the imports of the
// helper's own file; a variadic parameter is a slice of its type. A helper does not see the
// variables of the test that calls it, and its parameters do not change the test's variable of the
// same name.
func TestGoHelperParametersHaveTheirDeclaredTypes(t *testing.T) {
	tree := fstest.MapFS{
		"go.mod":         {Data: []byte("module example.com/app\n")},
		"store/store.go": {Data: []byte(storeSource)},
		"store/helpers_test.go": {Data: []byte(`package store_test

import (
	"testing"

	st "example.com/app/store"
)

func check(t *testing.T, s *st.Store) {
	t.Helper()
	_ = s.Append()
}

func checkAll(t *testing.T, stores ...*st.Store) {
	for _, s := range stores {
		_ = s.Append()
	}
}

func useOuter(t *testing.T) { _ = cfg.Validate() }
`)},
		"store/store_test.go": {Data: []byte(`package store_test

import (
	"testing"

	"example.com/app/store"
)

func TestHelperParam(t *testing.T) { check(t, &store.Store{}) }

func TestVariadicParam(t *testing.T) { checkAll(t, &store.Store{}) }

func TestCallerKeepsItsVariable(t *testing.T) {
	s := store.Default
	check(t, &store.Store{})
	_ = s.Validate()
}

func TestHelperDoesNotSeeTheCaller(t *testing.T) {
	cfg := store.Default
	_ = cfg
	useOuter(t)
}
`)},
	}
	want := strings.Join([]string{
		"TestCallerKeepsItsVariable calls=[example.com/app/store.Config.Validate×1 example.com/app/store.Store.Append×1] unresolved=0",
		"TestHelperDoesNotSeeTheCaller calls=[] unresolved=1",
		"TestHelperParam calls=[example.com/app/store.Store.Append×1] unresolved=0",
		// A call inside a loop counts as two: it may run more than once.
		"TestVariadicParam calls=[example.com/app/store.Store.Append×2] unresolved=0",
	}, "\n")
	if got := callsByTest(t, tree); got != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", got, want)
	}
}

// viewpoint: behaviour
// target: golang.Adapter.Analyze
// A package-level variable declared in one test file is typed in every test file of the same
// package, whether declared with a type or made by a helper; with one call on the right, the first
// name takes the call's first result. A test file of the external _test
// package does not see the variables of the internal test files.
func TestGoTestPackageVariablesAreSharedAcrossFiles(t *testing.T) {
	tree := fstest.MapFS{
		"go.mod":         {Data: []byte("module example.com/app\n")},
		"store/store.go": {Data: []byte(storeSource)},
		"store/setup_test.go": {Data: []byte(`package store_test

import (
	"testing"

	"example.com/app/store"
)

var shared *store.Store

var built = newStore()

var opened, openErr = openStore()

func newStore() *store.Store { return &store.Store{} }

func openStore() (*store.Store, error) { return &store.Store{}, nil }

func TestMain(m *testing.M) { shared = newStore(); m.Run() }
`)},
		"store/a_test.go": {Data: []byte(`package store_test

import "testing"

func TestDeclaredElsewhere(t *testing.T) { _ = shared.Append() }

func TestBuiltElsewhere(t *testing.T) { _ = built.Append() }

func TestOpenedElsewhere(t *testing.T) {
	_ = opened.Append()
	_ = openErr.Error()
}
`)},
		"store/internal_test.go": {Data: []byte(`package store

var other = Default
`)},
		"store/b_test.go": {Data: []byte(`package store_test

import "testing"

func TestInternalVarIsNotSeen(t *testing.T) { _ = other.Validate() }
`)},
	}
	want := strings.Join([]string{
		"TestBuiltElsewhere calls=[example.com/app/store.Store.Append×1] unresolved=0",
		"TestDeclaredElsewhere calls=[example.com/app/store.Store.Append×1] unresolved=0",
		"TestInternalVarIsNotSeen calls=[] unresolved=1",
		// error is not a type of the repository, so openErr.Error() names no target.
		"TestOpenedElsewhere calls=[example.com/app/store.Store.Append×1] unresolved=1",
	}, "\n")
	if got := callsByTest(t, tree); got != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", got, want)
	}
}

// viewpoint: behaviour
// target: golang.Adapter.Analyze
// A function literal sees the variables around it: a subtest reaches the test's variable and a
// table case's field, and = inside it types the variable around it. Its parameters and its :=
// declarations hide a variable of the same name only inside it. = with a value of unknown type
// keeps the declared type. A helper given to t.Run is walked with the test. The body of a for loop is
// read once, as a loop.
func TestGoFunctionLiteralsSeeTheVariablesAroundThem(t *testing.T) {
	tree := fstest.MapFS{
		"go.mod":         {Data: []byte("module example.com/app\n")},
		"store/store.go": {Data: []byte(storeSource)},
		"store/store_test.go": {Data: []byte(`package store_test

import (
	"testing"

	"example.com/app/store"
)

var shared = &store.Store{}

func TestSubtest(t *testing.T) {
	s := &store.Store{}
	t.Run("x", func(t *testing.T) { _ = s.Append() })
}

func TestTable(t *testing.T) {
	cases := []struct {
		name  string
		store *store.Store
	}{{"a", &store.Store{}}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { _ = tc.store.Append() })
	}
}

func TestParamHidesOnlyInside(t *testing.T) {
	c := store.Default
	func(c *store.Store) { _ = c.Append() }(nil)
	_ = c.Validate()
}

func TestAssignInsideReachesOutside(t *testing.T) {
	var s interface{ Append() int }
	func() { s = &store.Store{} }()
	_ = s.Append()
}

func TestAssignOfUnknownKeepsTheDeclaredType(t *testing.T) {
	var s *store.Store
	s = fake()
	_ = s.Append()
}

func TestForLoop(t *testing.T) {
	s := &store.Store{}
	for i := 0; i < 2; i++ {
		_ = s.Append()
	}
}

func TestRunHelper(t *testing.T) { t.Run("x", appendShared) }

func appendShared(t *testing.T) { _ = shared.Append() }

func TestDefineHidesOnlyInside(t *testing.T) {
	t.Run("x", func(t *testing.T) {
		shared := fake()
		_ = shared.Append()
	})
	_ = shared.Append()
}
`)},
	}
	want := strings.Join([]string{
		"TestAssignInsideReachesOutside calls=[example.com/app/store.Store.Append×1] unresolved=0",
		"TestAssignOfUnknownKeepsTheDeclaredType calls=[example.com/app/store.Store.Append×1] unresolved=0",
		"TestDefineHidesOnlyInside calls=[example.com/app/store.Store.Append×1] unresolved=1",
		"TestForLoop calls=[example.com/app/store.Store.Append×2] unresolved=0",
		"TestParamHidesOnlyInside calls=[example.com/app/store.Config.Validate×1 example.com/app/store.Store.Append×1] unresolved=0",
		"TestRunHelper calls=[example.com/app/store.Store.Append×1] unresolved=0",
		"TestSubtest calls=[example.com/app/store.Store.Append×1] unresolved=0",
		"TestTable calls=[example.com/app/store.Store.Append×2] unresolved=0",
	}, "\n")
	if got := callsByTest(t, tree); got != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", got, want)
	}
}
