package mutation

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// TempFor is the temporary directory hadome mutate gives the test commands of the repository whose
// copies are in trees (TreesFor): under the user's temporary directory, named by the repository's
// mark, which ends trees. It stays the same across runs because go test keys its test result cache
// on the environment variables a test read, and t.TempDir reads TMPDIR.
func TempFor(trees string) string {
	return filepath.Join(os.TempDir(), "hadome-mutate-"+filepath.Base(trees))
}

// ownTemp makes dir, open to the user alone, unless it is already a directory the user owns. In a
// temporary directory shared with other users, a path someone else prepared is never written into.
func ownTemp(dir string) error {
	fi, err := os.Lstat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return os.Mkdir(dir, 0o700)
	}
	if err != nil {
		return err
	}
	if !fi.IsDir() || !ownedByUser(fi) {
		return fmt.Errorf("%s, the temporary directory hadome mutate gives the test command, is not a directory of yours; "+
			"find out who made it, and remove it", dir)
	}
	return nil
}

// empty removes everything in dir, and leaves dir. A test killed before its cleanups ran can leave
// a directory it made read-only; removeAll makes such directories writable first.
func empty(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var errs []error
	for _, e := range entries {
		errs = append(errs, removeAll(filepath.Join(dir, e.Name())))
	}
	return errors.Join(errs...)
}

// tempEnv is hadome's environment with dir as the temporary directory, under each name a tool may
// read it by: TMPDIR on Unix, TMP and TEMP on Windows. A later entry wins over hadome's own.
func tempEnv(dir string) []string {
	return append(os.Environ(), "TMPDIR="+dir, "TMP="+dir, "TEMP="+dir)
}
