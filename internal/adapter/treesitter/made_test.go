//go:build cgo

package treesitter

import (
	"testing"
	"testing/fstest"
)

// viewpoint: behaviour
// target: java.trace, kotlin.trace
// Making one of the repository's classes is a call through it, and so are the methods called on a
// local variable declared without a type that holds one. An anonymous class of an interface, a
// library's class and a function that only looks like a class make nothing.
func TestJVMObjectsMadeInPlace(t *testing.T) {
	const java = "src/main/java/com/ex/"
	a := analyze(t, "java", fstest.MapFS{
		java + "repo/Repo.java": {Data: []byte("package com.ex.repo;\n\npublic class Repo {\n    public String find(String id) { return id; }\n}\n")},
		java + "repo/Port.java": {Data: []byte("package com.ex.repo;\n\npublic interface Port {\n    String find(String id);\n}\n")},
		java + "api/Api.java": {Data: []byte("package com.ex.api;\n\nimport com.ex.repo.Port;\nimport com.ex.repo.Repo;\nimport java.util.ArrayList;\n\n" +
			"public class Api {\n    public void make() { new Repo(); new ArrayList<String>(); }\n" +
			"    public String local(String id) { var r = new Repo(); var l = new ArrayList<String>(); l.add(id); return r.find(id); }\n" +
			"    public Port anon() { return new Port() { public String find(String id) { return id; } }; }\n}\n")},
	})
	cases := map[string]string{
		"com.ex.api.Api.make":  "through=[com.ex.repo.Repo] supertypes=[]",
		"com.ex.api.Api.local": "through=[com.ex.repo.Repo com.ex.repo.Repo.find] supertypes=[]",
		"com.ex.api.Api.anon":  "through=[] supertypes=[]",
	}
	for id, want := range cases {
		if got := callFacts(t, a, id); got != want {
			t.Errorf("%s:\n got  %s\n want %s", id, got, want)
		}
	}
	const kt = "src/main/kotlin/com/ex/"
	k := analyze(t, "kotlin", fstest.MapFS{
		kt + "repo/Repo.kt": {Data: []byte("package com.ex.repo\n\nclass Repo { fun find(id: String): String = id }\n\nfun Make(): Repo = Repo()\n")},
		kt + "api/Api.kt": {Data: []byte("package com.ex.api\n\nimport com.ex.repo.Make\nimport com.ex.repo.Repo\n\n" +
			"class Api {\n    fun local(id: String): String {\n        val r = Repo()\n        Make()\n        return r.find(id)\n    }\n}\n")},
	})
	if got := callFacts(t, k, "com.ex.api.Api.local"); got != "through=[com.ex.repo.Repo com.ex.repo.Repo.find] supertypes=[]" {
		t.Errorf("Kotlin Api.local: %s", got)
	}
}

// viewpoint: behaviour
// target: typescript.trace, python.trace
// In TypeScript and Python too, making one of the repository's classes is a call through it, and
// so are the methods called on what was just made, on a local variable holding it, and on a field
// or attribute set to it (private repo = new Repo(), self.repo = Repo()). A factory function's
// result is followed by the call of the factory itself, so it makes nothing here.
func TestScriptObjectsMadeInPlace(t *testing.T) {
	ts := analyze(t, "typescript", fstest.MapFS{
		"package.json":  {Data: []byte(`{"name":"app"}`)},
		"src/repo/r.ts": {Data: []byte("export class Repo { find(id: string): string { return id } }\nexport function makeRepo(): Repo { return new Repo() }\n")},
		"src/api/a.ts": {Data: []byte("import { Repo, makeRepo } from '../repo/r'\nimport * as repo from '../repo/r'\n\n" +
			"export class Api {\n  private made = new Repo()\n" +
			"  field(id: string): string { return this.made.find(id) }\n" +
			"  local(id: string): string { const r = new Repo(); return r.find(id) }\n" +
			"  inline(id: string): string { return new repo.Repo().find(id) }\n" +
			"  factory(id: string): string { return makeRepo().find(id) }\n}\n")},
	})
	py := analyze(t, "python", tree(
		"app/__init__.py", "",
		"app/repo.py", "class Repo:\n    def find(self, id):\n        return id\n\n\ndef make_repo():\n    return Repo()\n",
		"app/api.py", "from app import repo\nfrom app.repo import Repo, make_repo\n\n\nclass Api:\n    def __init__(self):\n        self.other = Repo()\n\n"+
			"    def field(self, id):\n        return self.other.find(id)\n\n"+
			"    def local(self, id):\n        r = Repo()\n        return r.find(id)\n\n"+
			"    def inline(self, id):\n        return repo.Repo().find(id)\n\n"+
			"    def factory(self, id):\n        return make_repo().find(id)\n",
	))
	cases := map[string]string{
		"src/api.Api.field":   "through=[src/repo.Repo.find] supertypes=[]",
		"src/api.Api.local":   "through=[src/repo.Repo src/repo.Repo.find] supertypes=[]",
		"src/api.Api.inline":  "through=[src/repo.Repo src/repo.Repo.find] supertypes=[]",
		"src/api.Api.factory": "through=[] supertypes=[]",
		"app/api.Api.field":   "through=[app/repo.Repo.find] supertypes=[]",
		"app/api.Api.local":   "through=[app/repo.Repo app/repo.Repo.find] supertypes=[]",
		"app/api.Api.inline":  "through=[app/repo.Repo app/repo.Repo.find] supertypes=[]",
		"app/api.Api.factory": "through=[] supertypes=[]",
	}
	for id, want := range cases {
		a := ts
		if id[:3] == "app" {
			a = py
		}
		if got := callFacts(t, a, id); got != want {
			t.Errorf("%s:\n got  %s\n want %s", id, got, want)
		}
	}
}
