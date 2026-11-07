package walk_test

import (
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/adapter/walk"
)

// viewpoint: behaviour
// target: walk.Files
// Directories that start with an underscore hold the product's own code and tests in common
// frameworks (jest's __tests__, Next.js's _components, Python's _internal), so they are read.
// Installed dependencies, build outputs and hidden directories are not.
func TestFilesReadsUnderscoreDirectoriesAndSkipsToolDirectories(t *testing.T) {
	fsys := fstest.MapFS{}
	for _, p := range []string{
		"src/__tests__/a.test.ts", "app/_components/b.tsx", "pkg/_internal/c.py", "src/d.ts",
		".git/HEAD", "node_modules/lib/index.js", "vendor/lib/lib.go", "dist/e.js", "build/f.js",
		"target/g.class", "testdata/h.txt", "pkg/__pycache__/c.pyc", "src/.cache/i.ts",
	} {
		fsys[p] = &fstest.MapFile{Data: []byte("x\n")}
	}
	got, err := walk.Files(fsys)
	if err != nil {
		t.Fatal(err)
	}
	want := "app/_components/b.tsx pkg/_internal/c.py src/__tests__/a.test.ts src/d.ts"
	if strings.Join(got, " ") != want {
		t.Errorf("got %v, want %s", got, want)
	}
}

// viewpoint: behaviour
// target: walk.SupportDir, walk.GoSupportDir, walk.HelperDir
// A directory named for test helpers holds test support in every language. test/ and e2e/ do in Go,
// TypeScript and Python, at any depth. tests/ does in TypeScript and Python only: in Go a directory
// names a package, and a package called tests may be product code. A name that only contains such a
// word (latest/, contests/, e2etools/) is no support directory.
func TestSupportDirectories(t *testing.T) {
	var got []string
	for _, p := range []string{
		"internal/testutil/a.go", "pkg/testhelpers/b.go", "src/__mocks__/c.ts", "web/test-utils/d.ts",
		"test/e.ts", "rules/tests/f.go", "e2e/g.ts", "internal/latest/h.go", "contests/i.ts", "testing.go",
		"test/helpers/j.go", "svc/e2e/client/k.go", "e2etools/l.go",
	} {
		got = append(got, fmt.Sprintf("%s helper=%v go=%v support=%v", p, walk.HelperDir(p), walk.GoSupportDir(p), walk.SupportDir(p)))
	}
	want := strings.Join([]string{
		"internal/testutil/a.go helper=true go=true support=true",
		"pkg/testhelpers/b.go helper=true go=true support=true",
		"src/__mocks__/c.ts helper=true go=true support=true",
		"web/test-utils/d.ts helper=true go=true support=true",
		"test/e.ts helper=false go=true support=true",
		"rules/tests/f.go helper=false go=false support=true",
		"e2e/g.ts helper=false go=true support=true",
		"internal/latest/h.go helper=false go=false support=false",
		"contests/i.ts helper=false go=false support=false",
		"testing.go helper=false go=false support=false",
		"test/helpers/j.go helper=false go=true support=true",
		"svc/e2e/client/k.go helper=false go=true support=true",
		"e2etools/l.go helper=false go=false support=false",
	}, "\n")
	if g := strings.Join(got, "\n"); g != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", g, want)
	}
}
