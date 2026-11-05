package notices_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/rerorets/hadome/internal/notices"
)

var std = notices.Module{Path: "Go standard library", Source: "https://go.dev/dl/",
	Licenses: []notices.License{{Name: "LICENSE", Text: "BSD text\n"}}}

func mod(path, version, text string) notices.Module {
	return notices.Module{Path: path, Version: version, Source: notices.ProxySource(path, version),
		Licenses: []notices.License{{Name: "LICENSE", Text: text}}}
}

// viewpoint: behaviour
// target: notices.Render, notices.Problems
// A file written for the linked modules has nothing wrong with it, whatever order the modules came in,
// and carries each module's version, source and license text.
func TestRenderedNoticesHaveNoProblems(t *testing.T) {
	mods := []notices.Module{mod("example.com/b", "v2.0.0", "MIT text"), mod("example.com/a", "v1.0.0", "Apache text\r\n")}
	file := notices.Render(std, mods)
	reversed := slices.Clone(mods)
	slices.Reverse(reversed)
	if p := notices.Problems(file, std, reversed); len(p) != 0 {
		t.Errorf("problems in a rendered file: %v", p)
	}
	text := string(file)
	for _, want := range []string{
		"example.com/a v1.0.0\nSource: https://proxy.golang.org/example.com/a/@v/v1.0.0.zip\n",
		"\n--- LICENSE\n\nApache text\n",
		"\n--- LICENSE\n\nMIT text\n",
		"Go standard library\nSource: https://go.dev/dl/\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the file lacks %q", want)
		}
	}
	if strings.Index(text, "example.com/a") > strings.Index(text, "example.com/b") {
		t.Error("modules are not in the order of their paths")
	}
}

// viewpoint: failure handling
// target: notices.Problems
// Every way the file can fall behind the modules is named, so that the one who regenerates it knows
// what changed.
func TestProblemsNameWhatIsOutOfDate(t *testing.T) {
	old := notices.Render(std, []notices.Module{mod("example.com/a", "v1.0.0", "MIT"), mod("example.com/gone", "v1.0.0", "MIT")})
	got := notices.Problems(old, std, []notices.Module{
		mod("example.com/a", "v1.0.0", "MIT, amended"),
		mod("example.com/new", "v0.1.0", "BSD"),
		{Path: "example.com/bare", Version: "v1.0.0", Source: "https://example.com"},
	})
	want := []string{
		"example.com/bare v1.0.0: ships no license file; find its license before shipping it",
		"example.com/a v1.0.0: the source or license text is out of date",
		"example.com/bare v1.0.0: missing",
		"example.com/new v0.1.0: missing",
		"example.com/gone v1.0.0: no longer linked into hadome",
	}
	if !slices.Equal(got, want) {
		t.Errorf("problems:\n got %q\nwant %q", got, want)
	}
}

// viewpoint: failure handling
// target: notices.Problems
// An upgraded module is a different version: its old section is stale and its new one missing.
func TestProblemsCatchAnUpgrade(t *testing.T) {
	old := notices.Render(std, []notices.Module{mod("example.com/a", "v1.0.0", "MIT")})
	got := notices.Problems(old, std, []notices.Module{mod("example.com/a", "v1.1.0", "MIT")})
	want := []string{"example.com/a v1.1.0: missing", "example.com/a v1.0.0: no longer linked into hadome"}
	if !slices.Equal(got, want) {
		t.Errorf("problems:\n got %q\nwant %q", got, want)
	}
}

// viewpoint: boundaries and errors
// target: notices.ProxySource
// The module proxy names an upper-case letter as '!' and its lower case.
func TestProxySourceEscapesUpperCase(t *testing.T) {
	got := notices.ProxySource("github.com/Example/Mod", "v1.0.0-RC1")
	if want := "https://proxy.golang.org/github.com/!example/!mod/@v/v1.0.0-!r!c1.zip"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}
