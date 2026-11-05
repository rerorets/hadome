package messages_test

import (
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode"

	"github.com/rerorets/hadome/internal/messages"
)

// verb is one formatting verb of fmt, with its flags, width and precision. %% is not a verb: it
// takes no argument.
var verb = regexp.MustCompile(`%[-+# 0]*(?:\[\d+\])?(?:\d+|\*)?(?:\.(?:\d+|\*))?[a-zA-Z%]`)

func verbs(format string) []string {
	var out []string
	for _, v := range verb.FindAllString(format, -1) {
		if v != "%%" {
			out = append(out, v)
		}
	}
	return out
}

// viewpoint: invariant
// target: messages.Japanese
// Messages invariant 2: every Japanese format takes the same arguments as its English format, the
// same verbs in the same order. Otherwise the arguments a rule passes would land in the wrong
// place, or print as %!s(MISSING).
func TestJapaneseHasTheVerbsOfEnglishInTheSameOrder(t *testing.T) {
	if got, want := verbs("%s took %5.1f%% of %d, %-8q"), []string{"%s", "%5.1f", "%d", "%-8q"}; !slices.Equal(got, want) {
		t.Fatalf("the verb checker itself: want %v, got %v", want, got)
	}
	for en, ja := range messages.Catalog("ja") {
		if !slices.Equal(verbs(en), verbs(ja)) {
			t.Errorf("%q has verbs %v, its Japanese %q has %v", en, verbs(en), ja, verbs(ja))
		}
	}
}

// viewpoint: invariant
// target: messages.Japanese
// Messages invariant 4: no entry is empty, so that empty wording never comes out.
func TestJapaneseHasNoEmptyEntry(t *testing.T) {
	for en, ja := range messages.Catalog("ja") {
		if strings.TrimSpace(ja) == "" {
			t.Errorf("%q has an empty Japanese format", en)
		}
		if strings.TrimSpace(en) == "" {
			t.Errorf("the catalog has an entry for empty wording: %q", ja)
		}
	}
}

// viewpoint: invariant
// target: messages.Japanese
// Every English format has words: a format that is only verbs, such as the one that carries a
// gate's own wording, is never translated.
func TestJapaneseHasNoEntryWithoutWords(t *testing.T) {
	for en := range messages.Catalog("ja") {
		if !strings.ContainsFunc(verb.ReplaceAllString(en, ""), unicode.IsLetter) {
			t.Errorf("%q has no words, so it must not be in the catalog", en)
		}
	}
}

// viewpoint: invariant
// target: messages.Catalog
// English and languages hadome does not write in have no catalog, so their wording stays English.
func TestOnlyJapaneseHasACatalog(t *testing.T) {
	if len(messages.Catalog("ja")) == 0 {
		t.Error("ja has no catalog")
	}
	for _, lang := range []string{"en", "", "fr"} {
		if c := messages.Catalog(lang); len(c) != 0 {
			t.Errorf("%q has a catalog of %d entries", lang, len(c))
		}
	}
}
