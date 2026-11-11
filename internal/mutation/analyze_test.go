package mutation_test

import (
	"errors"
	"io"
	"testing"

	"github.com/rerorets/hadome/internal/adapter"
	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
	"github.com/rerorets/hadome/internal/mutation"
	"github.com/rerorets/hadome/internal/source"
)

// oneAtATime reads one version at a time, and cannot read the version labelled fail.
type oneAtATime struct{ fail string }

func (o oneAtATime) Analyze(v engine.Version, c config.Config) (*facts.Analysis, error) {
	if v.Label == o.fail {
		return nil, errors.New("cannot read " + v.Label)
	}
	return adapter.New().WithoutReuse().Analyze(v, c)
}

// viewpoint: failure
// target: mutation.Mutate.Run
// With an analyzer that reads one version at a time, a version it cannot read stops the run before
// any mutant is tried.
func TestAVersionTheAnalyzerCannotReadStopsTheRun(t *testing.T) {
	t.Parallel()
	code := "package calc\n\n// Add adds.\nfunc Add(a, b int) int { return a + b }\n"
	test := "package calc\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 {\n\t\tt.Fatal(\"sum\")\n\t}\n}\n"
	for _, label := range []string{"base", "head"} {
		m := &mutation.Mutate{Analyzer: oneAtATime{fail: label}, Mutator: adapter.New(), Workdir: t.TempDir(), Trees: t.TempDir(), Progress: io.Discard}
		_, err := m.Run(source.Dirs{BaseDir: dir(t, version(code, test)), HeadDir: dir(t, version(code, test))})
		if err == nil {
			t.Errorf("%s: the run went on", label)
		}
	}
}
