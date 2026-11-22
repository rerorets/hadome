// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

//go:build cgo

package code

import (
	"strings"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/engine"
)

func init() {
	languageConfigLiteralCases = everyLanguage("code/config-literal", "a URL added inside a function fails in %s", map[string][2]string{
		"typescript": {"export function fetchAll() {\n  return get(\"/v1/orders\");\n}\n", "export function fetchAll() {\n  return get(\"https://api.shop.internal/v1/orders\");\n}\n"},
		"python":     {"def fetch_all():\n    return get(\"/v1/orders\")\n", "def fetch_all():\n    return get(\"https://api.shop.internal/v1/orders\")\n"},
		"java":       {javaClass("Object fetchAll() { return get(\"/v1/orders\"); }"), javaClass("Object fetchAll() { return get(\"https://api.shop.internal/v1/orders\"); }")},
		"kotlin":     {"package app\n\nfun fetchAll() = get(\"/v1/orders\")\n", "package app\n\nfun fetchAll() = get(\"https://api.shop.internal/v1/orders\")\n"},
		"rust":       {"pub fn fetch_all() -> String {\n    get(\"/v1/orders\")\n}\n", "pub fn fetch_all() -> String {\n    get(\"https://api.shop.internal/v1/orders\")\n}\n"},
	})
	languageSwallowedErrorCases = everyLanguage("code/swallowed-error", "an empty catch added fails in %s", map[string][2]string{
		"typescript": {"export function save(s: () => void) {\n  s();\n}\n", "export function save(s: () => void) {\n  try { s(); } catch (e) {}\n}\n"},
		"python":     {"def save(s):\n    s()\n", "def save(s):\n    try:\n        s()\n    except Exception:\n        pass\n"},
		"java":       {javaClass("void save(Runnable s) { s.run(); }"), javaClass("void save(Runnable s) { try { s.run(); } catch (RuntimeException e) {} }")},
		"kotlin":     {"package app\n\nfun save(s: () -> Unit) {\n    s()\n}\n", "package app\n\nfun save(s: () -> Unit) {\n    try { s() } catch (e: Exception) {}\n}\n"},
		"rust":       {"pub fn save(tx: Sender<i32>) {\n    let _ = tx;\n}\n", "pub fn save(tx: Sender<i32>) {\n    tx.send(1).ok();\n}\n"},
	})
	ts := func(a, b string) fstest.MapFS {
		return fstest.MapFS{"package.json": {Data: []byte("{}\n")}, "src/billing.ts": {Data: []byte(a)}, "src/invoice.ts": {Data: []byte(b)}}
	}
	round := func(name, v, half string) string {
		return "export function " + name + "(" + v + ": number, digits: number): number {\n" +
			"  let scale = 1;\n  for (let i = 0; i < digits; i++) {\n    scale *= 10;\n  }\n" +
			"  if (" + v + " < 0) {\n    return -Math.floor(-" + v + " * scale + " + half + ") / scale;\n  }\n" +
			"  return Math.floor(" + v + " * scale + " + half + ") / scale;\n}\n"
	}
	languageDuplicateCases = []engine.Case{
		{Name: "a copy with renamed variables added in another TypeScript file fails", WantFail: []string{"code/duplicate"},
			Base: ts(round("roundTotal", "total", "0.5"), ""), Head: ts(round("roundTotal", "total", "0.5"), round("roundInvoice", "sum", "0.5"))},
		{Name: "a TypeScript function with another literal passes",
			Base: ts(round("roundTotal", "total", "0.5"), ""), Head: ts(round("roundTotal", "total", "0.5"), round("roundInvoice", "sum", "0.4"))},
	}
	languageDivergedCopyCases = []engine.Case{
		{Name: "fixing one of two TypeScript copies reports the other", WantFail: []string{"code/diverged-copy"},
			Base: ts(round("roundTotal", "total", "0.5"), round("roundInvoice", "total", "0.5")),
			Head: ts(round("roundTotal", "total", "0.4999"), round("roundInvoice", "total", "0.5"))},
	}
}

func javaClass(member string) string {
	return "package app;\n\npublic class Client {\n    " + member + "\n}\n"
}

// everyLanguage makes one case per language from a base and a head source file, each of which
// must fail the rule.
func everyLanguage(rule, name string, sources map[string][2]string) []engine.Case {
	files := map[string]struct{ path, extra, extraData string }{
		"typescript": {"src/client.ts", "package.json", "{}\n"},
		"python":     {"app/client.py", "app/__init__.py", ""},
		"java":       {"src/main/java/app/Client.java", "", ""},
		"kotlin":     {"src/main/kotlin/app/Client.kt", "", ""},
		"rust":       {"src/lib.rs", "Cargo.toml", "[package]\nname = \"app\"\n"},
	}
	var out []engine.Case
	for _, lang := range []string{"typescript", "python", "java", "kotlin", "rust"} {
		f, src := files[lang], sources[lang]
		tree := func(code string) fstest.MapFS {
			t := fstest.MapFS{f.path: {Data: []byte(code)}}
			if f.extra != "" {
				t[f.extra] = &fstest.MapFile{Data: []byte(f.extraData)}
			}
			return t
		}
		out = append(out, engine.Case{Name: strings.Replace(name, "%s", lang, 1), WantFail: []string{rule},
			Base: tree(src[0]), Head: tree(src[1])})
	}
	return out
}
