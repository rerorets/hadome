// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

//go:build cgo

package treesitter

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/facts"
)

// callLines writes, for a symbol's calls in Calls and Through, the last part of each ID and the line
// CallAt gives it: "find:6 save:7".
func callLinesOf(t *testing.T, at map[string]int, ids ...[]string) string {
	t.Helper()
	var out []string
	for _, list := range ids {
		for _, id := range list {
			out = append(out, fmt.Sprintf("%s:%d", id[strings.LastIndexAny(id, "./")+1:], at[id]))
		}
	}
	sort.Strings(out)
	return strings.Join(out, " ")
}

func symbolCallLines(t *testing.T, a *facts.Analysis, id string) string {
	t.Helper()
	for _, s := range a.Symbols {
		if s.ID == id {
			return callLinesOf(t, s.CallAt, s.Calls, s.Through)
		}
	}
	t.Fatalf("no symbol %s", id)
	return ""
}

func symbolIDEndingIn(t *testing.T, a *facts.Analysis, suffix string) string {
	t.Helper()
	for _, s := range a.Symbols {
		if strings.HasSuffix(s.ID, suffix) {
			return s.ID
		}
	}
	var ids []string
	for _, s := range a.Symbols {
		ids = append(ids, s.ID)
	}
	t.Fatalf("no symbol ending in %s among %v", suffix, ids)
	return ""
}

// viewpoint: behaviour
// target: treesitter.trace
// Each call in Calls and Through has the line of its first call in the body, whichever way it is
// read: a direct call, a method on a typed value, an object made in place. A call made by the value
// of a module-level constant counts where the constant is used.
func TestCallsCarryTheLineOfTheirFirstCall(t *testing.T) {
	cases := []struct {
		lang string
		tree fstest.MapFS
		want map[string]string // symbol ID suffix -> call lines
	}{
		{"typescript", fstest.MapFS{
			"src/repo.ts": {Data: []byte("export class Repo {\n  find() { return 1; }\n}\nexport function save() { return 1; }\n")},
			"src/api.ts": {Data: []byte("import { Repo, save } from \"./repo\";\nconst ROOT = save();\n\nexport function show(r: Repo) {\n" +
				"  const x = 1;\n  r.find();\n  save();\n  save();\n  return x;\n}\nexport function make() {\n  new Repo().find();\n}\n" +
				"\nexport function root() { return ROOT; }\n")},
		}, map[string]string{"src.show": "find:6 save:7", "src.make": "Repo:12 find:12", "src.root": "save:15"}},
		{"python", fstest.MapFS{
			"app/__init__.py": {Data: []byte("")},
			"app/repo.py":     {Data: []byte("class Repo:\n    def find(self):\n        return 1\n\ndef save():\n    pass\n")},
			"app/api.py": {Data: []byte("from app.repo import Repo, save\n\ndef show(r: Repo):\n    x = 1\n    r.find()\n    save()\n" +
				"\ndef make():\n    Repo().find()\n")},
		}, map[string]string{"api.show": "find:5 save:6", "api.make": "Repo:9 find:9"}},
		{"rust", fstest.MapFS{
			"Cargo.toml":   {Data: []byte("[package]\nname = \"app\"\n")},
			"src/lib.rs":   {Data: []byte("pub mod store;\npub mod web;\n")},
			"src/store.rs": {Data: []byte("pub trait Finder { fn find(&self); }\npub fn save() {}\n")},
			"src/web.rs": {Data: []byte("use crate::store::{self, Finder};\n\npub fn show(f: &dyn Finder) {\n    let x = 1;\n" +
				"    f.find();\n    store::save();\n}\n")},
		}, map[string]string{"web.show": "find:5 save:6"}},
		{"java", fstest.MapFS{
			"src/main/java/app/Repo.java": {Data: []byte("package app;\npublic class Repo { public int find() { return 1; } }\n")},
			"src/main/java/app/web/Api.java": {Data: []byte("package app.web;\nimport app.Repo;\npublic class Api {\n" +
				"  public void show(Repo r) {\n    int x = 1;\n    r.find();\n  }\n  public void make() {\n    new Repo().find();\n  }\n}\n")},
		}, map[string]string{"Api.show": "find:6", "Api.make": "Repo:9 find:9"}},
	}
	for _, c := range cases {
		t.Run(c.lang, func(t *testing.T) {
			a := analyze(t, c.lang, c.tree)
			for suffix, want := range c.want {
				if got := symbolCallLines(t, a, symbolIDEndingIn(t, a, suffix)); got != want {
					t.Errorf("%s: got %q, want %q", suffix, got, want)
				}
			}
		})
	}
}

// viewpoint: behaviour
// target: typescript.trace
// The calls of entry-point code that is no symbol carry their lines too.
func TestEntryCodeCallsCarryTheirLines(t *testing.T) {
	a := analyze(t, "typescript", fstest.MapFS{
		"src/repo.ts": {Data: []byte("export function save() { return 1; }\n")},
		"src/server.ts": {Data: []byte("import express from \"express\";\nimport { save } from \"./repo\";\nconst app = express();\n" +
			"app.post(\"/orders\", (req, res) => {\n  const x = 1;\n  save();\n});\n")},
	})
	var got []string
	for _, e := range a.Entries {
		if e.Symbol == "" {
			got = append(got, callLinesOf(t, e.CallAt, e.Calls))
		}
	}
	if strings.Join(got, ",") != "save:6" {
		t.Errorf("got %q, want the inline handler's call of save at line 6", got)
	}
}
