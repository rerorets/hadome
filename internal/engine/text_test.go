package engine_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/messages"
)

// viewpoint: invariant
// target: engine.Text.In
// Messages invariant 4: wording the catalog lacks comes out in English, filled in as fmt would.
func TestWordingWithoutJapaneseComesOutInEnglish(t *testing.T) {
	format := "wording no catalog has: %s, %d"
	if _, ok := messages.Japanese[format]; ok {
		t.Fatalf("the test needs wording the catalog lacks, but it has %q", format)
	}
	got := engine.T(format, "x", 2).In("ja")
	if got != "wording no catalog has: x, 2" {
		t.Fatalf("want the English wording, got %q", got)
	}
}

// viewpoint: invariant
// target: engine.Text.In
// English writes every format as it is; Japanese writes the catalog's format.
func TestEachLanguageWritesItsOwnFormat(t *testing.T) {
	for en, ja := range messages.Japanese {
		if got := engine.T(en).In("en"); got != en {
			t.Errorf("in English, %q came out as %q", en, got)
		}
		if got := engine.T(en).In("ja"); got != ja {
			t.Errorf("in Japanese, %q came out as %q, not the catalog's %q", en, got, ja)
		}
	}
}

// viewpoint: invariant
// target: engine.Text.In
// Wording inside wording is written in the same language as the wording around it.
func TestWordingInsideWordingIsInTheSameLanguage(t *testing.T) {
	inner := engine.T("remove the marker")
	if inner.In("ja") == inner.In("en") {
		t.Fatal("the test needs inner wording that the catalog translates")
	}
	outer := engine.T("outer wording: %s", inner)
	for _, lang := range []string{"en", "ja"} {
		if got, want := outer.In(lang), fmt.Sprintf("outer wording: %s", inner.In(lang)); got != want {
			t.Errorf("%s: want %q, got %q", lang, want, got)
		}
	}
}

// viewpoint: invariant
// target: engine.Verbatim
// What hadome did not write, such as a gate's message, comes out as written in every language,
// even when it happens to match wording the catalog translates.
func TestVerbatimWordingIsNeverTranslated(t *testing.T) {
	const written = "remove the marker"
	if _, ok := messages.Japanese[written]; !ok {
		t.Fatalf("the test needs wording the catalog translates, but it lacks %q", written)
	}
	for _, lang := range []string{"en", "ja"} {
		if got := engine.Verbatim(written).In(lang); got != written {
			t.Errorf("%s: want %q, got %q", lang, written, got)
		}
	}
}

// viewpoint: invariant
// target: engine.WriteJSON
// Only the values of message, why and fix depend on the language: keys, rules, places and the exit
// code are the same, so agents and scripts read either.
func TestJSONDiffersByLanguageOnlyInWording(t *testing.T) {
	r := check(t, treeSource{base: module(""), head: module("\t_ = time.Now()\n")})
	if len(r.Regressions) == 0 {
		t.Fatal("the test needs a regression")
	}
	decoded := map[string]map[string]any{}
	for _, lang := range []string{"en", "ja"} {
		var out bytes.Buffer
		if err := engine.WriteJSON(&out, r, lang); err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(out.Bytes(), &m); err != nil {
			t.Fatalf("%s: not JSON: %v", lang, err)
		}
		decoded[lang] = m
	}
	en, ja := decoded["en"]["regressions"].([]any), decoded["ja"]["regressions"].([]any)
	for i := range en {
		e, j := en[i].(map[string]any), ja[i].(map[string]any)
		if e["message"] == j["message"] {
			t.Errorf("the message is the same in both languages: %v", e["message"])
		}
		for _, k := range []string{"message", "why", "fix"} {
			if s, ok := j[k].(string); !ok || s == "" {
				t.Errorf("ja: %s must be a non-empty string, got %v", k, j[k])
			}
			delete(e, k)
			delete(j, k)
		}
	}
	if !reflect.DeepEqual(decoded["en"], decoded["ja"]) {
		t.Fatalf("apart from the wording, the reports differ:\nen %v\nja %v", decoded["en"], decoded["ja"])
	}
}

