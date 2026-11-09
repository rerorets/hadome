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

// viewpoint: behaviour
// target: facts.ShortName
// A symbol shows as its unit's last path element and its name; a symbol of a unit at the root of a
// TypeScript project (unit ".") shows as its name alone, not with the dots of the root in front.
func TestShortNameShowsTheLastElementAndTheName(t *testing.T) {
	for id, want := range map[string]string{
		"example.com/app/store.Append": "store.Append",
		"web/src/lib.getSession":       "lib.getSession",
		"..devServer":                  "devServer",
		"vite.config.devServer":        "vite.config.devServer",
		"src":                          "src",
	} {
		if got := facts.ShortName(id); got != want {
			t.Errorf("ShortName(%q) = %q, want %q", id, got, want)
		}
	}
}

// viewpoint: behaviour
// target: facts.Analysis.Names
// Two IDs that ShortName would show the same show as the shortest trailing paths that tell them
// apart, each with as few path elements as it needs; an ID that collides with nothing, and every ID
// when Names is nil, shows as ShortName does.
func TestNamesShowCollidingIDsWithTheShortestPathThatTellsThemApart(t *testing.T) {
	a := &facts.Analysis{
		Symbols: []facts.Symbol{
			{ID: "src/routes/shops/[id].load"},
			{ID: "src/routes/orders/[id].load"},
			{ID: "src/routes/orders/[id].actions"},
			{ID: "example.com/app/a/x/store.New"},
			{ID: "example.com/app/b/x/store.New"},
			{ID: "example.com/app/c/y/store.New"},
		},
		Tests: []facts.Test{{Calls: []facts.Call{{Target: "worker/queue.Drain"}, {Target: "jobs/queue.Drain"}}}},
	}
	names := a.Names()
	for id, want := range map[string]string{
		"src/routes/shops/[id].load":     "shops/[id].load",
		"src/routes/orders/[id].load":    "orders/[id].load",
		"src/routes/orders/[id].actions": "[id].actions",
		"example.com/app/a/x/store.New":  "a/x/store.New",
		"example.com/app/b/x/store.New":  "b/x/store.New",
		"example.com/app/c/y/store.New":  "y/store.New",
		"worker/queue.Drain":             "worker/queue.Drain",
		"jobs/queue.Drain":               "jobs/queue.Drain",
	} {
		if got := names.Short(id); got != want {
			t.Errorf("Short(%q) = %q, want %q", id, got, want)
		}
	}
	// An ID that is the trailing path of another is shown whole, and so is the other.
	nested := (&facts.Analysis{Symbols: []facts.Symbol{{ID: "a/x.F"}, {ID: "b/a/x.F"}}}).Names()
	if got := nested.Short("a/x.F") + " " + nested.Short("b/a/x.F"); got != "a/x.F b/a/x.F" {
		t.Errorf("nested: got %q, want %q", got, "a/x.F b/a/x.F")
	}
	if got := facts.Names(nil).Short("src/routes/shops/[id].load"); got != "[id].load" {
		t.Errorf("nil Names: Short = %q, want [id].load", got)
	}
}
