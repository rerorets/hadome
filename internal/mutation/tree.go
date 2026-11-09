package mutation

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"

	"github.com/rerorets/hadome/internal/engine"
)

// TreesFor is the working directory hadome mutate keeps the copies of the repository at root in:
// under the user's cache directory, named by a hash of the root's absolute path with symbolic links
// followed. The same root always gets the same directory, so build caches keyed by path hit across
// runs.
func TreesFor(root string) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if abs, err = filepath.EvalSymlinks(abs); err != nil {
		return "", err
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("no cache directory to copy the versions into: %w", err)
	}
	sum := sha256.Sum256([]byte(abs))
	return filepath.Join(cache, "hadome", "mutate", hex.EncodeToString(sum[:])[:16]), nil
}

// writeTree makes dir hold exactly the files of v. Files and directories v lacks are removed, files
// whose contents differ are written, and files that already hold v's contents are left alone, so
// that caches keyed by modification time hit too.
func writeTree(v engine.Version, dir string) error {
	want := map[string][]byte{}
	dirs := map[string]bool{".": true}
	err := fs.WalkDir(v.FS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(v.FS, p)
		if err != nil {
			return err
		}
		want[p] = data
		for d := path.Dir(p); d != "."; d = path.Dir(d) {
			dirs[d] = true
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := prune(dir, want, dirs); err != nil {
		return err
	}
	names := make([]string, 0, len(want))
	for p := range want {
		names = append(names, p)
	}
	sort.Strings(names)
	for _, p := range names {
		dst := filepath.Join(dir, filepath.FromSlash(p))
		if info, err := os.Lstat(dst); err == nil && info.Mode().IsRegular() {
			if have, err := os.ReadFile(dst); err == nil && bytes.Equal(have, want[p]) {
				if info.Mode().Perm() != 0o644 {
					if err := os.Chmod(dst, 0o644); err != nil { // keeps the modification time
						return err
					}
				}
				continue
			}
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, want[p], 0o644); err != nil {
			return err
		}
		if err := os.Chmod(dst, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// prune removes from dir everything that is not a file in want or a directory in dirs. A file where
// a directory is wanted, or the other way round, is removed too.
func prune(dir string, want map[string][]byte, dirs map[string]bool) error {
	var gone []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		_, isFile := want[rel]
		switch {
		case d.IsDir() && dirs[rel]:
			return nil
		case !d.IsDir() && isFile && d.Type().IsRegular():
			return nil
		}
		gone = append(gone, p)
		if d.IsDir() {
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, p := range gone {
		if err := removeAll(p); err != nil {
			return err
		}
	}
	return nil
}

// removeAll removes a path, making read-only directories in it writable first: some tools, such as
// Go's module cache, leave directories that cannot be removed as they are.
func removeAll(p string) error {
	if err := os.RemoveAll(p); err == nil {
		return nil
	}
	filepath.WalkDir(p, func(q string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			os.Chmod(q, 0o755)
		}
		return nil
	})
	return os.RemoveAll(p)
}
