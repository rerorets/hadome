//go:build cgo

package suggest_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/adapter"
	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/suggest"
)

// keys returns the candidates of a tree, one per line.
func keys(t *testing.T, tree fstest.MapFS) string {
	t.Helper()
	a, err := adapter.New().Analyze(engine.Version{FS: tree}, config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, cd := range suggest.Candidates(engine.Input{Facts: a, Config: config.Config{}}) {
		out = append(out, cd.Key())
	}
	return strings.Join(out, "\n")
}

// viewpoint: behaviour
// target: suggest.Candidates
// The same words make candidates in camelCase and snake_case, and round trips pair up in the same
// spelling; a word glued to more letters (addressOf, deleted_rows) is not the word.
func TestCandidatesInEverySpelling(t *testing.T) {
	got := keys(t, fstest.MapFS{
		"src/rows/rows.ts": {Data: []byte(`export function deleteRow(id: string) {}
export function addressOf(id: string) { return id }
export function exportRows() {}
export function importRows() {}
`)},
		"app/store.py": {Data: []byte(`def delete_row(row_id):
    pass

def deleted_rows():
    pass

def export_rows():
    pass

def import_rows(rows):
    pass
`)},
	})
	want := strings.Join([]string{
		"erasable rows.deleteRow",
		"erasable store.delete_row",
		"round-trip rows.exportRows,rows.importRows",
		"round-trip store.export_rows,store.import_rows",
	}, "\n")
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// viewpoint: behaviour
// target: suggest.Candidates
// A React component is a part of a screen, not an operation, so DeleteRowDialog is no candidate;
// a camelCase function beside it still is.
func TestComponentsAreNotCandidates(t *testing.T) {
	got := keys(t, fstest.MapFS{
		"src/rows/DeleteRowDialog.tsx": {Data: []byte(`export function DeleteRowDialog() { return <div /> }
export const RemoveButton = () => <button />
export function deleteRows(ids: string[]) {}
`)},
	})
	if want := "erasable rows.deleteRows"; got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}
