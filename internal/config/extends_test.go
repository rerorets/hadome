// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

package config

import (
	"errors"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// viewpoint: state integrity
// target: config.LoadHead
// Head reads only the shared files base's .hadome.yml extends; one head adds is left out unread
// (here it is not even valid YAML), and the difference in extends is a looser change.
func TestLoadHeadReadsOnlyTheFilesBaseExtends(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "shared.yml"), "schema: 1\ninvariants:\n  - id: shared-1\n    says: Shared\n")
	write(t, filepath.Join(dir, "added.yml"), "schema: [\n")
	base := mustLoad(t, "schema: 1\nextends: [shared.yml]\n", dir)

	head, err := LoadHead(product("schema: 1\nextends: [shared.yml, added.yml]\n"), dir, base)
	if err != nil {
		t.Fatalf("head must load without reading added.yml: %v", err)
	}
	if len(head.Layers) != 2 || len(head.Invariants) != 1 {
		t.Errorf("head must read shared.yml and .hadome.yml only: layers %v, invariants %v", head.Layers, head.Invariants)
	}
	if got := looser(Diff(base, head)); !slices.Contains(got, "extends -[shared.yml]") {
		t.Errorf("want extends listed as looser, got %v", got)
	}
	if _, err := LoadHead(product("schema: 1\nextends: [https://example.com/a.yml]\n"), dir, base); err == nil {
		t.Error("a URL in head's extends is refused even though it is not read")
	}
}

// viewpoint: input and output
// target: config.Load
// An error in a shared layer gives its key and line but never its values, since the file is outside
// the repository; the same error in .hadome.yml shows the value.
func TestErrorsOfASharedLayerShowNoValues(t *testing.T) {
	dir := t.TempDir()
	const secret = "s3cr3t-value"
	for name, text := range map[string]string{
		"allowed.yml": "schema: 1\nlanguages: [" + secret + "]\n",
		"yaml.yml":    "schema: 1\nlanguages: [\"" + secret + "\n",
		"glob.yml":    "schema: 1\nfiles:\n  tests: [\"[" + secret + "\"]\n",
		"nested.yml":  "schema: 1\nextends: [" + secret + "/missing.yml]\n",
	} {
		write(t, filepath.Join(dir, name), text)
		es := loadErrors(t, "schema: 1\nextends: ["+name+"]\n", dir)
		if strings.Contains(es.Error(), secret) {
			t.Errorf("%s: the error shows a value of the shared file:\n%v", name, es)
		}
		if es[0].File != filepath.Join(dir, name) || es[0].Line == 0 {
			t.Errorf("%s: the error must still point at the shared file and line, got %s:%d", name, es[0].File, es[0].Line)
		}
	}
	write(t, filepath.Join(dir, "said.yml"), "schema: 1\nlanguages: ["+secret+"]\nrules:\n  size:\n    params: 0\n")
	if es := loadErrors(t, "schema: 1\nextends: [said.yml]\n", dir); !strings.Contains(es.Error(), "the value is not allowed") ||
		!strings.Contains(es.Error(), "must be at least 1") || strings.Contains(es.Error(), "got 0") {
		t.Errorf("a shared layer's error says what is wrong without the value, got %v", es)
	}
	for yml, want := range map[string]string{
		"schema: 1\nlanguages: [" + secret + "]\n":           secret,
		"schema: 1\nrules:\n  size:\n    params: 0\n":        "got 0",
		"schema: 1\nrules:\n  comments:\n    todo: \"(x\"\n": "missing closing )",
		"schema: 1\nrules:\n  size:\n    params: 0x10\n":     "must be a decimal integer",
		"schema: 1\nlanguages: go\n":                         "must be a list, got a string",
	} {
		if es := loadErrors(t, yml, dir); !strings.Contains(es.Error(), want) {
			t.Errorf("an error in .hadome.yml shows the value %q, got %v", want, es)
		}
	}
}

// viewpoint: boundaries
// target: config.Load
// A shared layer is read only when it is a regular file of at most MaxFileSize, so a directory or a
// device is never read without end; .hadome.yml has the same limit.
func TestConfigurationFilesAreRegularAndBounded(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "sub", "x.yml"), "schema: 1\n")
	write(t, filepath.Join(dir, "at.yml"), "schema: 1\n#"+strings.Repeat("x", MaxFileSize-12)+"\n")
	write(t, filepath.Join(dir, "over.yml"), "schema: 1\n#"+strings.Repeat("x", MaxFileSize-11)+"\n")
	mustLoad(t, "schema: 1\nextends: [at.yml]\n", dir)
	refs := map[string]string{"sub": "not a regular file", "over.yml": "larger than 1 MiB"}
	if runtime.GOOS != "windows" {
		refs["/dev/zero"] = "not a regular file"
	}
	for ref, want := range refs {
		if es := loadErrors(t, "schema: 1\nextends: ["+ref+"]\n", dir); !strings.Contains(es.Error(), want) {
			t.Errorf("%s: want %q, got %v", ref, want, es)
		}
	}
	_, err := Load(product("schema: 1\n#"+strings.Repeat("x", MaxFileSize)+"\n"), dir)
	var es Errors
	if !errors.As(err, &es) || !strings.Contains(es.Error(), "larger than 1 MiB") {
		t.Errorf("an oversized .hadome.yml must be refused, got %v", err)
	}
}