// viewpoint: invariant
// target: engine.WriteText
// The summary line comes out in the chosen language.
func TestTextSummaryIsInTheChosenLanguage(t *testing.T) {
	r := check(t, treeSource{base: module(""), head: module("\t_ = time.Now()\n")})
	last := map[string]string{}
	for _, lang := range []string{"en", "ja"} {
		var out bytes.Buffer
		engine.WriteText(&out, r, lang)
		lines := strings.Split(strings.TrimSpace(out.String()), "\n")
		last[lang] = lines[len(lines)-1]
	}
	if last["en"] == last["ja"] {
		t.Fatalf("the summary is the same in both languages: %q", last["en"])
	}
	if !strings.Contains(last["en"], "regression") {
		t.Fatalf("the English summary must count regressions, got %q", last["en"])
	}
}

// viewpoint: state integrity
// target: engine.Engine.Check
// Messages invariant 1: wording plays no part in comparing versions. A finding already in base
// stays untouched when head switches to Japanese, and switching never needs review (LooserNever).
// guards: base-never-blocks
func TestSwitchingTheLanguageChangesNoVerdict(t *testing.T) {
	tree := module("\t_ = time.Now()\n")
	r := check(t, treeSource{base: withConfig(tree, "schema: 1\nmessages: en\n"), head: withConfig(tree, "schema: 1\nmessages: ja\n")})
	if r.ExitCode() != engine.ExitOK || r.Untouched != 1 {
		t.Fatalf("want exit 0 with the finding untouched, got exit %d, untouched %d, %+v %+v",
			r.ExitCode(), r.Untouched, r.Regressions, r.Reviews)
	}
	if r.Messages != "ja" {
		t.Fatalf("the report must carry head's messages, got %q", r.Messages)
	}
}

// viewpoint: invariant
// target: engine.WriteText
// The sentences that introduce a list explain it to the reader, so they come out in the chosen
// language; the listed keys and places stay as they are.
func TestListIntroductionsAreInTheChosenLanguage(t *testing.T) {
	r := engine.Report{
		Candidates: []engine.Candidate{{Type: "durable", Targets: []string{"store.Append"}, File: "store/store.go", Line: 3, Why: "writes rows"}},
		Introduced: []config.Change{{Key: "rules.size.file-lines", File: ".hadome.yml", Line: 4, Removed: "400", Added: "500"}},
	}
	out := map[string]string{}
	for _, lang := range []string{"en", "ja"} {
		var b bytes.Buffer
		engine.WriteText(&b, r, lang)
		out[lang] = b.String()
	}
	if n := strings.Count(out["en"], "information only"); n != 2 {
		t.Errorf("en: want both introductions in English, found %d\n%s", n, out["en"])
	}
	if strings.Contains(out["ja"], "information only") || strings.Count(out["ja"], "知らせるだけ") != 2 {
		t.Errorf("ja: want both introductions in Japanese\n%s", out["ja"])
	}
	for _, kept := range []string{"durable store.Append", "rules.size.file-lines  .hadome.yml:4  400 → 500"} {
		if !strings.Contains(out["ja"], kept) {
			t.Errorf("ja: %q must stay as it is\n%s", kept, out["ja"])
		}
	}
}

// viewpoint: external contract
// target: engine.WriteText and engine.WriteJSON
// A finding about a whole file (file-lines, purity-lost) has no line. The text output names only
// the file, never "file:0"; the JSON keeps line 0, which CI reads as the whole file.
func TestAWholeFileFindingShowsNoLine(t *testing.T) {
	r := engine.Report{Regressions: []engine.Finding{{Rule: "size/file-lines", File: "store/store.go",
		Message: engine.T("too long"), Why: engine.T("why"), Fix: engine.T("fix")}}}
	var text bytes.Buffer
	engine.WriteText(&text, r, "en")
	if strings.Contains(text.String(), "store/store.go:") || !strings.Contains(text.String(), "\n  store/store.go\n") {
		t.Errorf("text: want the file alone on its line\n%s", text.String())
	}
	var js bytes.Buffer
	if err := engine.WriteJSON(&js, r, "en"); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Regressions []struct {
			Line *int `json:"line"`
		} `json:"regressions"`
	}
	if err := json.Unmarshal(js.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Regressions) != 1 || got.Regressions[0].Line == nil || *got.Regressions[0].Line != 0 {
		t.Errorf("json: want line 0\n%s", js.String())
	}
}
