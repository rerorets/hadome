//go:build !sqlgrammars

package sql

import (
	"slices"
	"sort"
	"testing"
)

// viewpoint: behaviour
// target: sql.grammars
// A build links the grammars of the enabled dialects and no other: enabling a dialect is adding its
// grammar to the build and setting Enabled, and a dialect nobody can choose costs no binary size.
func TestTheBuildLinksTheGrammarsOfEnabledDialects(t *testing.T) {
	var linked []string
	for name := range grammars {
		linked = append(linked, name)
	}
	sort.Strings(linked)
	if got := EnabledDialects(); !slices.Equal(linked, got) {
		t.Errorf("grammars %v are linked, enabled dialects are %v", linked, got)
	}
}
