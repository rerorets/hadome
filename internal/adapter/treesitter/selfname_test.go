// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

//go:build cgo

package treesitter

import (
	"fmt"
	"testing"
)

// viewpoint: behaviour
// target: python.trace
// The object itself (self) is not read as a name, even when the module declares a value of that
// name: a method calling self.get() calls its class's get, and reaches nothing that value is made
// from.
func TestTheObjectItselfIsNotAValueOfTheSameName(t *testing.T) {
	a := analyze(t, "python", tree(
		"app/__init__.py", "",
		"app/make.py", "def build():\n    return 1\n",
		"app/repo.py", "from app.make import build\n\nself = build()\n\n\nclass Repo:\n    def get(self):\n        return 1\n\n"+
			"    def find(self):\n        return self.get()\n",
	))
	for _, s := range a.Symbols {
		if s.ID == "app/repo.Repo.find" {
			if got := fmt.Sprint(s.Calls); got != "[app/repo.Repo.get]" {
				t.Errorf("Repo.find calls %s, want [app/repo.Repo.get]", got)
			}
			return
		}
	}
	t.Fatal("no symbol app/repo.Repo.find")
}
