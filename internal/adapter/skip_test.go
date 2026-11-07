package adapter_test

import (
	"slices"
	"testing"

	"github.com/rerorets/hadome/internal/adapter/walk"
	"github.com/rerorets/hadome/internal/rules/comments"
)

// viewpoint: consistency
// target: walk.SkippedDirs, comments.SkippedDirs
// comments/broken-reference looks for a file named only by its name in the files the adapters
// read. Rules may not import the adapters, so the rule keeps its own copy of the directories they
// skip; a directory added to one list and not the other would let a reference find a file hadome
// never reads, or miss one it does.
func TestBrokenReferenceSkipsTheDirectoriesTheAdaptersSkip(t *testing.T) {
	adapters, rule := walk.SkippedDirs(), comments.SkippedDirs()
	slices.Sort(adapters)
	slices.Sort(rule)
	if !slices.Equal(adapters, rule) {
		t.Errorf("the adapters skip %v, comments/broken-reference skips %v", adapters, rule)
	}
	for _, dir := range adapters {
		if !walk.Skip(dir) {
			t.Errorf("walk.Skip(%q) = false for a directory walk.SkippedDirs lists", dir)
		}
	}
}
