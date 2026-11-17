package mutation

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// held is the error of a run that finds another holding the lock. It never waits: a run that waits
// gives the user no sign of why nothing moves.
func held(path string) error {
	who := "another hadome mutate"
	if data, err := os.ReadFile(path); err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
			who = fmt.Sprintf("another hadome mutate (process %d)", pid)
		}
	}
	return fmt.Errorf("%s is running on this repository and holds %s; run again when it has finished. "+
		"The operating system releases the lock when that process ends, even when it crashes", who, path)
}

// writePid records who holds the lock, for the message of a run that finds it held.
func writePid(f *os.File) error {
	if err := f.Truncate(0); err != nil {
		return err
	}
	_, err := f.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"), 0)
	return err
}

// hold takes the lock of the working directory, so that no other hadome mutate copies into it.
func (m *Mutate) hold() (release func(), err error) {
	if m.Trees == "" {
		return nil, errors.New("no working directory to copy the versions into")
	}
	if err := os.MkdirAll(m.Trees, 0o755); err != nil {
		return nil, err
	}
	release, err = lock(filepath.Join(m.Trees, "lock"))
	if err != nil || m.Temp == "" {
		return release, err
	}
	if err := ownTemp(m.Temp); err != nil {
		release()
		return nil, err
	}
	return release, nil
}
