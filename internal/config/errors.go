// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

package config

import (
	"fmt"
	"strings"
)

// Error is one problem in a configuration file, written so that an agent can fix it without
// reading the specification.
type Error struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Key     string `json:"key"`
	Problem string `json:"problem"`
	Allowed string `json:"allowed,omitempty"`
	Example string `json:"example,omitempty"`
}

// Error formats the problem with what is allowed and an example on the following lines.
func (e Error) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s:%d: %s: %s", e.File, e.Line, e.Key, e.Problem)
	if e.Allowed != "" {
		b.WriteString("\n  allowed: " + e.Allowed)
	}
	if e.Example != "" {
		b.WriteString("\n  example: " + strings.ReplaceAll(e.Example, "\n", "\n           "))
	}
	return b.String()
}

// Errors are all the problems found in a configuration. A configuration with any error never runs a check.
type Errors []Error

// Error formats every problem, one after another.
func (es Errors) Error() string {
	parts := make([]string, len(es))
	for i, e := range es {
		parts[i] = e.Error()
	}
	return strings.Join(parts, "\n")
}
