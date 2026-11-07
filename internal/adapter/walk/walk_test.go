package walk_test

import (
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
