package mutation

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/rerorets/hadome/internal/engine"
)

// viewpoint: behaviour
// target: mutation.writeTree
// Invariant 9: the copy holds exactly the version. What an earlier run left (a stale file, build
// output, a broken file, a file where a directory now is) is gone or rewritten; a file that already
// holds the version's contents keeps its modification time, so caches keyed by it hit.
func TestTheCopyHoldsExactlyTheVersion(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	old := time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)
	for name, data := range map[string]string{
		"same.go":          "package a\n",
		"broken.go":        "package a // BROKEN\n",
		"gone.go":          "package a\n",
		"out/build/cache":  "output",
		"store":            "a file where a directory is now",
		"keep/nested/x.go": "package x // old\n",
	} {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(filepath.Join(dir, "same.go"), old, old); err != nil {
		t.Fatal(err)
	}
	version := fstest.MapFS{
		"same.go":          {Data: []byte("package a\n")},
		"broken.go":        {Data: []byte("package a\n")},
		"store/store.go":   {Data: []byte("package store\n")},
		"keep/nested/x.go": {Data: []byte("package x\n")},
	}
	if err := writeTree(engine.Version{FS: version}, dir); err != nil {
		t.Fatal(err)
	}
	var got []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == dir {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		got = append(got, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"broken.go", "keep", "keep/nested", "keep/nested/x.go", "same.go", "store", "store/store.go"}
	if !slices.Equal(got, want) {
		t.Fatalf("want exactly %q, got %q", want, got)
	}
	for name, f := range version {
		if data, _ := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name))); string(data) != string(f.Data) {
			t.Errorf("%s holds %q, want %q", name, data, f.Data)
		}
	}
	if info, err := os.Stat(filepath.Join(dir, "same.go")); err != nil || !info.ModTime().Equal(old) {
		t.Errorf("a file already holding the version's contents was written again: %v, %v", info.ModTime(), err)
	}
}

// viewpoint: behaviour
// target: mutation.TreesFor
// The working directory is under the user's cache directory and depends only on which directory the
// repository is: the same for a relative path or a path through a symbolic link, different for
// another repository.
func TestEachRepositoryHasItsOwnFixedWorkingDirectory(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("HOME", cache)
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("LocalAppData", cache)
	userCache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	a, b := t.TempDir(), t.TempDir()
	dirA, err := TreesFor(a)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(dirA, filepath.Join(userCache, "hadome", "mutate")+string(filepath.Separator)) {
		t.Errorf("want it under %s, got %s", filepath.Join(userCache, "hadome", "mutate"), dirA)
	}
	link := filepath.Join(t.TempDir(), "link")
	linked := os.Symlink(a, link) == nil
	t.Chdir(a)
	for _, same := range []string{a, ".", link} {
		if same == link && !linked {
			t.Logf("this system cannot make a symbolic link; %s is not compared", link)
			continue
		}
		if got, err := TreesFor(same); err != nil || got != dirA {
			t.Errorf("%s: want %s, got %s (%v)", same, dirA, got, err)
		}
	}
	if dirB, err := TreesFor(b); err != nil || dirB == dirA {
		t.Errorf("another repository shares the working directory %s (%v)", dirB, err)
	}
}
