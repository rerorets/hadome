// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

package source_test

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rerorets/hadome/internal/source"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// viewpoint: input and output
// target: source.Git
// Integration: only a real git repository shows what ls-tree, cat-file and rename detection return.
func TestGitSourceReadsBaseHeadAndRenames(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	write(t, dir, "a/old.go", "package a\n\n// a long enough body for rename detection to recognise the file\nfunc F() {}\n")
	write(t, dir, "keep.txt", "keep\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "base")
	git(t, dir, "mv", "a/old.go", "a/new.go")
	write(t, dir, "untracked.txt", "new\n")
	write(t, dir, ".gitignore", "ignored.txt\n")
	write(t, dir, "ignored.txt", "x\n")

	src := source.Git{Root: dir, BaseRev: "main"}
	base, err := src.Base()
	if err != nil {
		t.Fatal(err)
	}
	if b, err := fs.ReadFile(base.FS, "a/old.go"); err != nil || len(b) == 0 {
		t.Fatalf("base must contain a/old.go with its content, err %v", err)
	}
	head, err := src.Head()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"a/new.go", "keep.txt", "untracked.txt"} {
		if _, err := fs.Stat(head.FS, want); err != nil {
			t.Errorf("head must contain %s: %v", want, err)
		}
	}
	if _, err := fs.Stat(head.FS, "ignored.txt"); err == nil {
		t.Error("head must not contain ignored files")
	}
	renames, err := src.Renames()
	if err != nil {
		t.Fatal(err)
	}
	if renames["a/old.go"] != "a/new.go" {
		t.Fatalf("want rename a/old.go -> a/new.go, got %v", renames)
	}
}

// viewpoint: input and output
// target: source.Git
// With HeadRev, head is that revision and not the working tree: edits and untracked files are not
// in it, and renames are those between the two revisions.
func TestGitSourceComparesTwoRevisions(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	write(t, dir, "a/old.go", "package a\n\n// a long enough body for rename detection to recognise the file\nfunc F() {}\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "base")
	git(t, dir, "branch", "v1")
	git(t, dir, "mv", "a/old.go", "a/new.go")
	git(t, dir, "commit", "-q", "-m", "rename")
	git(t, dir, "branch", "v2")
	write(t, dir, "a/new.go", "package a\n\nfunc Edited() {}\n")
	write(t, dir, "untracked.txt", "new\n")

	src := source.Git{Root: dir, BaseRev: "v1", HeadRev: "v2"}
	head, err := src.Head()
	if err != nil {
		t.Fatal(err)
	}
	if head.Label != "v2" {
		t.Errorf("head is labelled by its revision, got %q", head.Label)
	}
	b, err := fs.ReadFile(head.FS, "a/new.go")
	if err != nil || strings.Contains(string(b), "Edited") {
		t.Errorf("head must hold a/new.go as committed in v2, not as edited: %q, %v", b, err)
	}
	if _, err := fs.Stat(head.FS, "untracked.txt"); err == nil {
		t.Error("head must not contain files outside the revision")
	}
	renames, err := src.Renames()
	if err != nil {
		t.Fatal(err)
	}
	if renames["a/old.go"] != "a/new.go" {
		t.Fatalf("want rename a/old.go -> a/new.go between the revisions, got %v", renames)
	}
}

// viewpoint: input and output
// target: source.Git
// A submodule is neither a file of base nor of the working tree, whether or not it is checked out,
// and reading either version does not fail on it.
func TestGitSourceLeavesSubmodulesOut(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	write(t, dir, "keep.txt", "keep\n")
	git(t, dir, "add", ".")
	git(t, dir, "update-index", "--add", "--cacheinfo", "160000,1111111111111111111111111111111111111111,vendor/lib")
	git(t, dir, "commit", "-q", "-m", "base")
	if err := os.MkdirAll(filepath.Join(dir, "vendor", "lib"), 0o755); err != nil {
		t.Fatal(err)
	}

	src := source.Git{Root: dir, BaseRev: "main"}
	base, err := src.Base()
	if err != nil {
		t.Fatalf("base: %v", err)
	}
	head, err := src.Head()
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	for label, fsys := range map[string]fs.FS{"base": base.FS, "head": head.FS} {
		if _, err := fs.Stat(fsys, "keep.txt"); err != nil {
			t.Errorf("%s must contain keep.txt: %v", label, err)
		}
		if _, err := fs.Stat(fsys, "vendor/lib"); err == nil {
			t.Errorf("%s must not contain the submodule vendor/lib", label)
		}
	}
}
