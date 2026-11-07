package golang

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
)

// viewpoint: behaviour
// target: golang.Adapter.Analyze
// A test that runs a testify suite with suite.Run reaches the code through the suite's setup and
// test methods: through the suite's fields and its helper methods' declared parameters, and
// checking with its assertions.
func TestGoTestsReachTheCodeThroughTheirSuite(t *testing.T) {
	tree := fstest.MapFS{
		"go.mod": {Data: []byte("module example.com/app\n")},
		"store/store.go": {Data: []byte(`package store

// Store keeps rows.
type Store struct{ rows []string }

// New makes a Store.
func New() *Store { return &Store{} }

// Add adds a row.
func (s *Store) Add(row string) int { s.rows = append(s.rows, row); return len(s.rows) }
`)},
		"store/store_test.go": {Data: []byte(`package store_test

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"example.com/app/store"
)

type StoreSuite struct {
	suite.Suite
	s *store.Store
}

func (x *StoreSuite) SetupTest() { x.s = store.New() }

func (x *StoreSuite) TestAdd() { x.Equal(1, x.s.Add("x")) }

func (x *StoreSuite) TestTwice() { x.Require().Equal(2, x.twice()) }

func (x *StoreSuite) twice() int { x.s.Add("a"); return x.s.Add("b") }

func (x *StoreSuite) TestParam() { x.addTo(store.New()) }

func (x *StoreSuite) addTo(s *store.Store) { s.Add("p") }

func TestStoreSuite(t *testing.T) { suite.Run(t, new(StoreSuite)) }

func TestStoreSuiteLiteral(t *testing.T) { suite.Run(t, &StoreSuite{}) }
`)},
	}
	paths := []string{"store/store.go", "store/store_test.go"}
	a, err := Adapter{}.Analyze(engine.Version{FS: tree}, config.Config{}, paths)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, ts := range a.Tests {
		var calls []string
		for _, c := range ts.Calls {
			calls = append(calls, fmt.Sprintf("%s×%d", c.Target, c.Times))
		}
		got = append(got, fmt.Sprintf("%s calls=%v unresolved=%d asserts=%d", ts.Name, calls, ts.Unresolved, ts.Asserts))
	}
	sort.Strings(got)
	want := strings.Join([]string{
		"TestStoreSuite calls=[example.com/app/store.New×2 example.com/app/store.Store.Add×4] unresolved=0 asserts=2",
		"TestStoreSuiteLiteral calls=[example.com/app/store.New×2 example.com/app/store.Store.Add×4] unresolved=0 asserts=2",
	}, "\n")
	if g := strings.Join(got, "\n"); g != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", g, want)
	}
}

// viewpoint: behaviour
// target: golang.Adapter.Analyze
// Inside a suite, only the receiver's assertions count as checks: s.Run and s.T() do not, nor do a
// chain from s.T(), from another value's Require(), or from a function's result. A suite helper is followed
// each time it is called, once per chain when helpers call each other. A call on a field that is
// not of a repository type, or of an unexported method from outside its package, is not identified.
func TestGoSuiteMethodsAreReadPrecisely(t *testing.T) {
	tree := fstest.MapFS{
		"go.mod":         {Data: []byte("module example.com/app\n")},
		"store/store.go": {Data: []byte("package store\n\n// Store keeps rows.\ntype Store struct{}\n\n// Add adds a row.\nfunc (s *Store) Add(row string) int { return 1 }\n")},
		"store/store_test.go": {Data: []byte(`package store_test

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"example.com/app/store"
)

type S struct {
	suite.Suite
	s     *store.Store
	other *strings.Builder
}

func (x *S) TestOwn() {
	x.Run("sub", func() {})
	x.T().Log("x")
	other.Require().Equal(1, 1)
	fresh().Equal(1, 1)
	x.other.WriteString("a")
	x.s.flush()
}

func (x *S) TestHelpers() {
	x.ping()
	x.ping()
}

func (x *S) ping() { x.s.Add("a"); x.pong() }

func (x *S) pong() { x.ping() }

func TestS(t *testing.T) { suite.Run(t, new(S)) }
`)},
	}
	a, err := Adapter{}.Analyze(engine.Version{FS: tree}, config.Config{}, []string{"store/store.go", "store/store_test.go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Tests) != 1 {
		t.Fatalf("want the one runner test, got %d", len(a.Tests))
	}
	ts := a.Tests[0]
	var calls []string
	for _, c := range ts.Calls {
		calls = append(calls, fmt.Sprintf("%s×%d", c.Target, c.Times))
	}
	got := fmt.Sprintf("calls=%v unresolved=%d asserts=%d", calls, ts.Unresolved, ts.Asserts)
	if want := "calls=[example.com/app/store.Store.Add×2] unresolved=6 asserts=0"; got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}
