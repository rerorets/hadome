package facts_test

import (
	"testing"

	"github.com/rerorets/hadome/internal/facts"
)

// viewpoint: behaviour
// target: facts.CountLines
// A file has as many lines as an editor shows: the newline that ends the last line starts no new
// one, so file-lines does not count one line more than the file has.
func TestCountLinesCountsWhatAnEditorShows(t *testing.T) {
	for _, tc := range []struct {
		src  string
		want int
	}{
		{"", 0},
		{"a", 1},
		{"a\n", 1},
		{"a\nb", 2},
		{"a\nb\n", 2},
		{"a\n\n", 2},
		{"\n", 1},
	} {
		if got := facts.CountLines([]byte(tc.src)); got != tc.want {
			t.Errorf("CountLines(%q) = %d, want %d", tc.src, got, tc.want)
		}
	}
}
