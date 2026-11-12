package engine

import (
	"testing"
	"testing/fstest"
)

// viewpoint: behaviour
// target: engine.detectRenames
// A head file takes part in at most one rename: not in one by content when the source already
// reported a rename to it, and not in two by content when two removed files held what it holds.
func TestAHeadFileIsTheTargetOfOneRename(t *testing.T) {
	t.Parallel()
	same := []byte("rows\n")
	t.Run("reported", func(t *testing.T) {
		base := fstest.MapFS{"old.txt": {Data: []byte("a\n")}, "gone.txt": {Data: same}}
		head := fstest.MapFS{"new.txt": {Data: same}}
		got, err := detectRenames(newSummedFS(base), newSummedFS(head), map[string]string{"old.txt": "new.txt"})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got["old.txt"] != "new.txt" {
			t.Errorf("renames %v, want only the reported old.txt → new.txt", got)
		}
	})
	t.Run("two removed files", func(t *testing.T) {
		base := fstest.MapFS{"a.txt": {Data: same}, "b.txt": {Data: same}}
		head := fstest.MapFS{"c.txt": {Data: same}}
		got, err := detectRenames(newSummedFS(base), newSummedFS(head), nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || (got["a.txt"] != "c.txt" && got["b.txt"] != "c.txt") {
			t.Errorf("renames %v, want one of a.txt and b.txt renamed to c.txt", got)
		}
	})
}
