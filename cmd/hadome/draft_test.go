package main

import (
	"os"
	"strings"
	"testing"

	"github.com/rerorets/hadome/internal/config"
)

// viewpoint: external contract
// target: hadome suggest diagram, hadome suggest calls, hadome init
// The drafts are written to the files init's commented rules.deps setting names, and uncommenting
// that setting once gives valid configuration that the drafts satisfy.
func TestInitsDiagramSettingTakesTheDrafts(t *testing.T) {
	t.Chdir(writeTree(t, map[string]string{
		"go.mod":         "module example.com/app\n",
		"api/api.go":     "package api\n\nimport \"example.com/app/store\"\n\nfunc Get() { store.Find() }\n",
		"store/store.go": "package store\n\nfunc Find() {}\n",
		"docs/.keep":     "",
	}))
	if code, out := runIn(t, "init"); code != 0 || !strings.Contains(out, "hadome suggest diagram") {
		t.Fatalf("exit %d, and init must name hadome suggest diagram as a next step: %s", code, out)
	}
	for kind, file := range map[string]string{"diagram": "docs/dependencies.md", "calls": "docs/calls.md"} {
		code, out := runIn(t, "suggest", kind)
		if code != 0 || !strings.Contains(out, "api --> store") {
			t.Fatalf("suggest %s: exit %d, want the arrow api --> store:\n%s", kind, code, out)
		}
		if heading := map[string]string{"diagram": "# Dependencies\n", "calls": "# Calls\n"}[kind]; !strings.HasPrefix(out, heading) {
			t.Errorf("suggest %s: want the draft headed %q, got\n%s", kind, heading, out)
		}
		if err := os.WriteFile(file, []byte(out), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	written, err := os.ReadFile(config.FileName)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(written), "\n")
	for i, l := range lines {
		if l == "# rules:" {
			for j := i; j < len(lines) && strings.HasPrefix(lines[j], "# "); j++ {
				lines[j] = strings.TrimPrefix(lines[j], "# ")
			}
		}
	}
	uncommented := strings.Join(lines, "\n")
	if err := os.WriteFile(config.FileName, []byte(uncommented), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out := runIn(t, "config", "check"); code != 0 || !strings.Contains(uncommented, "\nrules:\n  deps:\n    diagram: docs/dependencies.md\n    calls: docs/calls.md\n") {
		t.Fatalf("the uncommented diagram setting is invalid: %s\n%s", out, uncommented)
	}
}

// viewpoint: failure
// target: hadome suggest diagram
// A depth below 1 groups nothing, so it, an unknown flag and an extra argument are mistakes in the
// command: each exits 2 and says what is wrong.
func TestADraftDepthBelowOneIsAMistake(t *testing.T) {
	t.Chdir(writeTree(t, map[string]string{"go.mod": "module example.com/app\n", "a/a.go": "package a\n"}))
	for _, tc := range []struct {
		args []string
		says string
	}{
		{[]string{"suggest", "diagram", "--depth", "0"}, "--depth must be 1 or more"},
		{[]string{"suggest", "calls", "--nope"}, "flag provided but not defined: -nope"},
		{[]string{"suggest", "calls", "extra"}, ""},
	} {
		if code, out := runIn(t, tc.args...); code != 2 || !strings.Contains(out, tc.says) {
			t.Errorf("%v: want exit 2 saying %q, got %d: %s", tc.args, tc.says, code, out)
		}
	}
	if code, out := runIn(t, "suggest", "diagram", "--depth", "1"); code != 0 || !strings.Contains(out, "a[\"a\"]") {
		t.Errorf("want a box for a at depth 1, got exit %d:\n%s", code, out)
	}
}

// viewpoint: failure
// target: hadome suggest diagram
// A configuration that does not load stops the draft with exit 2 and says why, rather than drawing
// a diagram from settings hadome could not read.
func TestADraftStopsOnABrokenConfiguration(t *testing.T) {
	t.Chdir(writeTree(t, map[string]string{"go.mod": "module example.com/app\n", "a/a.go": "package a\n",
		config.FileName: "schema: 1\nnot-a-key: 1\n"}))
	if code, out := runIn(t, "suggest", "diagram"); code != 2 || !strings.Contains(out, "not-a-key") || strings.Contains(out, "mermaid") {
		t.Errorf("want exit 2 naming the unknown key and no draft, got %d: %s", code, out)
	}
}
