// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

//go:build cgo

package code

import (
	"strings"
	"testing"

	"github.com/rerorets/hadome/internal/engine"
)

// viewpoint: behaviour
// target: code.everyLanguage
// In a build that reads every language, code/config-literal and code/swallowed-error have a case in
// each language the design names.
func TestEveryLanguageHasACase(t *testing.T) {
	for _, r := range []engine.Rule{configLiteral{}, swallowedError{}} {
		for _, lang := range []string{"typescript", "python", "java", "kotlin", "rust"} {
			found := false
			for _, c := range r.Cases() {
				found = found || strings.Contains(strings.ToLower(c.Name), lang)
			}
			if !found {
				t.Errorf("%T has no case in %s", r, lang)
			}
		}
	}
}
