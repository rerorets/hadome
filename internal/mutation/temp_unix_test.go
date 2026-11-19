//go:build !windows

package mutation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// viewpoint: behaviour
// target: mutation.empty
// A directory a test made read-only and left behind, killed before its cleanups ran, is emptied
// all the same: nothing in the temporary directory is anyone's but the test commands'.
func TestADirectoryLeftReadOnlyIsEmptiedAllTheSame(t *testing.T) {
	t.Parallel()
	temp := t.TempDir()
	locked := filepath.Join(temp, "a", "locked")
	if err := os.MkdirAll(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locked, "x"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{locked, filepath.Dir(locked)} {
		if err := os.Chmod(d, 0o500); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { os.Chmod(filepath.Dir(locked), 0o755); os.Chmod(locked, 0o755) })
	if err := empty(temp); err != nil {
		t.Fatal(err)
	}
	if left := entriesOf(t, temp); len(left) > 0 {
		t.Errorf("want the directory emptied, got %q in it", left)
	}
}

// viewpoint: failure
// target: mutation.ownTemp
// A temporary directory that cannot be looked at, as one in a directory the user cannot read, stops
// the run: whether it is the user's own is not known.
func TestATemporaryDirectoryThatCannotBeLookedAtStopsTheRun(t *testing.T) {
	t.Parallel()
	if os.Getuid() == 0 {
		t.Skip("root looks into a directory it cannot read, so nothing cannot be looked at")
	}
	closed := filepath.Join(t.TempDir(), "closed")
	if err := os.Mkdir(closed, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(closed, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(closed, 0o755) })
	if err := ownTemp(filepath.Join(closed, "temp")); err == nil {
		t.Error("want a directory that cannot be looked at to stop the run")
	}
}

// viewpoint: failure
// target: mutation.empty
// What cannot be removed even with its directories made writable, as an entry of a temporary
// directory the user cannot write, is reported as not emptied.
func TestWhatCannotBeRemovedIsReportedAsNotEmptied(t *testing.T) {
	t.Parallel()
	if os.Getuid() == 0 {
		t.Skip("root removes entries of a directory it cannot write, so nothing cannot be removed")
	}
	temp := t.TempDir()
	if err := os.WriteFile(filepath.Join(temp, "x"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(temp, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(temp, 0o755) })
	if err := empty(temp); err == nil {
		t.Error("want an entry that could not be removed reported")
	}
}

// viewpoint: failure
// target: mutation.Mutate.dropGoCache
// Design "The Go build cache": a build cache that cannot be removed, as one in a directory the user
// cannot write, does not stop the run, and is told of.
func TestABuildCacheThatCannotBeRemovedIsToldOf(t *testing.T) {
	t.Parallel()
	if os.Getuid() == 0 {
		t.Skip("root removes entries of a directory it cannot write, so nothing cannot be removed")
	}
	closed := t.TempDir()
	cache := filepath.Join(closed, "gocache")
	if err := os.Mkdir(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(closed, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(closed, 0o755) })
	var progress strings.Builder
	m := &Mutate{GoCache: cache, Progress: &progress}
	m.dropGoCache("en")
	if want := cache + ", the test commands' Go build cache, could not be removed"; !strings.Contains(progress.String(), want) {
		t.Errorf("want %q told, got %q", want, progress.String())
	}
}
