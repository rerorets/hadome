// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

package engine_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/adapter"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/rules/tests"
)

// viewpoint: external contract
// target: engine.Engine.Check
// Calls hadome cannot follow are listed only for the units where head counts more than base, and
// never change the exit code.
func TestCheckListsUnitsWhereUnfollowedCallsGrew(t *testing.T) {
	count := func(in engine.Input) map[string]int {
		out := map[string]int{"steady": 4, "shrunk": 1}
		for _, f := range in.Facts.Files {
			if strings.HasPrefix(f.Path, "store/") {
				out["store"]++
			}
			if f.Path == "store/extra.go" {
				out["shrunk"] = 0
			}
		}
		return out
	}
	head := module("")
	head["store/extra.go"] = &fstest.MapFile{Data: []byte("package store\n")}
	e := &engine.Engine{Analyzer: adapter.New(), Rules: tests.All(), Unfollowed: count}
	r, err := e.Check(treeSource{base: module(""), head: head})
	if err != nil {
		t.Fatal(err)
	}
	if r.ExitCode() != engine.ExitOK || len(r.Unfollowed) != 1 || r.Unfollowed[0] != (engine.Unfollowed{Unit: "store", Base: 2, Head: 3}) {
		t.Fatalf("want store 2 → 3 alone, exit 0; got exit %d, %+v", r.ExitCode(), r.Unfollowed)
	}
}

// viewpoint: external contract
// target: engine.WriteJSON, engine.WriteText
// The units where unfollowed calls grew reach the JSON under unfollowed, an empty array when there
// are none, and the text after a sentence saying a type annotation makes them followable.
func TestUnfollowedCallsAreWrittenOut(t *testing.T) {
	r := engine.Report{Unfollowed: []engine.Unfollowed{{Unit: "app/controller", Base: 3, Head: 5}}}
	var out bytes.Buffer
	if err := engine.WriteJSON(&out, r, "en"); err != nil {
		t.Fatal(err)
	}
	var m struct {
		Unfollowed []map[string]any `json:"unfollowed"`
	}
	if err := json.Unmarshal(out.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Unfollowed) != 1 || m.Unfollowed[0]["unit"] != "app/controller" || m.Unfollowed[0]["base"] != 3.0 || m.Unfollowed[0]["head"] != 5.0 {
		t.Errorf("want app/controller 3 → 5, got %v", m.Unfollowed)
	}
	out.Reset()
	if err := engine.WriteJSON(&out, engine.Report{}, "en"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"unfollowed": []`) {
		t.Errorf("want an empty array without growth, got %s", out.String())
	}
	out.Reset()
	engine.WriteText(&out, r, "ja")
	if text := out.String(); !strings.Contains(text, "型の注釈を書くとたどれます") || !strings.Contains(text, "  app/controller: 3 → 5\n") {
		t.Errorf("want the sentence and the unit's line, got %s", text)
	}
}
