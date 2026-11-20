// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

package mutation

import "io/fs"

// ownedByUser reports whether the user running hadome owns fi. On Windows the temporary directory
// is under the user's own profile, which other users cannot write into, so whatever is there is.
func ownedByUser(fs.FileInfo) bool {
	return true
}
