package main

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"strings"
	"testing"
	"unicode"

	"github.com/rerorets/hadome/internal/adapter"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/messages"
	"github.com/rerorets/hadome/internal/mutation"
)

type caseSource struct{ base, head fs.FS }

func (s caseSource) Base() (engine.Version, error) {
	return engine.Version{Label: "base", FS: s.base}, nil
}
func (s caseSource) Head() (engine.Version, error) {
	return engine.Version{Label: "head", FS: s.head}, nil
}
func (s caseSource) Renames() (map[string]string, error) { return nil, nil }

// formats adds the formats of t and of the wording inside it.
func formats(t engine.Text, into map[string]bool) {
	into[t.Format] = true
	for _, a := range t.Args {
		if inner, ok := a.(engine.Text); ok {
			formats(inner, into)
		}
	}
}

// hasWords reports whether a format has anything to translate besides its verbs.
func hasWords(format string) bool {
	for i := 0; i < len(format); i++ {
		if format[i] == '%' {
			i++ // the verb letter
			continue
		}
		if r := rune(format[i]); r < unicode.MaxASCII && unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

// viewpoint: invariant
// target: internal/messages (the Japanese catalog)
// Messages invariant 3: every wording that the built-in rules' self-test cases produce, and every
// rule's own description, has a Japanese version. A new or reworded message fails here until the
// catalog has it.
func TestEveryBuiltInWordingIsInTheJapaneseCatalog(t *testing.T) {
	seen := map[string]bool{}
	rules := append(newEngine().Rules, mutation.Rule())
	for _, rule := range rules {
		if d, ok := rule.(engine.Describer); ok {
			a := d.About()
			for _, text := range []engine.Text{a.Finds, a.Why, a.Fix} {
				formats(text, seen)
			}
		}
		for _, c := range rule.Cases() {
			e := &engine.Engine{Analyzer: adapter.New(), Rules: []engine.Rule{rule}}
			r, err := e.Check(caseSource{base: c.Base, head: c.Head})
			if err != nil {
				t.Fatalf("%s: %s: %v", rule.ID(), c.Name, err)
			}
			for _, f := range append(r.Regressions, r.Reviews...) {
				for _, text := range []engine.Text{f.Message, f.Why, f.Fix} {
					formats(text, seen)
				}
			}
		}
	}
	if len(seen) < len(rules) {
		t.Fatalf("collected only %d formats from %d rules", len(seen), len(rules))
	}
	for format := range seen {
		if _, ok := messages.Japanese[format]; hasWords(format) && !ok {
			t.Errorf("no Japanese for %q (internal/messages)", format)
		}
	}
}

// viewpoint: external contract
// target: hadome check --lang, messages in .hadome.yml
// The language is --lang, else messages in head's configuration, else English. JSON keys, rules and
// the exit code do not change with it.
func TestCheckWritesTheChosenLanguage(t *testing.T) {
	base := writeTree(t, project(""))
	plain := writeTree(t, project("\t_ = time.Now()\n"))
	japanese := project("\t_ = time.Now()\n")
	japanese[".hadome.yml"] = "schema: 1\nmessages: ja\n"
	configured := writeTree(t, japanese)

	message := func(head string, args ...string) (string, string) {
		t.Helper()
		var out, errOut bytes.Buffer
		code := run(append([]string{"check", "--base-dir", base, "--head-dir", head, "--format", "json"}, args...), &out, &errOut)
		if code != engine.ExitRegression {
			t.Fatalf("want exit 1, got %d\n%s%s", code, out.String(), errOut.String())
		}
		var report struct {
			Regressions []struct {
				Rule    string `json:"rule"`
				Message string `json:"message"`
			} `json:"regressions"`
		}
		if err := json.Unmarshal(out.Bytes(), &report); err != nil || len(report.Regressions) != 1 {
			t.Fatalf("want one regression in JSON, got %v\n%s", err, out.String())
		}
		return report.Regressions[0].Rule, report.Regressions[0].Message
	}
	rule, english := message(plain)
	if !strings.Contains(english, "test reads the clock") {
		t.Fatalf("by default the message is English, got %q", english)
	}
	for _, c := range []struct {
		name, head string
		args       []string
		japanese   bool
	}{
		{"--lang ja", plain, []string{"--lang", "ja"}, true},
		{"messages: ja", configured, nil, true},
		{"--lang en over messages: ja", configured, []string{"--lang", "en"}, false},
	} {
		gotRule, got := message(c.head, c.args...)
		if gotRule != rule {
			t.Errorf("%s: the rule changed from %s to %s", c.name, rule, gotRule)
		}
		if (got != english) != c.japanese {
			t.Errorf("%s: want Japanese %v, got %q", c.name, c.japanese, got)
		}
	}
}

// viewpoint: boundaries and errors
// target: hadome check, mutate, explain and agents --lang
// Every --lang accepts the values of messages, and anything else is a configuration error.
func TestUnknownLanguageIsRefused(t *testing.T) {
	for _, args := range [][]string{
		{"check", "--lang", "fr"},
		{"mutate", "--lang", "fr"},
		{"explain", "--lang", "fr", "tests/no-sleep"},
		{"agents", "--lang", "fr"},
	} {
		var out, errOut bytes.Buffer
		if code := run(args, &out, &errOut); code != engine.ExitConfig {
			t.Errorf("%v: want exit 2, got %d", args, code)
		}
		if !strings.Contains(errOut.String(), "en or ja") {
			t.Errorf("%v: the error must name the languages, got %q", args, errOut.String())
		}
	}
}

// viewpoint: external contract
// target: hadome explain --lang
// A rule's description follows --lang; its ID and the labels stay as they are.
func TestExplainWritesARuleInTheChosenLanguage(t *testing.T) {
	got := map[string]string{}
	for _, lang := range []string{"en", "ja"} {
		var out, errOut bytes.Buffer
		if code := run([]string{"explain", "--lang", lang, "tests/no-sleep"}, &out, &errOut); code != engine.ExitOK {
			t.Fatalf("%s: want exit 0, got %d: %s", lang, code, errOut.String())
		}
		got[lang] = out.String()
	}
	if got["en"] == got["ja"] {
		t.Fatal("the description is the same in both languages")
	}
	for _, lang := range []string{"en", "ja"} {
		for _, label := range []string{"tests/no-sleep", "Finds: ", "Why: ", "Fix: "} {
			if !strings.Contains(got[lang], label) {
				t.Errorf("%s: %q is missing from\n%s", lang, label, got[lang])
			}
		}
	}
}
