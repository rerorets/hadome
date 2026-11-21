// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

package source

import (
	"io/fs"
	"path/filepath"

	"github.com/rerorets/hadome/internal/engine"
)

// Dirs compares two directories. It never reports renames.
type Dirs struct{ BaseDir, HeadDir string }

// Base returns the base directory.
func (d Dirs) Base() (engine.Version, error) { return dirVersion("base", d.BaseDir) }

// Head returns the head directory.
func (d Dirs) Head() (engine.Version, error) { return dirVersion("head", d.HeadDir) }

func dirVersion(label, root string) (engine.Version, error) {
	big, err := tooLargeIn(root)
	return engine.Version{Label: label, FS: Tree(root), Root: root, TooLarge: big}, err
}

// tooLargeIn lists the regular files under root that Tree leaves out for their size, without
// following symbolic links.
func tooLargeIn(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() > MaxFileSize {
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	return out, err
}

// Renames is always empty for directories.
func (d Dirs) Renames() (map[string]string, error) { return nil, nil }
