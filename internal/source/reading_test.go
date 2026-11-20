// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

package source_test

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rerorets/hadome/internal/source"
)

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("this system cannot make symbolic links: %v", err)
	}
}

// viewpoint: input and output
// target: source.Git
// Integration: a file name holding a newline followed by another blob's ID is what made cat-file
// answer twice for one file; every file of the revision must still hold its own content.
func TestGitRevisionReadsEveryFileAsCommittedWhateverItsName(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	write(t, dir, "c.py", "def f():\n    return 1\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "base")
	other := filepath.Join(dir, "other.txt")
	if err := os.WriteFile(other, []byte("decoy\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	id := gitOut(t, dir, "hash-object", "-w", "other.txt")
	if err := os.Remove(other); err != nil {
		t.Fatal(err)
	}
	odd := "b\n" + id
	write(t, dir, odd, "odd\n")
	write(t, dir, "c.py", "def f():\n    return 2\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "head")

	head, err := source.Git{Root: dir, BaseRev: "main~1", HeadRev: "main"}.Head()
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"c.py": "def f():\n    return 2\n", odd: "odd\n"} {
		if got, err := fs.ReadFile(head.FS, name); err != nil || string(got) != want {
			t.Errorf("%q must hold %q, got %q, %v", name, want, got, err)
		}
	}
}

// viewpoint: errors
// target: source.Git
// Integration: a revision that lists a file whose content git no longer has cannot be read whole,
// so reading it fails instead of leaving the file out.
func TestGitRevisionWithAMissingBlobFails(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	write(t, dir, "a.txt", "only here\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "base")
	id := gitOut(t, dir, "rev-parse", "main:a.txt")
	if err := os.Remove(filepath.Join(dir, ".git", "objects", id[:2], id[2:])); err != nil {
		t.Fatal(err)
	}

	if _, err := (source.Git{Root: dir, BaseRev: "main"}).Base(); err == nil || !strings.Contains(err.Error(), id) {
		t.Fatalf("want an error naming the missing object %s, got %v", id, err)
	}
}

// viewpoint: errors
// target: source.Git
// A revision that looks like an option is a revision to git, never an option: --output would
// otherwise make git diff write a file.
func TestGitRevisionIsNeverAnOption(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	write(t, dir, "a.txt", "a\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "base")
	written := filepath.Join(t.TempDir(), "written")
	src := source.Git{Root: dir, BaseRev: "--output=" + written}

	if _, err := src.Renames(); err == nil {
		t.Error("renames from a revision that does not exist must fail")
	}
	if _, err := src.Base(); err == nil {
		t.Error("reading a revision that does not exist must fail")
	}
	if _, err := os.Stat(written); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("git must not have written %s: %v", written, err)
	}
}

// viewpoint: input and output
// target: source.Git
// Integration: in the working tree, as in a revision, a symbolic link is a file holding its target,
// so a link to a file outside the repository never shows that file.
func TestGitWorkingTreeReadsLinksAsTheirTarget(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	write(t, dir, "keep.txt", "keep\n")
	symlink(t, outside, filepath.Join(dir, ".hadome.yml"))
	symlink(t, os.DevNull, filepath.Join(dir, "z.py"))
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "base")

	src := source.Git{Root: dir, BaseRev: "main"}
	base, err := src.Base()
	if err != nil {
		t.Fatal(err)
	}
	head, err := src.Head()
	if err != nil {
		t.Fatal(err)
	}
	for label, fsys := range map[string]fs.FS{"base": base.FS, "working tree": head.FS} {
		for name, want := range map[string]string{".hadome.yml": outside, "z.py": os.DevNull} {
			if got, err := fs.ReadFile(fsys, name); err != nil || string(got) != want {
				t.Errorf("%s: %s must hold the link's target %q, got %q, %v", label, name, want, got, err)
			}
		}
	}
}

// viewpoint: input and output
// target: source.Dirs
// A directory is read as git would store it: every symbolic link, whatever it points at or whether
// it points anywhere, is a file holding its target, and a path through a link does not exist.
func TestDirsReadLinksAsTheirTarget(t *testing.T) {
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	write(t, dir, "a.txt", "a\n")
	write(t, dir, "real/b.txt", "b\n")
	links := map[string]string{"to-dir": "real", "broken": "missing", "to-file": "a.txt",
		"out": outside, ".hadome.yml": filepath.Join(outside, "secret")}
	for name, target := range links {
		symlink(t, target, filepath.Join(dir, name))
	}

	head, err := source.Dirs{BaseDir: t.TempDir(), HeadDir: dir}.Head()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	err = fs.WalkDir(head.FS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(head.FS, p)
		got[p] = string(b)
		return err
	})
	if err != nil {
		t.Fatalf("reading every file: %v", err)
	}
	want := map[string]string{"a.txt": "a\n", "real/b.txt": "b\n"}
	for name, target := range links {
		want[name] = target
	}
	if len(got) != len(want) {
		t.Errorf("got files %v, want %v", got, want)
	}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s: got %q, want %q", name, got[name], w)
		}
	}
	entries, err := fs.ReadDir(head.FS, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		info, err := e.Info()
		if target, ok := links[e.Name()]; ok && (err != nil || e.IsDir() || info.Size() != int64(len(target))) {
			t.Errorf("%s is listed as a file of %d bytes, got dir %v, %v, %v", e.Name(), len(target), e.IsDir(), info, err)
		}
	}
	if _, err := fs.ReadFile(head.FS, "real"); err == nil {
		t.Error("reading a directory as a file must fail")
	}
	if _, err := fs.ReadDir(head.FS, "a.txt"); err == nil {
		t.Error("listing a file as a directory must fail")
	}
	if _, err := fs.ReadDir(head.FS, "to-dir"); err == nil {
		t.Error("listing a link to a directory must fail: it is a file")
	}
	if _, err := fs.ReadDir(head.FS, "nowhere"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("listing a missing directory must say it does not exist, got %v", err)
	}
	for _, through := range []string{"out/secret", "to-dir/b.txt"} {
		if _, err := fs.ReadFile(head.FS, through); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s goes through a link and must not exist, got %v", through, err)
		}
	}
}

// viewpoint: boundaries
// target: source.Dirs, source.Git
// A file over the limit stops reading with an error naming it, in a directory and in the working
// tree, rather than being read whole; a file at the limit is read.
func TestReadingAFileOverTheLimitFails(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	write(t, dir, "keep.txt", "keep\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "base")
	sized := func(name string, size int64) {
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := f.Truncate(size); err != nil {
			t.Fatal(err)
		}
	}
	sized("at.bin", source.MaxFileSize)

	head, err := source.Git{Root: dir, BaseRev: "main"}.Head()
	if err != nil {
		t.Fatalf("a file at the limit must be read: %v", err)
	}
	if info, err := fs.Stat(head.FS, "at.bin"); err != nil || info.Size() != source.MaxFileSize {
		t.Fatalf("at.bin must be read whole: %v", err)
	}

	sized("big.bin", source.MaxFileSize+1)
	if _, err := (source.Git{Root: dir, BaseRev: "main"}).Head(); err == nil || !strings.Contains(err.Error(), "big.bin") {
		t.Errorf("working tree: want an error naming big.bin, got %v", err)
	}
	dirs, _ := source.Dirs{BaseDir: dir, HeadDir: dir}.Head()
	if _, err := fs.ReadFile(dirs.FS, "big.bin"); err == nil || !strings.Contains(err.Error(), "big.bin") {
		t.Errorf("directory: want an error naming big.bin, got %v", err)
	}
}
