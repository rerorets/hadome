// Package walk lists the files of a version that hadome reads, skipping what is never the
// product's own code.
package walk

import (
	"io/fs"
	"regexp"
	"slices"
	"strings"
)

// skipped are the directories skipped besides hidden ones.
var skipped = []string{"vendor", "testdata", "node_modules", "target", "build", "dist", "__pycache__"}

// SkippedDirs lists the directories Skip skips besides those starting with a dot. The
// comments/broken-reference rule keeps a copy, since rules may not import adapters.
func SkippedDirs() []string { return slices.Clone(skipped) }

// Skip reports whether a directory is skipped: vendored and installed dependencies, test fixtures,
// build outputs, and hidden directories, which tools use for their own files. A leading underscore
// does not skip a directory: frameworks keep the product's own code and tests there (__tests__,
// _components, _internal).
func Skip(name string) bool {
	return slices.Contains(skipped, name) || strings.HasPrefix(name, ".")
}

var (
	helperDir  = regexp.MustCompile(`(^|/)(__mocks__|__fixtures__|testutils?|test-utils|test_utils|testhelpers?)/`)
	supportDir = regexp.MustCompile(`(^|/)(test|tests|e2e)/`)
)

// HelperDir reports whether a file path lies under a directory whose name says it holds helpers for
// the tests (testutil/, testhelpers/, __mocks__/ and the like). No language keeps product code
// under such a name.
func HelperDir(p string) bool { return helperDir.MatchString(p) }

// SupportDir reports whether a file path lies under a directory that holds code that exists for the
// tests in TypeScript and Python: a HelperDir, or test/, tests/ or e2e/. In Go a directory names its
// package, and a package called tests can be product code, so Go uses HelperDir only.
func SupportDir(p string) bool { return HelperDir(p) || supportDir.MatchString(p) }

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
