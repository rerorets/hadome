//go:build !windows

package mutation

import (
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// viewpoint: invariant
// target: mutation.lock
// A run leaves no file of the lock open behind it: not when it is refused, and not when it
// releases the lock.
func TestALockLeavesNoFileOpen(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "lock")
	release, err := lock(path)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(openOn(t, path)); n != 1 {
		t.Fatalf("want the held lock open once, got %d", n)
	}
	if _, err := lock(path); err == nil {
		t.Fatal("a second lock in the same process was taken")
	}
	if n := len(openOn(t, path)); n != 1 {
		t.Errorf("a refused run left the lock open: %d open, want 1", n)
	}
	release()
	if n := len(openOn(t, path)); n != 0 {
		t.Errorf("a released lock is still open %d times", n)
	}
}

// viewpoint: invariant
// target: mutation.lock
// Releasing the lock frees it even while another copy of its open file lives on, as in a test
// command forked at that moment that has not yet replaced itself: the next run is not kept out.
func TestAReleasedLockIsFreeWhileACopyOfItsFileIsOpen(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "lock")
	release, err := lock(path)
	if err != nil {
		t.Fatal(err)
	}
	fds := openOn(t, path)
	if len(fds) != 1 {
		t.Fatalf("want the held lock open once, got %d", len(fds))
	}
	// A duplicate shares the open file the way a forked child does.
	dup, err := syscall.Dup(fds[0])
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(dup)
	release()
	again, err := lock(path)
	if err != nil {
		t.Fatalf("a released lock is kept by a copy of its open file: %v", err)
	}
	again()
}

// viewpoint: invariant
// target: mutation.lock
// A run that takes the lock but cannot write who holds it fails with that error and does not keep
// the lock: trying again fails the same way, not as held by another run. The lock file it leaves
// is taken by the next run.
func TestALockThatCannotBeWrittenIsNotKept(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "lock")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(self, StandInUnwritableLock, path).CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 2 {
		t.Fatalf("want two attempts reported, got %q", out)
	}
	for i, line := range lines {
		if line == "taken" || strings.Contains(line, "is running on this repository") {
			t.Errorf("attempt %d: want the write's error, got %q", i+1, line)
		}
	}
	if lines[0] != lines[1] {
		t.Errorf("the second attempt failed otherwise than the first: %q, then %q", lines[0], lines[1])
	}
	release, err := lock(path)
	if err != nil {
		t.Fatalf("the lock file a failed run left cannot be taken by the next: %v", err)
	}
	release()
}

// StandInUnwritable is the run of StandInUnwritableLock: files may not grow in it, so the lock is
// taken but who holds it cannot be written. Its output must be a pipe: a file could not grow either.
func StandInUnwritable(path string) int {
	// A write past the limit raises SIGXFSZ, which would end the process instead of failing the write.
	signal.Ignore(syscall.SIGXFSZ)
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &syscall.Rlimit{}); err != nil {
		os.Stdout.WriteString("setrlimit: " + err.Error() + "\n")
		return 2
	}
	for range 2 {
		if release, err := lock(path); err != nil {
			os.Stdout.WriteString(err.Error() + "\n")
		} else {
			os.Stdout.WriteString("taken\n")
			release()
		}
	}
	return 0
}

// openOn is the descriptors of this process open on the file at path.
func openOn(t *testing.T, path string) []int {
	t.Helper()
	var want syscall.Stat_t
	if err := syscall.Stat(path, &want); err != nil {
		t.Fatal(err)
	}
	// Names only: other tests open and close descriptors meanwhile, and reading the directory with
	// each entry's details fails on one closed between the listing and the details.
	dir, err := os.Open("/dev/fd")
	if err != nil {
		t.Fatal(err)
	}
	names, err := dir.Readdirnames(-1)
	dir.Close()
	if err != nil {
		t.Fatal(err)
	}
	var fds []int
	for _, name := range names {
		fd, err := strconv.Atoi(name)
		if err != nil {
			continue
		}
		var st syscall.Stat_t
		// A descriptor gone or reused since the listing is not on this test's own file, which only
		// this test opens.
		if syscall.Fstat(fd, &st) == nil && st.Dev == want.Dev && st.Ino == want.Ino {
			fds = append(fds, fd)
		}
	}
	return fds
}
