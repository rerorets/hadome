package engine_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

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
