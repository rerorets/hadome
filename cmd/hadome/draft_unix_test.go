//go:build !windows

package main

import (
	"os"
	"strings"
	"testing"
)

// viewpoint: failure
// target: hadome suggest diagram
// A tree that cannot be read whole stops the draft with exit 2 and says why: a draft of part of the
// tree would leave units out of every box.
func TestADraftStopsOnATreeThatCannotBeRead(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root reads a directory it has no permission for, so nothing cannot be read")
	}
	dir := writeTree(t, map[string]string{"go.mod": "module example.com/app\n", "a/a.go": "package a\n", "b/b.go": "package b\n"})
	t.Chdir(dir)
	if err := os.Chmod("b", 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir+"/b", 0o755) })
	if code, out := runIn(t, "suggest", "diagram"); code != 2 || !strings.Contains(out, "hadome:") || strings.Contains(out, "mermaid") {
		t.Errorf("want exit 2 saying why and no draft, got %d: %s", code, out)
	}
}
