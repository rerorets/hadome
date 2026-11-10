package adapter_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/adapter"
	"github.com/rerorets/hadome/internal/adapter/reuse"
	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
)

var withSQL = fstest.MapFS{
	"db/billing.sql": {Data: []byte("CREATE FUNCTION billing.is_active(id int) RETURNS boolean AS $$ SELECT true $$ LANGUAGE sql;\n")},
	"README.md":      {Data: []byte("# shop\n")},
}

// viewpoint: behaviour
// target: adapter.Analyzer.Analyze
// Reading one version builds its SQL catalog once: Link gets the catalog Analyze built. When the
// call returns, the session holds only what lives for the whole call, the catalog.
func TestOneVersionBuildsItsCatalogOnce(t *testing.T) {
	_, held, err := adapter.New().AnalyzeHeld(engine.Version{FS: withSQL}, config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if st := held.Stats["sql/build"]; st.Misses != 1 || st.Hits != 1 {
		t.Errorf("the catalog was built %d times and served %d times, want built once and served to Link once", st.Misses, st.Hits)
	}
	if !slices.Equal(held.Remembered, []string{"sql/build"}) {
		t.Errorf("the session holds %v at the end, want only the catalog", held.Remembered)
	}
}

// viewpoint: behaviour
// target: adapter.Analyzer.WithoutReuse
// The analyzer without reuse serves no stage from what it remembered, and still gives the same
// facts.
func TestTheAnalyzerWithoutReuseServesNothing(t *testing.T) {
	plain, ok := adapter.New().WithoutReuse().(adapter.Analyzer)
	if !ok {
		t.Fatal("WithoutReuse is not an adapter.Analyzer")
	}
	_, held, err := plain.AnalyzeHeld(engine.Version{FS: withSQL}, config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for id, st := range held.Stats {
		if st.Hits != 0 {
			t.Errorf("stage %s served %d times without reuse", id, st.Hits)
		}
	}
	b, h, stats, err := plain.PairWithStats(engine.Version{FS: mixed}, config.Config{}, engine.Version{FS: clone(mixed)}, config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for id, st := range stats {
		if st.Hits != 0 {
			t.Errorf("pair: stage %s served %d times without reuse", id, st.Hits)
		}
	}
	if len(b.Files) == 0 || len(h.Files) != len(b.Files) {
		t.Errorf("base has %d files and head %d", len(b.Files), len(h.Files))
	}
}

// viewpoint: behaviour
// target: adapter.Analyzer.AnalyzePair
// When both versions are read, the session holds only what lives for the whole call: every
// language's results were let go when head's analysis of the language ended.
func TestAPairKeepsOnlyTheCatalogToTheEnd(t *testing.T) {
	head := clone(mixed)
	head["store/store.go"] = &fstest.MapFile{Data: []byte("package store\n\nfunc Append(row string) error { return nil }\n\nfunc More() {}\n")}
	held, err := adapter.New().PairHeld(engine.Version{Label: "base", FS: mixed}, engine.Version{Label: "head", FS: head}, config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(held.Remembered, []string{"sql/build"}) {
		t.Errorf("the session holds %v at the end, want only catalogs", held.Remembered)
	}
}

// failing is a language that claims .fail files and cannot read them, and a linker that cannot
// link when asked to.
type failing struct{ link bool }

func (failing) Name() string         { return "failing" }
func (failing) Claims(p string) bool { return strings.HasSuffix(p, ".fail") }
func (f failing) Analyze(r *reuse.Run, _ config.Analysis, paths []string) (*facts.Analysis, error) {
	if f.link {
		return &facts.Analysis{}, nil
	}
	return nil, errors.New("cannot read " + paths[0])
}
func (failing) ReadsTexts(config.Analysis, []string) bool { return false }
func (f failing) Link(*reuse.Run, config.Analysis, *facts.Analysis) error {
	if f.link {
		return errors.New("cannot link")
	}
	return nil
}

// viewpoint: failure
// target: adapter.Analyzer.AnalyzePair
// A language that cannot read a version, or a linker that cannot link it, fails the analysis with
// the version it failed on; the analysis never goes on as though the language found nothing.
func TestALanguageOrLinkerThatFailsFailsTheAnalysis(t *testing.T) {
	tree := fstest.MapFS{"x.fail": {Data: []byte("x")}}
	for _, f := range []failing{{link: false}, {link: true}} {
		an := adapter.Analyzer{Languages: []adapter.Language{f}}
		if _, err := an.Analyze(engine.Version{Label: "v", FS: tree}, config.Config{}); err == nil {
			t.Errorf("link %v: reading one version did not fail", f.link)
		}
		_, _, err := an.AnalyzePair(engine.Version{Label: "base", FS: tree}, config.Config{}, engine.Version{Label: "head", FS: tree}, config.Config{})
		if err == nil || !strings.Contains(err.Error(), "analysing base") {
			t.Errorf("link %v: reading both versions gave %v, want an error naming base", f.link, err)
		}
	}
}
