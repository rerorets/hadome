// Package walk lists the files of a version that hadome reads, skipping what is never the
// product's own code.
package walk

import (
	"io/fs"
	"strings"
)

// Skip reports whether a directory is skipped: vendored and installed dependencies, test fixtures,
// build outputs, and hidden directories, which tools use for their own files. A leading underscore
// does not skip a directory: frameworks keep the product's own code and tests there (__tests__,
// _components, _internal).
func Skip(name string) bool {
	return name == "vendor" || name == "testdata" || name == "node_modules" || name == "target" ||
		name == "build" || name == "dist" || name == "__pycache__" || strings.HasPrefix(name, ".")
}

// Files lists every file outside skipped directories, in lexical order.
func Files(fsys fs.FS) ([]string, error) {
	var out []string
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != "." && Skip(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		out = append(out, p)
		return nil
	})
	return out, err
}
