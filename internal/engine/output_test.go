// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

package engine_test

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
)

// hostile is a report whose every value that comes from the repository tries to start a line of its
// own or to send the terminal an escape sequence.
func hostile() engine.Report {
	const name = "x\n::error title=INJECTED::forged\nb.py"
	return engine.Report{
		Regressions: []engine.Finding{{Rule: "gate/one\r::stop-commands::t", File: name, Line: 3,
			Message: engine.Verbatim("broke\n::warning::forged"), Why: engine.Verbatim("why\x1b[2J"),
			Fix: engine.Verbatim("fix\u009b31m")}},
		Reviews: []engine.Finding{{Rule: "gate/two", File: name, Line: 0,
			Message: engine.Verbatim("look\x07"), Why: engine.Verbatim("a\u2028::notice::b")}},
		NewExceptions: []engine.Marker{{Rule: "tests/no-clock", File: name, Line: 7,
			Reason: "ok\x1b]8;;https://example.com\x1b\\link\x1b]8;;\x1b\\\n::error::x"}},
		Candidates: []engine.Candidate{{Type: "durable", Targets: []string{"store\n::error::a"}, File: name, Line: 1,
			Why: "why\x7f"}},
		Unfollowed: []engine.Unfollowed{{Unit: "unit\n::error::u", Base: 1, Head: 2}},
		Introduced: []config.Change{{Key: "rules.size.file-lines", File: ".hadome.yml\n::error::k", Line: 2,
			Removed: "300\n::error::r", Added: "\xff400"}},
	}
}

// viewpoint: invariant
// target: engine.WriteText
// Engine invariant 7: no value from the repository can start a line of its own or put a control
// character into the text output: every line the output holds is one hadome started, none starts with "::" (which
// GitHub Actions runs as a workflow command), and nothing but the newlines hadome writes is a
// control character or a byte that is not UTF-8.
func TestTextOutputWritesNoControlCharacterFromTheRepository(t *testing.T) {
	for _, lang := range []string{"en", "ja"} {
		var out bytes.Buffer
		engine.WriteText(&out, hostile(), lang)
		text := out.String()
		if !utf8.ValidString(text) {
			t.Errorf("%s: the output is not UTF-8:\n%q", lang, text)
		}
		for _, r := range text {
			if r != '\n' && (r < 0x20 || (r >= 0x7f && r <= 0x9f) || r == '\u2028' || r == '\u2029') {
				t.Errorf("%s: the output holds the control character %U:\n%q", lang, r, text)
			}
		}
		lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
		for _, l := range lines {
			if strings.HasPrefix(strings.TrimLeft(l, " \t"), "::") {
				t.Errorf("%s: a line starts with \"::\": %q", lang, l)
			}
		}
		// A regression is 4 lines, a review 3, an exception 2, then a heading and 1 line for each of
		// the candidates, the unfollowed units and the introduced settings, then the summary.
		if want := 4 + 3 + 2 + 2 + 2 + 2 + 1; len(lines) != want {
			t.Errorf("%s: want %d lines, got %d:\n%s", lang, want, len(lines), text)
		}
	}
}

// viewpoint: external contract
// target: engine.WriteText
// A value with a control character is written quoted and escaped as a Go string literal, so a
// person still reads what it was.
func TestTextOutputQuotesAValueWithAControlCharacter(t *testing.T) {
	var out bytes.Buffer
	engine.WriteText(&out, hostile(), "en")
	text := out.String()
	for _, want := range []string{
		`"x\n::error title=INJECTED::forged\nb.py":3`,
		`"gate/one\r::stop-commands::t"`,
		`"broke\n::warning::forged"`,
		`why: "why\x1b[2J"`,
		`fix: "fix\u009b31m"`,
		`"look\a"`,
		`why: "a\u2028::notice::b"`,
		`new exception: "ok\x1b]8;;https://example.com\x1b\\link\x1b]8;;\x1b\\\n::error::x"`,
		`"durable store\n::error::a"`,
		`— "why\x7f"`,
		`"unit\n::error::u": 1 → 2`,
		`".hadome.yml\n::error::k":2`,
		`"300\n::error::r" → "\xff400"`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("want %s in the output:\n%s", want, text)
		}
	}
}

// viewpoint: external contract
// target: engine.WriteText
// A value without a control character is written as it is, quotes, backslashes and non-ASCII
// letters included.
func TestTextOutputWritesAPlainValueAsItIs(t *testing.T) {
	r := engine.Report{NewExceptions: []engine.Marker{{Rule: "tests/no-clock", File: `ストア\store.go`, Line: 4,
		Reason: `measures "wall" time on purpose`}}}
	var out bytes.Buffer
	engine.WriteText(&out, r, "en")
	for _, want := range []string{
		"new exception: measures \"wall\" time on purpose\n",
		"\n  ストア\\store.go:4\n",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("want %q in the output:\n%s", want, out.String())
		}
	}
}
