package golang

import (
	"slices"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/facts"
)

// viewpoint: behaviour
// target: golang.walker.packageCall
// A product function that calls an input or output package does input or output; one that does
// not, does none.
func TestGoFunctionsCallingIOPackagesDoIO(t *testing.T) {
	a := analyzeTree(t, fstest.MapFS{
		"go.mod": {Data: []byte("module example.com/app\n")},
		"store/store.go": {Data: []byte("// Package store keeps rows.\npackage store\n\nimport \"os\"\n\n" +
			"// Load reads rows.\nfunc Load() ([]byte, error) { return os.ReadFile(\"rows\") }\n\n" +
			"// Count counts rows.\nfunc Count(rows []byte) int { return len(rows) }\n")},
	})
	io := map[string]bool{}
	for _, s := range a.Symbols {
		io[s.ID] = s.IO
	}
	if !io["example.com/app/store.Load"] || io["example.com/app/store.Count"] {
		t.Errorf("IO %v, want Load only", io)
	}
}

// viewpoint: behaviour
// target: golang.walker.packageCall
// suite.Run makes the suite's test methods tests, which call what they call.
func TestGoSuiteRunMakesTests(t *testing.T) {
	a := analyzeTree(t, fstest.MapFS{
		"go.mod":         {Data: []byte("module example.com/app\n")},
		"store/store.go": {Data: []byte("// Package store keeps rows.\npackage store\n\n// Seed adds a row.\nfunc Seed() {}\n")},
		"store/store_test.go": {Data: []byte(`package store_test

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"example.com/app/store"
)

type StoreSuite struct{ suite.Suite }

func (s *StoreSuite) TestSeed() {
	store.Seed()
	s.Require().True(true)
}

func TestStore(t *testing.T) { suite.Run(t, new(StoreSuite)) }
`)},
	})
	found := false
	for _, ts := range a.Tests {
		for _, c := range ts.Calls {
			found = found || c.Target == "example.com/app/store.Seed"
		}
	}
	if !found {
		t.Errorf("no test calls store.Seed: %+v", a.Tests)
	}
}

// viewpoint: behaviour
// target: golang.walker.packageCall
// An assertion that cannot fail is a hazard at its line: True(t, true), False(t, false), and Equal of
// a value with itself when the value calls nothing.
func TestGoAssertionsThatCannotFail(t *testing.T) {
	a := analyzeTree(t, fstest.MapFS{
		"go.mod":         {Data: []byte("module example.com/app\n")},
		"store/store.go": {Data: []byte("// Package store keeps rows.\npackage store\n\n// Count counts.\nfunc Count() int { return 1 }\n")},
		"store/store_test.go": {Data: []byte(`package store_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"example.com/app/store"
)

func TestCount(t *testing.T) {
	n := store.Count()
	assert.True(t, true)
	assert.False(t, false)
	assert.Equal(t, n, n)
	assert.Equal(t, store.Count(), store.Count())
	assert.True(t, n > 0)
}
`)},
	})
	var lines []int
	for _, h := range a.Tests[0].Hazards {
		if h.Category == facts.HazardAlwaysTrue {
			lines = append(lines, h.Line)
		}
	}
	if !slices.Equal(lines, []int{13, 14, 15}) {
		t.Errorf("always-true hazards at lines %v, want 13, 14 and 15", lines)
	}
}
