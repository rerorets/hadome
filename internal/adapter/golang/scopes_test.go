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
// package, whether declared with a type or made by a helper. A test file of the external _test
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

func newStore() *store.Store { return &store.Store{} }

func TestMain(m *testing.M) { shared = newStore(); m.Run() }
`)},
		"store/a_test.go": {Data: []byte(`package store_test

import "testing"

func TestDeclaredElsewhere(t *testing.T) { _ = shared.Append() }

func TestBuiltElsewhere(t *testing.T) { _ = built.Append() }
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
	}, "\n")
	if got := callsByTest(t, tree); got != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", got, want)
	}
}

// viewpoint: behaviour
// target: golang.Adapter.Analyze
// A function literal sees the variables around it: a subtest reaches the test's variable and a
// table case's field. Its parameters and its := declarations hide a variable of the same name only
// inside it.
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
		"TestDefineHidesOnlyInside calls=[example.com/app/store.Store.Append×1] unresolved=1",
		"TestParamHidesOnlyInside calls=[example.com/app/store.Config.Validate×1 example.com/app/store.Store.Append×1] unresolved=0",
		"TestSubtest calls=[example.com/app/store.Store.Append×1] unresolved=0",
		"TestTable calls=[example.com/app/store.Store.Append×2] unresolved=0",
	}, "\n")
	if got := callsByTest(t, tree); got != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", got, want)
	}
}
