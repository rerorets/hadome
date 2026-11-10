package adapter_test

import (
	"errors"
	"io/fs"
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

// letGo counts how often a held result is let go.
var letGo int

type heldResult struct{}

func (heldResult) Release() { letGo++ }

// holding is a result that lives until the analysis call ends, like a catalog.
var holding = reuse.NewPure("test/holding", reuse.ScopePair, func(p struct{ Path string }) (heldResult, error) {
	return heldResult{}, nil
})

// failing is a language that claims .fail files and holds a result for each before reading it.
// It cannot read a file that says "bad", and as a linker it cannot link when link is set.
type failing struct{ link bool }

func (failing) Name() string         { return "failing" }
func (failing) Claims(p string) bool { return strings.HasSuffix(p, ".fail") }
func (f failing) Analyze(r *reuse.Run, _ config.Analysis, paths []string) (*facts.Analysis, error) {
	for _, p := range paths {
		if _, err := holding.Do(r, struct{ Path string }{p}); err != nil {
			return nil, err
		}
		data, err := fs.ReadFile(r.FS(), p)
		if err != nil {
			return nil, err
		}
		if string(data) == "bad" {
			return nil, errors.New("cannot read " + p)
		}
	}
	return &facts.Analysis{}, nil
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
// the version it failed on; the analysis never goes on as though the language found nothing. What
// the session held is let go all the same.
func TestALanguageOrLinkerThatFailsFailsTheAnalysis(t *testing.T) {
	good := fstest.MapFS{"x.fail": {Data: []byte("good")}}
	bad := fstest.MapFS{"x.fail": {Data: []byte("bad")}}
	cases := []struct {
		name       string
		lang       failing
		base, head fstest.MapFS
		failsOn    string
	}{
		{"base unreadable", failing{}, bad, good, "base"},
		{"head unreadable", failing{}, good, bad, "head"},
		{"link fails", failing{link: true}, good, good, "base"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			an := adapter.Analyzer{Languages: []adapter.Language{tc.lang}}
			letGo = 0
			if _, err := an.Analyze(engine.Version{Label: "v", FS: map[string]fstest.MapFS{"base": tc.base, "head": tc.head}[tc.failsOn]}, config.Config{}); err == nil {
				t.Error("reading the failing version alone did not fail")
			}
			if letGo != 1 {
				t.Errorf("reading one version let go of %d held results, want 1", letGo)
			}
			letGo = 0
			_, _, err := an.AnalyzePair(engine.Version{Label: "base", FS: tc.base}, config.Config{}, engine.Version{Label: "head", FS: tc.head}, config.Config{})
			if err == nil || !strings.Contains(err.Error(), "analysing "+tc.failsOn) {
				t.Errorf("reading both versions gave %v, want an error naming %s", err, tc.failsOn)
			}
			if letGo == 0 {
				t.Error("reading both versions let go of nothing it held")
			}
		})
	}
}
