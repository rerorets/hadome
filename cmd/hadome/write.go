// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// createNew writes a file that must not exist yet. O_EXCL refuses any existing path, a symbolic
// link included, even one that leads nowhere, so the write never lands where a link points.
func createNew(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err == nil {
		_, err = f.Write(data)
		err = errors.Join(err, f.Close())
	}
	return err
}

// replaceFile writes data to path through a new file renamed over it. A rename replaces whatever
// is at path, a symbolic link included, and never writes where a link points. The file keeps the
// permissions of the regular file it replaces.
func replaceFile(path string, data []byte) error {
	perm := fs.FileMode(0o644)
	if info, err := os.Lstat(path); err == nil && info.Mode().IsRegular() {
		perm = info.Mode().Perm()
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".hadome-*")
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	err = errors.Join(err, f.Chmod(perm), f.Close())
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		_ = os.Remove(f.Name())
	}
	return err
}
