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
// read once, as a loop. A helper given to t.Run twice is walked twice.
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

func TestRunHelperTwice(t *testing.T) {
	t.Run("a", appendShared)
	t.Run("b", appendShared)
}

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
		"TestRunHelperTwice calls=[example.com/app/store.Store.Append×2] unresolved=0",
		"TestSubtest calls=[example.com/app/store.Store.Append×1] unresolved=0",
		"TestTable calls=[example.com/app/store.Store.Append×2] unresolved=0",
	}, "\n")
	if got := callsByTest(t, tree); got != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", got, want)
	}
}

// viewpoint: behaviour
// target: golang.Adapter.Analyze
// A helper's parameter declared with an interface, or with a type the adapter cannot read, has the
// type of the value the caller passed, so a method called on it is the method of that value's type.
// When the caller's value has no known type either, the call is unresolved, never the interface's
// method. A parameter declared with a concrete type keeps that type.
func TestGoHelperInterfaceParametersTakeTheCallersType(t *testing.T) {
	tree := fstest.MapFS{
		"go.mod": {Data: []byte("module example.com/app\n")},
		"store/store.go": {Data: []byte(`package store

import "net/http"

// Receiver accepts writes over HTTP.
type Receiver struct{ n int }

// ServeHTTP counts the request.
func (r *Receiver) ServeHTTP(w http.ResponseWriter, req *http.Request) { r.n++ }

// Appender appends one entry.
type Appender interface{ Append() int }

// Store holds entries.
type Store struct{ n int }

// Append adds one entry.
func (s *Store) Append() int { s.n++; return s.n }
`)},
		"store/store_test.go": {Data: []byte(`package store

import (
	"net/http"
	"testing"
)

func add(t *testing.T, a Appender) { _ = a.Append() }

func addTwice(t *testing.T, a Appender) { add(t, a); add(t, a) }

func post(t *testing.T, h http.Handler) { h.ServeHTTP(nil, nil) }

func addConcrete(t *testing.T, s *Store) { _ = s.Append() }

func TestRepositoryInterface(t *testing.T) { add(t, &Store{}) }

func TestThroughTwoHelpers(t *testing.T) {
	s := &Store{}
	addTwice(t, s)
}

func TestExternalInterface(t *testing.T) { post(t, &Receiver{}) }

func TestUnknownArgument(t *testing.T) { add(t, open()) }

func TestConcreteParameter(t *testing.T) { addConcrete(t, nil) }
`)},
	}
	want := strings.Join([]string{
		"TestConcreteParameter calls=[example.com/app/store.Store.Append×1] unresolved=0",
		"TestExternalInterface calls=[example.com/app/store.Receiver.ServeHTTP×1] unresolved=0",
		"TestRepositoryInterface calls=[example.com/app/store.Store.Append×1] unresolved=0",
		"TestThroughTwoHelpers calls=[example.com/app/store.Store.Append×2] unresolved=0",
		"TestUnknownArgument calls=[] unresolved=1",
	}, "\n")
	if got := callsByTest(t, tree); got != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", got, want)
	}
}

// viewpoint: behaviour
// target: golang.Adapter.Analyze
// Every variable set from one call that returns several values has the type of its own result, and
// a variable holding a method value (f := s.Append) makes a call of it a call of that method, from
// the package itself (an unexported method included) and from its external test package.
func TestGoMultipleResultsAndMethodValues(t *testing.T) {
	tree := fstest.MapFS{
		"go.mod": {Data: []byte("module example.com/app\n")},
		"store/store.go": {Data: []byte(`package store

// Store holds entries.
type Store struct{ n int }

// Append adds one entry.
func (s *Store) Append() int { s.n++; return s.n }

func (s *Store) reset() { s.n = 0 }

// Queue holds jobs.
type Queue struct{ n int }

// Push adds one job.
func (q *Queue) Push() { q.n++ }

// Pair returns a store and a queue.
func Pair() (*Store, *Queue) { return &Store{}, &Queue{} }

// Three returns a store, a queue and a store.
func Three() (a *Store, q *Queue, b *Store) { return &Store{}, &Queue{}, &Store{} }
`)},
		"store/store_test.go": {Data: []byte(`package store

import "testing"

func TestPair(t *testing.T) {
	s, q := Pair()
	q.Push()
	_ = s.Append()
}

func TestNamedResults(t *testing.T) {
	var a, q, b = Three()
	q.Push()
	_ = a.Append()
	_ = b.Append()
}

func TestAssignedLater(t *testing.T) {
	var s *Store
	var q *Queue
	s, q = Pair()
	q.Push()
	_ = s.Append()
}

func TestMethodValue(t *testing.T) {
	s := &Store{}
	f := s.Append
	if f() != 1 {
		t.Fatal("first append")
	}
	r := s.reset
	r()
}
`)},
		"store/ext_test.go": {Data: []byte(`package store_test

import (
	"testing"

	"example.com/app/store"
)

func TestMethodValueOutside(t *testing.T) {
	s := &store.Store{}
	f := s.Append
	_ = f()
}
`)},
	}
	want := strings.Join([]string{
		"TestAssignedLater calls=[example.com/app/store.Pair×1 example.com/app/store.Queue.Push×1 example.com/app/store.Store.Append×1] unresolved=0",
		"TestMethodValue calls=[example.com/app/store.Store.Append×1 example.com/app/store.Store.reset×1] unresolved=0",
		"TestMethodValueOutside calls=[example.com/app/store.Store.Append×1] unresolved=0",
		"TestNamedResults calls=[example.com/app/store.Queue.Push×1 example.com/app/store.Store.Append×2 example.com/app/store.Three×1] unresolved=0",
		"TestPair calls=[example.com/app/store.Pair×1 example.com/app/store.Queue.Push×1 example.com/app/store.Store.Append×1] unresolved=0",
	}, "\n")
	if got := callsByTest(t, tree); got != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", got, want)
	}
}
