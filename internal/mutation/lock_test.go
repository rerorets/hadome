package mutation

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// viewpoint: invariant
// target: mutation.lock
// Invariant 10: while one hadome mutate holds a repository, another is refused at once, with the
// process that holds it and the lock file named, never left waiting. When the holder ends, even when
// it is killed, the lock is free again: no stale lock is left to remove.
func TestASecondRunIsRefusedAndAKilledRunLeavesNoLock(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "lock")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	holder := exec.Command(self, StandInHoldLock, path)
	stdin, err := holder.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	stdout, err := holder.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	if line, _ := bufio.NewReader(stdout).ReadString('\n'); strings.TrimSpace(line) != "held" {
		holder.Process.Kill()
		holder.Wait()
		t.Fatalf("the holding process did not take the lock: %q", line)
	}

	_, err = lock(path)
	if err == nil {
		t.Fatal("a second run took a lock another process holds")
	}
	if pid := strconv.Itoa(holder.Process.Pid); !strings.Contains(err.Error(), "process "+pid) || !strings.Contains(err.Error(), path) {
		t.Errorf("want the holder's process %s and %s named, got %v", pid, path, err)
	}

	if err := holder.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	holder.Wait()
	release, err := lock(path)
	if err != nil {
		t.Fatalf("the lock of a killed run is still held: %v", err)
	}
	if _, err := lock(path); err == nil || !strings.Contains(err.Error(), "process "+strconv.Itoa(os.Getpid())) {
		t.Errorf("want a second lock in the same process refused, naming it, got %v", err)
	}
	release()
	again, err := lock(path)
	if err != nil {
		t.Fatalf("a released lock cannot be taken: %v", err)
	}
	again()
}
