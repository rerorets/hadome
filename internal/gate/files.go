package gate

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/rerorets/hadome/internal/engine"
)

// workspace holds the directories gates are given, writing a copy of a version that is not on
// disk once per check.
type workspace struct {
	dirs map[string]string // by version label
	tmp  []string
}

func (w *workspace) root(v engine.Version) (string, error) {
	if v.Root != "" {
		return filepath.Abs(v.Root)
	}
	if d, ok := w.dirs[v.Label]; ok {
		return d, nil
	}
	d, err := os.MkdirTemp("", "hadome-version-")
	if err != nil {
		return "", err
	}
	w.tmp = append(w.tmp, d)
	if err := copyFS(d, v.FS); err != nil {
		return "", err
	}
	w.dirs[v.Label] = d
	return d, nil
}

// Close removes the copies.
func (w *workspace) Close() error {
	var first error
	for _, d := range w.tmp {
		if err := os.RemoveAll(d); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func copyFS(dir string, fsys fs.FS) error {
	return fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		dst := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o644)
	})
}

// selectFiles lists the version's files that match one of the globs, or all of them when there are none.
func selectFiles(v engine.Version, globs []string) ([]string, error) {
	var out []string
	err := fs.WalkDir(v.FS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if len(globs) == 0 || matchesAny(globs, p) {
			out = append(out, p)
		}
		return nil
	})
	return out, err
}

func matchesAny(globs []string, p string) bool {
	for _, g := range globs {
		if match(strings.Split(g, "/"), strings.Split(p, "/")) {
			return true
		}
	}
	return false
}

// match matches path elements against glob elements, where "**" matches any number of elements.
func match(glob, elems []string) bool {
	if len(glob) == 0 {
		return len(elems) == 0
	}
	if glob[0] == "**" {
		for i := 0; i <= len(elems); i++ {
			if match(glob[1:], elems[i:]) {
				return true
			}
		}
		return false
	}
	if len(elems) == 0 {
		return false
	}
	ok, err := path.Match(glob[0], elems[0])
	return err == nil && ok && match(glob[1:], elems[1:])
}
