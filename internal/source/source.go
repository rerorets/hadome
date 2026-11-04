// Package source provides the two versions the engine compares.
//
// Design: docs/design/engine.md (Sources)
package source

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/engine"
)

// Dirs compares two directories. It never reports renames.
type Dirs struct{ BaseDir, HeadDir string }

// Base returns the base directory.
func (d Dirs) Base() (engine.Version, error) {
	return engine.Version{Label: "base", FS: os.DirFS(d.BaseDir), Root: d.BaseDir}, nil
}

// Head returns the head directory.
func (d Dirs) Head() (engine.Version, error) {
	return engine.Version{Label: "head", FS: os.DirFS(d.HeadDir), Root: d.HeadDir}, nil
}

// Renames is always empty for directories.
func (d Dirs) Renames() (map[string]string, error) { return nil, nil }

// Git compares two versions of the repository at Root: the revision BaseRev, and the revision
// HeadRev or, when HeadRev is empty, the working tree. The working tree holds tracked files and
// untracked files that are not ignored.
type Git struct{ Root, BaseRev, HeadRev string }

// Base reads every file of the base revision.
func (g Git) Base() (engine.Version, error) { return g.revision(g.BaseRev) }

func (g Git) revision(rev string) (engine.Version, error) {
	names, err := g.lines("ls-tree", "-r", "-z", "--name-only", rev)
	if err != nil {
		return engine.Version{}, err
	}
	fsys, err := g.catFiles(rev, names)
	if err != nil {
		return engine.Version{}, err
	}
	return engine.Version{Label: rev, FS: fsys}, nil
}

// Head reads the head revision, or the working tree. The working tree is copied into memory so
// that the check sees one consistent snapshot.
func (g Git) Head() (engine.Version, error) {
	if g.HeadRev != "" {
		return g.revision(g.HeadRev)
	}
	names, err := g.lines("ls-files", "-z", "-c", "-o", "--exclude-standard")
	if err != nil {
		return engine.Version{}, err
	}
	fsys := fstest.MapFS{}
	for _, n := range names {
		b, err := os.ReadFile(filepath.Join(g.Root, filepath.FromSlash(n)))
		if os.IsNotExist(err) {
			continue // tracked but deleted in the working tree
		}
		if err != nil {
			return engine.Version{}, err
		}
		fsys[n] = &fstest.MapFile{Data: b}
	}
	return engine.Version{Label: "working tree", FS: fsys}, nil
}

// Renames maps base paths to head paths for files git detects as renamed.
func (g Git) Renames() (map[string]string, error) {
	args := []string{"diff", "-z", "-M", "--name-status", "--no-color", g.BaseRev}
	if g.HeadRev != "" {
		args = append(args, g.HeadRev)
	}
	fields, err := g.lines(args...)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for i := 0; i < len(fields); i++ {
		if strings.HasPrefix(fields[i], "R") && i+2 < len(fields) {
			out[fields[i+1]] = fields[i+2]
			i += 2
		} else if strings.HasPrefix(fields[i], "C") {
			i += 2
		} else {
			i++
		}
	}
	return out, nil
}

func (g Git) lines(args ...string) ([]string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = g.Root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	var res []string
	for _, s := range strings.Split(string(out), "\x00") {
		if s != "" {
			res = append(res, s)
		}
	}
	return res, nil
}

// catFiles reads many blobs of a revision through one git process.
func (g Git) catFiles(rev string, names []string) (fstest.MapFS, error) {
	cmd := exec.Command("git", "cat-file", "--batch")
	cmd.Dir = g.Root
	var in bytes.Buffer
	for _, n := range names {
		fmt.Fprintf(&in, "%s:%s\n", rev, n)
	}
	cmd.Stdin = &in
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git cat-file: %w", err)
	}
	r := bufio.NewReader(bytes.NewReader(out))
	fsys := fstest.MapFS{}
	for _, n := range names {
		header, err := r.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("git cat-file: reading %s: %w", n, err)
		}
		parts := strings.Fields(header)
		if len(parts) != 3 || parts[1] != "blob" {
			continue // submodules and other non-blobs are not files of this repository
		}
		size, err := strconv.Atoi(parts[2])
		if err != nil {
			return nil, fmt.Errorf("git cat-file: bad header %q", header)
		}
		data := make([]byte, size+1) // content plus the trailing newline git adds
		if _, err := io.ReadFull(r, data); err != nil {
			return nil, fmt.Errorf("git cat-file: reading %s: %w", n, err)
		}
		fsys[n] = &fstest.MapFile{Data: data[:size]}
	}
	return fsys, nil
}
