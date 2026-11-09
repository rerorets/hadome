//go:build cgo

package adapter_test

import (
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/adapter"
	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
)

// viewpoint: behaviour
// target: adapter.Analyzer.Analyze
// Entry points come ordered by path, then line, whichever language found them.
func TestEntriesAreOrderedByPathAcrossLanguages(t *testing.T) {
	tree := fstest.MapFS{
		"go.mod": {Data: []byte("module example.com/app\n")},
		"zeta/web.go": {Data: []byte("package zeta\n\nimport \"net/http\"\n\n" +
			"func Orders(w http.ResponseWriter, r *http.Request) {}\n")},
		"package.json": {Data: []byte(`{"name":"app"}`)},
		"alpha/app.ts": {Data: []byte("import express from 'express'\n\nexport function users() {}\n\n" +
			"const app = express()\napp.post('/users', users)\n")},
	}
	a, err := adapter.New().Analyze(engine.Version{FS: tree}, config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range a.Entries {
		got = append(got, fmt.Sprintf("%s:%d", e.Path, e.Line))
	}
	if want := "alpha/app.ts:6 zeta/web.go:5"; strings.Join(got, " ") != want {
		t.Errorf("got %v, want %s", got, want)
	}
}
