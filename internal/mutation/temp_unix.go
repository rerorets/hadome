// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

//go:build !windows

package mutation

import (
	"io/fs"
	"os"
	"syscall"
)

// ownedByUser reports whether the user running hadome owns fi.
func ownedByUser(fi fs.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == os.Getuid()
}
