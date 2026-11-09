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
// A test that calls a function of a test support package is marked as calling support code, even
// when that function reaches no product code; a test that calls none is not.
func TestTestsThatCallASupportPackageAreMarked(t *testing.T) {
	tree := fstest.MapFS{
		"go.mod":                    {Data: []byte("module example.com/app\n")},
		"store/store.go":            {Data: []byte("// Package store keeps rows.\npackage store\n\n// Open opens.\nfunc Open() int { return 1 }\n")},
		"internal/testutil/zone.go": {Data: []byte("package testutil\n\nfunc Zone() string { return \"UTC\" }\n")},
		"internal/testutil/zone_test.go": {Data: []byte(`package testutil_test

import (
	"testing"

	"example.com/app/internal/testutil"
	"example.com/app/store"
)

func TestZone(t *testing.T) {
	if testutil.Zone() != "UTC" {
		t.Fatal("want UTC")
	}
}

func TestOpen(t *testing.T) {
	if store.Open() != 1 {
		t.Fatal("want 1")
	}
}
`)},
	}
	paths := []string{"internal/testutil/zone.go", "internal/testutil/zone_test.go", "store/store.go"}
	a, err := Adapter{}.Analyze(engine.Version{FS: tree}, config.Config{}, paths)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, ts := range a.Tests {
		got = append(got, fmt.Sprintf("%s support=%v", ts.Name, ts.CallsSupport))
	}
	sort.Strings(got)
	if g, want := strings.Join(got, "\n"), "TestOpen support=false\nTestZone support=true"; g != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", g, want)
	}
}
