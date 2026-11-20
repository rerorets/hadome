// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

// Package source provides the two versions the engine compares.
//
// Design: docs/design/engine.md (Sources)
package source

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing/fstest"
	"time"

	"github.com/rerorets/hadome/internal/engine"
)

// MaxFileSize is the most hadome reads of one file. A larger file stops the check, since a version
// read without it could not be judged.
const MaxFileSize = 128 << 20

// Dirs compares two directories. It never reports renames.
type Dirs struct{ BaseDir, HeadDir string }

// Base returns the base directory.
func (d Dirs) Base() (engine.Version, error) {
	return engine.Version{Label: "base", FS: Tree(d.BaseDir), Root: d.BaseDir}, nil
}

// Head returns the head directory.
func (d Dirs) Head() (engine.Version, error) {
	return engine.Version{Label: "head", FS: Tree(d.HeadDir), Root: d.HeadDir}, nil
}

// Renames is always empty for directories.
func (d Dirs) Renames() (map[string]string, error) { return nil, nil }

// Tree is the directory at root as Git would store it: a symbolic link is a file whose content is
// the link's target, never the file it leads to, and a path through a link does not exist. Only
// regular files, links and directories are in it. Reading a file over MaxFileSize is an error.
func Tree(root string) fs.FS { return dirFS{root} }

type dirFS struct{ root string }

func (d dirFS) Open(name string) (fs.File, error) {
	data, k, err := d.read("open", name)
	switch {
	case err != nil:
		return nil, err
	case k == isDir:
		return os.Open(d.native(name))
	}
	return fstest.MapFS{name: &fstest.MapFile{Data: data, Mode: 0o444}}.Open(name)
}

func (d dirFS) ReadFile(name string) ([]byte, error) {
	data, k, err := d.read("read", name)
	if err == nil && k == isDir {
		err = &fs.PathError{Op: "read", Path: name, Err: errors.New("is a directory")}
	}
	return data, err
}

func (d dirFS) ReadDir(name string) ([]fs.DirEntry, error) {
	_, k, err := d.read("readdir", name)
	if err == nil && k != isDir {
		err = &fs.PathError{Op: "readdir", Path: name, Err: errors.New("not a directory")}
	}
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(d.native(name))
	if err != nil {
		return nil, err
	}
	out := entries[:0]
	for _, e := range entries {
		switch t := e.Type(); {
		case t.IsDir(), t.IsRegular():
			out = append(out, e)
		case t&fs.ModeSymlink != 0:
			target, err := os.Readlink(filepath.Join(d.native(name), e.Name()))
			if err != nil {
				return nil, err
			}
			out = append(out, linkEntry{name: e.Name(), size: int64(len(target))})
		}
	}
	return out, nil
}

func (d dirFS) native(name string) string { return filepath.Join(d.root, filepath.FromSlash(name)) }

func (d dirFS) read(op, name string) ([]byte, kind, error) {
	if !fs.ValidPath(name) {
		return nil, missing, &fs.PathError{Op: op, Path: name, Err: fs.ErrInvalid}
	}
	data, k, err := readNoFollow(d.root, name)
	if err == nil && k == missing {
		err = &fs.PathError{Op: op, Path: name, Err: fs.ErrNotExist}
	}
	return data, k, err
}

// linkEntry lists a symbolic link as the file Tree reads it as.
type linkEntry struct {
	name string
	size int64
}

func (e linkEntry) Name() string               { return e.name }
func (e linkEntry) IsDir() bool                { return false }
func (e linkEntry) Type() fs.FileMode          { return 0 }
func (e linkEntry) Info() (fs.FileInfo, error) { return e, nil }
func (e linkEntry) Size() int64                { return e.size }
func (e linkEntry) Mode() fs.FileMode          { return 0o444 }
func (e linkEntry) ModTime() time.Time         { return time.Time{} }
func (e linkEntry) Sys() any                   { return nil }

type kind int

const (
	missing kind = iota // absent, beyond a link, or neither a file, a link nor a directory
	isFile
	isDir
)

// readNoFollow reads name, a slash-separated path under root, without following any symbolic link:
// a link is its target text, and a path through a link does not exist. A FIFO or a device is
// missing, so reading one never blocks or runs without end.
func readNoFollow(root, name string) ([]byte, kind, error) {
	if name == "." {
		return nil, isDir, nil
	}
	p := root
	parts := strings.Split(name, "/")
	for i, part := range parts {
		p = filepath.Join(p, part)
		info, err := os.Lstat(p)
		if errors.Is(err, fs.ErrNotExist) {
			return nil, missing, nil
		}
		if err != nil {
			return nil, missing, err
		}
		if i == len(parts)-1 {
			return readEntry(name, p, info)
		}
		if !info.IsDir() {
			return nil, missing, nil
		}
	}
	return nil, missing, nil
}

