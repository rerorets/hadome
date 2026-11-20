// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/rerorets/hadome/internal/config"
)

// viewpoint: state integrity
// target: hadome init, hadome agents --write
// Neither command writes through a symbolic link: init refuses a link at .hadome.yml, even one that
// leads nowhere, and agents --write refuses a link it was asked to update. The places the links
// point at stay as they were.
func TestWritingCommandsNeverFollowALink(t *testing.T) {
	outside := t.TempDir()
	t.Chdir(writeTree(t, map[string]string{"go.mod": "module example.com/app\n"}))
	dangling := filepath.Join(outside, "created-by-init")
	if err := os.Symlink(dangling, config.FileName); err != nil {
		t.Fatalf("this system cannot make symbolic links: %v", err)
	}
	if code, out := runIn(t, "init"); code != 2 {
		t.Errorf("init over a dangling link must fail with 2, got %d: %s", code, out)
	}
	if _, err := os.Lstat(dangling); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("init wrote where the link points: %v", err)
	}

	target := filepath.Join(outside, "AGENTS.md")
	if err := os.WriteFile(target, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, "CLAUDE.md"); err != nil {
		t.Fatal(err)
	}
	if code, out := runIn(t, "agents", "--write", "CLAUDE.md"); code != 2 {
		t.Errorf("agents --write to a link must fail with 2, got %d: %s", code, out)
	}
	if data, _ := os.ReadFile(target); string(data) != "mine\n" {
		t.Errorf("agents --write changed the file the link points at: %q", data)
	}
}

// viewpoint: state integrity
// target: replaceFile
// Replacing a file whose path became a link after it was checked replaces the link itself: the
// file it pointed at is untouched.
func TestReplaceFileReplacesALinkInsteadOfFollowingIt(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("target\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("this system cannot make symbolic links: %v", err)
	}
	if err := replaceFile(link, []byte("new\n")); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(target); string(data) != "target\n" {
		t.Errorf("the link's target changed: %q", data)
	}
	if info, err := os.Lstat(link); err != nil || !info.Mode().IsRegular() {
		t.Errorf("link must now be a regular file: %v", err)
	}
}

// viewpoint: state integrity
// target: replaceFile
// A regular file replaced keeps its permissions.
func TestReplaceFileKeepsPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no POSIX permission bits: Go reports every writable file as 0666, so 0600 cannot be kept or observed")
	}
	dir := t.TempDir()
	regular := filepath.Join(dir, "regular")
	if err := os.WriteFile(regular, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := replaceFile(regular, []byte("new\n")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(regular)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("want permissions 0600 kept, got %v", info.Mode().Perm())
	}
}

// viewpoint: errors
// target: createNew, replaceFile
// A write that cannot happen is an error and leaves nothing behind: createNew over an existing
// file keeps it, and replaceFile into a missing directory, or over a directory, fails without
// leaving its temporary file.
func TestWritesThatCannotHappenFailAndLeaveNothing(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "existing")
	if err := os.WriteFile(existing, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := createNew(existing, []byte("new\n")); err == nil {
		t.Error("createNew over an existing file must fail")
	}
	if data, _ := os.ReadFile(existing); string(data) != "mine\n" {
		t.Errorf("createNew changed the existing file: %q", data)
	}
	fresh := filepath.Join(dir, "fresh")
	if err := createNew(fresh, []byte("new\n")); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(fresh); string(data) != "new\n" {
		t.Errorf("createNew wrote %q", data)
	}

	if err := replaceFile(filepath.Join(dir, "missing", "f"), []byte("x")); err == nil {
		t.Error("replaceFile into a missing directory must fail")
	}
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := replaceFile(sub, []byte("x")); err == nil {
		t.Error("replaceFile over a directory must fail")
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".hadome-*")); len(left) != 0 {
		t.Errorf("temporary files left behind: %v", left)
	}
}
