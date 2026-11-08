package mutation

import (
	"testing"
	"testing/fstest"
)

// viewpoint: behaviour
// target: mutation.unbuilt, mutation.buildContext
// Whether a Go file is outside the build follows the environment written in the test command:
// CGO_ENABLED=, GOOS=, GOARCH= and -tags. Files of other languages are never outside it.
func TestTheTestCommandDecidesWhatIsBuilt(t *testing.T) {
	fsys := fstest.MapFS{
		"a/cgo.go":          {Data: []byte("//go:build cgo\n\npackage a\n")},
		"a/nocgo.go":        {Data: []byte("//go:build !cgo\n\npackage a\n")},
		"a/plan9.go":        {Data: []byte("package a\n")},
		"a/x_plan9.go":      {Data: []byte("package a\n")},
		"a/x_plan9_test.go": {Data: []byte("package a\n")},
		"a/x_mips64.go":     {Data: []byte("package a\n")},
		"a/extra.go":        {Data: []byte("//go:build extra && more\n\npackage a\n")},
		"a/_scratch.go":     {Data: []byte("package a\n")},
		"a/ignored.ts":      {Data: []byte("//go:build ignore\n")},
	}
	for _, tc := range []struct {
		test []string
		file string
		out  bool
	}{
		{[]string{"env", "CGO_ENABLED=0", "go", "test", "./..."}, "a/cgo.go", true},
		{[]string{"env", "CGO_ENABLED=0", "go", "test", "./..."}, "a/nocgo.go", false},
		{[]string{"env", "CGO_ENABLED=1", "go", "test", "./..."}, "a/nocgo.go", true},
		{[]string{"env", "CGO_ENABLED=1", "go", "test", "./..."}, "a/cgo.go", false},
		{[]string{"env", "GOOS=plan9", "go", "test"}, "a/x_plan9.go", false},
		{[]string{"env", "GOOS=plan9", "go", "test"}, "a/x_plan9_test.go", false},
		{[]string{"env", "GOOS=windows", "go", "test"}, "a/x_plan9.go", true},
		{[]string{"env", "GOOS=windows", "go", "test"}, "a/x_plan9_test.go", true},
		{[]string{"env", "GOOS=windows", "go", "test"}, "a/plan9.go", false},
		{[]string{"env", "GOARCH=mips64", "go", "test"}, "a/x_mips64.go", false},
		{[]string{"env", "GOARCH=amd64", "go", "test"}, "a/x_mips64.go", true},
		{[]string{"go", "test", "./..."}, "a/extra.go", true},
		{[]string{"go", "test", "-tags", "extra,more", "./..."}, "a/extra.go", false},
		{[]string{"go", "test", "-tags=extra,more", "./..."}, "a/extra.go", false},
		{[]string{"go", "test", "-tags=extra", "./..."}, "a/extra.go", true},
		{[]string{"go", "test", "./..."}, "a/_scratch.go", true},
		{[]string{"go", "test", "./..."}, "a/ignored.ts", false},
	} {
		if got := unbuilt(buildContext(tc.test), fsys, tc.file); got != tc.out {
			t.Errorf("%v, %s: want outside the build %v, got %v", tc.test, tc.file, tc.out, got)
		}
	}
}
