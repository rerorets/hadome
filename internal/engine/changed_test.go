package engine

import (
	"testing"

	"github.com/rerorets/hadome/internal/config"
)

// viewpoint: behaviour
// target: engine.changed
// An introduced setting shows what it replaced and what it is; a setting with only one side shows
// that side, marking a removal so it does not read as a value.
func TestAChangeShowsItsValues(t *testing.T) {
	for _, tc := range []struct {
		c    config.Change
		want string
	}{
		{config.Change{Removed: "400", Added: "500"}, "400 → 500"},
		{config.Change{Added: "ja"}, "ja"},
		{config.Change{Removed: "go"}, "removed go"},
	} {
		if got := changed(tc.c); got != tc.want {
			t.Errorf("%+v: got %q, want %q", tc.c, got, tc.want)
		}
	}
}
