// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

//go:build cgo

package treesitter

import (
	"fmt"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/facts"
)

// assertedOf returns a symbol's calls and calls through a value's type, then those of them taken
// as asserted, or "no symbol".
func assertedOf(a *facts.Analysis, id string) string {
	for _, s := range a.Symbols {
		if s.ID == id {
			return fmt.Sprintf("%v %v asserted=%v", s.Calls, s.Through, s.Asserted)
		}
	}
	return "no symbol"
}

// viewpoint: behaviour
// target: java.trace, kotlin.trace, typescript.trace
// A method called on a local declared with a cast as its value (Java's (T) e, Kotlin's e as T,
// TypeScript's e as T and <T>e, in parentheses or not) is among the symbol's Asserted calls; one
// called on a parameter, a field or an object made in place is not, even when the same method is
// also called on a cast, and neither is one called on a local that a later declaration of the name
// sets to anything else, or that is assigned a cast after it is declared.
func TestCallsOnCastsAreAsserted(t *testing.T) {
	const java = "src/main/java/com/ex/"
	j := analyze(t, "java", fstest.MapFS{
		java + "repo/Port.java": {Data: []byte("package com.ex.repo;\n\npublic interface Port {\n    String find(String id);\n}\n")},
		java + "api/Api.java": {Data: []byte("package com.ex.api;\n\nimport com.ex.repo.Port;\n\npublic class Api {\n" +
			"    private final Port held;\n    public Api(Port held) { this.held = held; }\n" +
			"    public String asked(Object e) { Port p = (Port) e; return p.find(\"x\"); }\n" +
			"    public String paren(Object e) { Port p = ((Port) e); return p.find(\"x\"); }\n" +
			"    public String plain(Port p) { return p.find(\"x\"); }\n" +
			"    public String both(Object e) { Port p = (Port) e; p.find(\"x\"); return held.find(\"y\"); }\n" +
			"    public String again(Object e) { if (e != null) { Port p = (Port) e; } Port p = held; return p.find(\"x\"); }\n}\n")},
	})
	const kt = "src/main/kotlin/com/ex/"
	k := analyze(t, "kotlin", fstest.MapFS{
		kt + "repo/Port.kt": {Data: []byte("package com.ex.repo\n\ninterface Port {\n    fun find(id: String): String\n}\n")},
		kt + "api/Api.kt": {Data: []byte("package com.ex.api\n\nimport com.ex.repo.Port\n\nclass Api(private val held: Port) {\n" +
			"    fun asked(e: Any): String {\n        val p: Port = e as Port\n        return p.find(\"x\")\n    }\n" +
			"    fun paren(e: Any): String {\n        val p: Port = (e as Port)\n        return p.find(\"x\")\n    }\n" +
			"    fun plain(p: Port): String = p.find(\"x\")\n" +
			"    fun both(e: Any): String {\n        val p: Port = e as Port\n        p.find(\"x\")\n        return held.find(\"y\")\n    }\n" +
			"    fun later(e: Any): String {\n        val p: Port\n        p = e as Port\n        return p.find(\"x\")\n    }\n}\n")},
	})
	ts := analyze(t, "typescript", fstest.MapFS{
		"package.json":  {Data: []byte(`{"name":"app"}`)},
		"src/repo/r.ts": {Data: []byte("export interface Port { find(id: string): string }\nexport class Repo { find(id: string): string { return id } }\n")},
		"src/api/a.ts": {Data: []byte("import { Port, Repo } from '../repo/r'\n\n" +
			"export const arrow = (e: unknown): string => { const p: Port = e as Port; return p.find('x') }\n\n" +
			"export class Api {\n  constructor(private held: Port) {}\n" +
			"  asked(e: unknown): string { const p: Port = e as Port; return p.find('x') }\n" +
			"  angle(e: unknown): string { const p: Port = <Port>e; return p.find('x') }\n" +
			"  plain(p: Port): string { return p.find('x') }\n" +
			"  both(e: unknown): string { const p: Port = (e as Port); p.find('x'); return this.held.find('y') }\n" +
			"  made(e: unknown): string { const r: Repo = e as Repo; r.find('x'); return new Repo().find('y') }\n}\n")},
	})
	for _, c := range []struct {
		a    *facts.Analysis
		id   string
		want string
	}{
		{j, "com.ex.api.Api.asked", "[com.ex.repo.Port.find] [] asserted=[com.ex.repo.Port.find]"},
		{j, "com.ex.api.Api.paren", "[com.ex.repo.Port.find] [] asserted=[com.ex.repo.Port.find]"},
		{j, "com.ex.api.Api.plain", "[com.ex.repo.Port.find] [] asserted=[]"},
		{j, "com.ex.api.Api.both", "[com.ex.repo.Port.find] [] asserted=[]"},
		{j, "com.ex.api.Api.again", "[com.ex.repo.Port.find] [] asserted=[]"},
		{k, "com.ex.api.Api.asked", "[com.ex.repo.Port.find] [] asserted=[com.ex.repo.Port.find]"},
		{k, "com.ex.api.Api.paren", "[com.ex.repo.Port.find] [] asserted=[com.ex.repo.Port.find]"},
		{k, "com.ex.api.Api.plain", "[com.ex.repo.Port.find] [] asserted=[]"},
		{k, "com.ex.api.Api.both", "[com.ex.repo.Port.find] [] asserted=[]"},
		{k, "com.ex.api.Api.later", "[com.ex.repo.Port.find] [] asserted=[]"},
		{ts, "src/api.Api.asked", "[] [src/repo.Port.find] asserted=[src/repo.Port.find]"},
		{ts, "src/api.Api.angle", "[] [src/repo.Port.find] asserted=[src/repo.Port.find]"},
		{ts, "src/api.Api.plain", "[] [src/repo.Port.find] asserted=[]"},
		{ts, "src/api.Api.both", "[] [src/repo.Port.find] asserted=[]"},
		{ts, "src/api.Api.made", "[] [src/repo.Repo src/repo.Repo.find] asserted=[]"},
		{ts, "src/api.arrow", "[] [src/repo.Port.find] asserted=[src/repo.Port.find]"},
	} {
		if got := assertedOf(c.a, c.id); got != c.want {
			t.Errorf("%s:\n got  %s\n want %s", c.id, got, c.want)
		}
	}
}
