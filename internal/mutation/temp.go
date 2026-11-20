package mutation

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/rerorets/hadome/internal/engine"
)

// TempFor is the temporary directory hadome mutate gives the test commands of the repository whose
// copies are in trees (TreesFor): under the user's temporary directory, named by the repository's
// mark, which ends trees. It stays the same across runs because go test keys its test result cache
// on the environment variables a test read, and t.TempDir reads TMPDIR.
func TempFor(trees string) string {
	return filepath.Join(os.TempDir(), "hadome-mutate-"+filepath.Base(trees))
}

// GoCacheFor is the Go build cache hadome mutate gives the test commands of the repository whose
// copies are in trees (TreesFor). It is inside trees, so removing the working directory removes it
// too.
func GoCacheFor(trees string) string {
	return filepath.Join(trees, "gocache")
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

// scratch is where a test command keeps what it makes outside its copy: its temporary directory
// (TempFor) and Go's build cache (GoCacheFor). An empty one is left as hadome's.
type scratch struct{ temp, gocache string }

// env is hadome's environment with s.temp as the temporary directory, under each name a tool may
// read it by (TMPDIR on Unix, TMP and TEMP on Windows), and s.gocache as Go's build cache. A later
// entry wins over hadome's own.
func (s scratch) env() []string {
	env := os.Environ()
	if s.temp != "" {
		env = append(env, "TMPDIR="+s.temp, "TMP="+s.temp, "TEMP="+s.temp)
	}
	if s.gocache != "" {
		env = append(env, "GOCACHE="+s.gocache)
	}
	return env
}

// dropGoCache removes the test commands' build cache. A cache that cannot be removed changes no
// result, so it is only told of; the next run tries again.
func (m *Mutate) dropGoCache(lang string) {
	if m.GoCache == "" {
		return
	}
	if err := removeAll(m.GoCache); err != nil {
		fmt.Fprintln(m.Progress, engine.T("mutate: %s, the test commands' Go build cache, could not be removed; it is tried again when the next run starts: %v",
			m.GoCache, err).In(lang))
	}
}
