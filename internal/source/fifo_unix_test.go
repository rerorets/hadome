// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

//go:build unix

package source_test

import (
	"errors"
	"io/fs"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/rerorets/hadome/internal/source"
)

// viewpoint: input and output
// target: source.Dirs
// A FIFO is not a file of the version: it is not listed, and opening it by name finds nothing
// instead of waiting for a writer that never comes.
func TestDirsLeaveOutFIFOs(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.txt", "a\n")
	if err := syscall.Mkfifo(filepath.Join(dir, "pipe"), 0o644); err != nil {
		t.Fatal(err)
	}

	head, err := source.Dirs{BaseDir: dir, HeadDir: dir}.Head()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := fs.ReadDir(head.FS, ".")
	if err != nil || len(entries) != 1 || entries[0].Name() != "a.txt" {
		t.Errorf("want only a.txt listed, got %v, %v", entries, err)
	}
	if _, err := fs.ReadFile(head.FS, "pipe"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("want the FIFO not to exist, got %v", err)
	}
}
