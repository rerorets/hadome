package mutation

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/rerorets/hadome/internal/config"
)

// viewpoint: behaviour
// target: mutation.TempFor
// Design "Temporary files": a repository's test commands get the same temporary directory on
// every run, so that go test's result cache, keyed on TMPDIR for tests that call t.TempDir, keeps
// hitting; it is in the user's temporary directory, and another repository gets another one.
func TestARepositoryGetsTheSameTemporaryDirectoryEveryRun(t *testing.T) {
	t.Parallel()
	a, err := TreesFor(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	b, err := TreesFor(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first, again, other := TempFor(a), TempFor(a), TempFor(b)
	if first != again {
		t.Errorf("want the same directory for the same repository, got %s and %s", first, again)
	}
	if first == other {
		t.Errorf("want another directory for another repository, got %s for both", first)
	}
	if filepath.Dir(first) != filepath.Clean(os.TempDir()) || !strings.HasPrefix(filepath.Base(first), "hadome-mutate-") {
		t.Errorf("want hadome-mutate-<mark> in %s, got %s", os.TempDir(), first)
	}
}

// viewpoint: invariant
// target: mutation.Runner.run
// Invariant 20: a test command is given the temporary directory, finds it empty even when an
// earlier run was cut short, and leaves nothing in it, whether it exits or is ended at its time
// limit, as go test ended by SIGKILL leaves its work directory.
func TestATestCommandFindsItsTemporaryDirectoryEmptyAndLeavesNothingInIt(t *testing.T) {
	t.Parallel()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"exit", "hang"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			temp := filledTemp(t)
			r, err := newRunner(t.TempDir(), config.Mutation{Test: []string{self, StandInTemp, mode}}, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			r.clock = scriptedClock(nil)
			r.temp = temp
			out, passed, timedOut, err := r.run(r.Test, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			if timedOut != (mode == "hang") || passed != (mode == "exit") {
				t.Errorf("want timed out %v and passed %v, got %v and %v", mode == "hang", mode == "exit", timedOut, passed)
			}
			if want := "temp " + temp + " 0\n"; !strings.HasPrefix(out, want) {
				t.Errorf("want the command to say %q, got %q", want, out)
			}
			if left := entriesOf(t, temp); len(left) > 0 {
				t.Errorf("want the temporary directory empty after the run, got %q in it", left)
			}
		})
	}
}

// viewpoint: failure
// target: mutation.ownTemp
// Design "Temporary files": the temporary directory is made open to the user alone, and a path
// that is already there as anything but a directory of the user's stops the run, so that nothing
// is written into what someone else prepared in a shared temporary directory.
func TestTheTemporaryDirectoryIsTheUsersOwn(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	made := filepath.Join(dir, "made")
	if err := ownTemp(made); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(made)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o700 {
		t.Errorf("want the directory made with permissions 0700, got %v", fi.Mode().Perm())
	}
	if err := ownTemp(made); err != nil {
		t.Errorf("want a directory of the user's taken again, got %v", err)
	}
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ownTemp(file); err == nil {
		t.Error("want a file in the way to stop the run")
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(made, link); err != nil {
		t.Logf("a symbolic link could not be made, so a link in the way is not tried: %v", err)
		return
	}
	if err := ownTemp(link); err == nil {
		t.Error("want a symbolic link in the way to stop the run, even to a directory of the user's")
	}
}

// viewpoint: failure
// target: mutation.empty
// A temporary directory that is not there cannot be emptied, and says so: the run is not told
// that a test command found it empty.
func TestATemporaryDirectoryThatIsNotThereIsNotEmptied(t *testing.T) {
	t.Parallel()
	if err := empty(filepath.Join(t.TempDir(), "gone")); err == nil {
		t.Error("want a directory that is not there reported as not emptied")
	}
}

// viewpoint: failure
// target: mutation.Mutate.hold
// A temporary directory that is not the user's stops the run before any test runs, and leaves the
// lock free for the next run.
func TestATemporaryDirectoryInTheWayStopsTheRunAndFreesTheLock(t *testing.T) {
	t.Parallel()
	trees, dir := t.TempDir(), t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := (&Mutate{Trees: trees, Temp: file}).hold(); err == nil {
		t.Fatal("want a file in the way to stop the run")
	}
	release, err := (&Mutate{Trees: trees, Temp: filepath.Join(dir, "temp")}).hold()
	if err != nil {
		t.Fatalf("want the lock free after a stopped run, got %v", err)
	}
	release()
}

// filledTemp is a temporary directory for a test command, holding what an earlier run left.
func filledTemp(t *testing.T) string {
	t.Helper()
	temp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(temp, "go-build0", "b001"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(temp, "left"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return temp
}

// entriesOf are the names in dir.
func entriesOf(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// viewpoint: failure
// target: mutation.Runner.run
// Design "Temporary files": a temporary directory that cannot be emptied does not stop the run, and
// is told of once, not at every run.
func TestATemporaryDirectoryThatCannotBeEmptiedIsToldOfOnce(t *testing.T) {
	t.Parallel()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	r, err := newRunner(t.TempDir(), config.Mutation{Test: []string{self, StandInTemp, "exit"}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	r.clock = scriptedClock(nil)
	r.temp = filepath.Join(t.TempDir(), "gone") // made by no one, so it cannot be emptied
	var told []error
	r.unemptied = func(err error) { told = append(told, err) }
	for range 2 {
		if _, _, _, err := r.run(r.Test, time.Hour); err != nil {
			t.Fatalf("want the run to go on, got %v", err)
		}
	}
	if len(told) != 1 {
		t.Errorf("want the directory told of once, got %q", told)
	}
}
