// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

package facts

// File is one file of a version.
type File struct {
	Path     string
	Language string
	Role     Role
	Lines    int
	// Unit is what a dependency diagram draws a box for, as imports name it: in Go, the package's
	// import path. Empty for files that belong to no unit.
	Unit string
	// UnitPath is where the unit is in the repository, the name a dependency diagram uses: a
	// directory, or for Rust a module's path such as src/store.
	UnitPath string
	// Unread is true for a file of a supported language that could not be read: it does not parse,
	// or this build of hadome has no reader for its language.
	Unread bool
	// Build is the condition a Go file is built under: its //go:build expression and its _GOOS and
	// _GOARCH name, joined by &&. Empty for a file without one and for other languages.
	Build string
	// Calls, Through, Asserted and CallAt are those of Symbol for the code of a product file outside
	// the bodies of its functions and methods: package variables, class bodies, top-level statements.
	Calls    []string
	Through  []string
	Asserted []string
	CallAt   map[string]int
}
