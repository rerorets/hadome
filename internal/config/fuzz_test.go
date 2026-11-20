// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

package config

import (
	"errors"
	"strings"
	"testing"
)

// viewpoint: input
// target: config.Load
// Any bytes as .hadome.yml either load or fail with validation errors, and every error in the file
// names a line within it.
func FuzzLoad(f *testing.F) {
	f.Add(full)
	f.Add("schema: 1\n")
	f.Add("schema: 1\nrules:\n  size:\n    file-lines: -3\n")
	f.Add("schema: [1\n")
	f.Add("a: 1\n---\nb: 2\n")
	f.Fuzz(func(t *testing.T, yml string) {
		// extends reads other files from disk, which a fuzzed name could point anywhere; without it
		// the working directory is never read.
		if strings.Contains(yml, "extends") {
			return
		}
		_, err := Load(product(yml), "")
		if err == nil {
			return
		}
		var es Errors
		if !errors.As(err, &es) {
			t.Fatalf("failed without validation errors: %v", err)
		}
		// YAML breaks lines at \n, \r\n and a lone \r.
		breaks := strings.ReplaceAll(strings.ReplaceAll(yml, "\r\n", "\n"), "\r", "\n")
		last := strings.Count(breaks, "\n") + 1
		for _, e := range es {
			if e.File == FileName && (e.Line < 1 || e.Line > last) {
				t.Errorf("%s: %s at line %d, outside the %d lines of the file", e.Key, e.Problem, e.Line, last)
			}
		}
	})
}