func readEntry(name, p string, info fs.FileInfo) ([]byte, kind, error) {
	switch m := info.Mode(); {
	case m.IsDir():
		return nil, isDir, nil
	case m&fs.ModeSymlink != 0:
		target, err := os.Readlink(p)
		return []byte(target), isFile, err
	case !m.IsRegular():
		return nil, missing, nil
	case info.Size() > MaxFileSize:
		return nil, missing, tooLarge(name)
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, missing, err
	}
	defer f.Close()
	if now, err := f.Stat(); err != nil || !os.SameFile(info, now) {
		return nil, missing, fmt.Errorf("%s changed while it was read", name)
	}
	// The size checked above bounds what is read, even if the file grows meanwhile.
	data, err := io.ReadAll(io.LimitReader(f, info.Size()))
	return data, isFile, err
}

func tooLarge(name string) error {
	return fmt.Errorf("%s is larger than %d MiB, the most hadome reads of one file", name, MaxFileSize>>20)
}

// Git compares two versions of the repository at Root: the revision BaseRev, and the revision
// HeadRev or, when HeadRev is empty, the working tree. The working tree holds tracked files and
// untracked files that are not ignored. In every version a symbolic link is a file holding its
// target, as Git stores it.
type Git struct{ Root, BaseRev, HeadRev string }

// Base reads every file of the base revision.
func (g Git) Base() (engine.Version, error) { return g.revision(g.BaseRev) }

// blob is one file of a revision: its object ID and its path.
type blob struct{ id, path string }

func (g Git) revision(rev string) (engine.Version, error) {
	records, err := g.lines("ls-tree", "-r", "-z", "--end-of-options", rev)
	if err != nil {
		return engine.Version{}, err
	}
	var blobs []blob
	for _, r := range records {
		meta, p, ok := strings.Cut(r, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 3 {
			return engine.Version{}, fmt.Errorf("git ls-tree: unexpected record %q", r)
		}
		if fields[1] != "blob" {
			continue // submodules are not files of this repository
		}
		blobs = append(blobs, blob{id: fields[2], path: p})
	}
	fsys, err := g.catFiles(blobs)
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
		data, k, err := readNoFollow(g.Root, n)
		if err != nil {
			return engine.Version{}, err
		}
		// Missing is a file tracked but deleted in the working tree. A directory is a submodule,
		// listed as one path, and not a file of this repository.
		if k == isFile {
			fsys[n] = &fstest.MapFile{Data: data}
		}
	}
	return engine.Version{Label: "working tree", FS: fsys}, nil
}

// Renames maps base paths to head paths for files git detects as renamed.
func (g Git) Renames() (map[string]string, error) {
	args := []string{"diff", "-z", "-M", "--name-status", "--no-color", "--end-of-options", g.BaseRev}
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

// catFiles reads many blobs through one git process. It sends object IDs only, never paths: a path
// may hold a newline, which would split one request in two and shift every answer after it. Each
// answer must name the object asked for, so a shifted or missing answer stops the check.
func (g Git) catFiles(blobs []blob) (fstest.MapFS, error) {
	cmd := exec.Command("git", "cat-file", "--batch")
	cmd.Dir = g.Root
	var in bytes.Buffer
	for _, b := range blobs {
		in.WriteString(b.id + "\n")
	}
	cmd.Stdin = &in
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("git cat-file: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("git cat-file: %w", err)
	}
	fsys, readErr := readBatch(bufio.NewReader(stdout), blobs)
	if readErr != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, fmt.Errorf("git cat-file: %w", readErr)
	}
	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("git cat-file: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return fsys, nil
}

// readBatch reads cat-file's answers, one per blob in order, without reading more than MaxFileSize
// of any one.
func readBatch(r *bufio.Reader, blobs []blob) (fstest.MapFS, error) {
	fsys := fstest.MapFS{}
	for _, b := range blobs {
		header, err := r.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("no answer for %s: %w", b.path, err)
		}
		parts := strings.Fields(header)
		if len(parts) != 3 || parts[0] != b.id || parts[1] != "blob" {
			return nil, fmt.Errorf("asked for %s (%s), got %q", b.id, b.path, strings.TrimSpace(header))
		}
		size, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil || size < 0 {
			return nil, fmt.Errorf("bad header %q", header)
		}
		if size > MaxFileSize {
			return nil, tooLarge(b.path)
		}
		data := make([]byte, size+1) // content plus the trailing newline git adds
		if _, err := io.ReadFull(r, data); err != nil {
			return nil, fmt.Errorf("reading %s: %w", b.path, err)
		}
		fsys[b.path] = &fstest.MapFile{Data: data[:size]}
	}
	return fsys, nil
}
