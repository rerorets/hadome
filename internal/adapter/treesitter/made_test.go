// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

//go:build cgo

package treesitter

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/facts"
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
			"export class Api {\n  private made = new Repo()\n  private kept: Repo\n" +
			"  keptField(id: string): string { return this.kept.find(id) }\n" +
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
		"src/api.Api.field":     "through=[src/repo.Repo.find] supertypes=[]",
		"src/api.Api.keptField": "through=[src/repo.Repo.find] supertypes=[]",
		"src/api.Api.local":     "through=[src/repo.Repo src/repo.Repo.find] supertypes=[]",
		"src/api.Api.inline":    "through=[src/repo.Repo src/repo.Repo.find] supertypes=[]",
		"src/api.Api.factory":   "through=[] supertypes=[]",
		"app/api.Api.field":     "through=[app/repo.Repo.find] supertypes=[]",
		"app/api.Api.local":     "through=[app/repo.Repo app/repo.Repo.find] supertypes=[]",
		"app/api.Api.inline":    "through=[app/repo.Repo app/repo.Repo.find] supertypes=[]",
		"app/api.Api.factory":   "through=[] supertypes=[]",
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

// viewpoint: behaviour
// target: python.trace, typescript.trace
// Calls hadome cannot follow are the method calls on values from outside whose type is not written:
// parameters without an annotation (or typed any), attributes and fields set from one, values at the
// top of the module made by the repository's functions, and values imported from its modules. Locals,
// self, a library's values, values of a written type and objects made in place are not counted.
func TestUnfollowedCallsAreCountedOnValuesFromOutside(t *testing.T) {
	py := analyze(t, "python", tree(
		"app/__init__.py", "",
		"app/repo.py", "class Repo:\n    def find(self, id):\n        return id\n\n\ndef make_repo():\n    return Repo()\n\n\nshared = make_repo()\n",
		"app/api.py", "import logging\nfrom app.repo import Repo, make_repo, shared\n\nmade = Repo()\nbuilt = make_repo()\nlog = logging.getLogger()\n\n\n"+
			"class Api:\n    def __init__(self, repo, typed: Repo):\n        self.repo = repo\n        self.typed = typed\n        self.items = []\n\n"+
			"    def injected(self, id):\n        return self.repo.find(id)\n\n"+
			"    def param(self, id, other=None):\n        return other.find(id)\n\n"+
			"    def imported(self, id):\n        return shared.find(id)\n\n"+
			"    def module(self, id):\n        return built.find(id)\n\n"+
			"    def known(self, id):\n        self.injected(id)\n        log.info(id)\n        self.items.append(id)\n        self.typed.find(id)\n        made.find(id)\n"+
			"        local = make_repo()\n        local.find(id)\n        built = 1\n        return built.find(id)\n",
	))
	ts := analyze(t, "typescript", fstest.MapFS{
		"package.json": {Data: []byte(`{"name":"app"}`)},
		"src/repo.ts":  {Data: []byte("export class Repo { find(id: string): string { return id } }\nexport function makeRepo(): Repo { return new Repo() }\nexport const shared = makeRepo()\n")},
		"src/api.ts": {Data: []byte("import pino from 'pino'\nimport { Repo, makeRepo, shared } from './repo'\n\n" +
			"const made = new Repo()\nconst built = makeRepo()\nconst log = pino()\nconst loose: any = null\n\n" +
			"export class Api {\n  constructor(private repo, private typed: Repo) {}\n" +
			"  injected(id: string) { return this.repo.find(id) }\n" +
			"  param(id: string, other, anything: any) { return other.find(id) + anything.find(id) }\n" +
			"  imported(id: string) { return shared.find(id) }\n" +
			"  module(id: string) { return built.find(id) + loose.find(id) }\n" +
			"  known(id: string) {\n    const items: string[] = []\n    items.push(id)\n    log.info(id)\n    this.typed.find(id)\n    made.find(id)\n" +
			"    const built = new Repo()\n    return built.find(id)\n  }\n}\n")},
	})
	cases := map[string]int{
		"app/api.Api.injected": 1, "app/api.Api.param": 1, "app/api.Api.imported": 1, "app/api.Api.module": 1, "app/api.Api.known": 0,
		"src.Api.injected": 1, "src.Api.param": 2, "src.Api.imported": 1, "src.Api.module": 2, "src.Api.known": 0,
	}
	for id, want := range cases {
		a := ts
		if strings.HasPrefix(id, "app/") {
			a = py
		}
		got := -1
		for _, s := range a.Symbols {
			if s.ID == id {
				got = s.Unfollowed
			}
		}
		if got != want {
			t.Errorf("%s: unfollowed %d, want %d", id, got, want)
		}
	}
}

// viewpoint: behaviour
// target: python.trace, typescript.trace
// A value imported from another module, made there from one of the repository's classes or written
// with one as its type, is a value of that class: its method calls are calls of the class's
// methods, not calls hadome cannot follow. Through a package that imports it, it is the same value.
func TestValuesMadeInAnotherModuleAreFollowed(t *testing.T) {
	py := analyze(t, "python", tree(
		"app/__init__.py", "",
		"app/mgr.py", "class Manager:\n    def switch(self):\n        pass\n",
		"app/state/__init__.py", "from app.state.values import manager\n",
		"app/state/values.py", "from app.mgr import Manager\n\nmanager = Manager()\n",
		"app/api.py", "from app.state.values import manager\nfrom app.state import manager as again\n\n\n"+
			"def direct():\n    manager.switch()\n\n\ndef through():\n    again.switch()\n",
	))
	ts := analyze(t, "typescript", fstest.MapFS{
		"package.json":       {Data: []byte(`{"name":"app"}`)},
		"src/svc/api.ts":     {Data: []byte("export class Api { get(): void {} post(): void {} }\n")},
		"src/state/index.ts": {Data: []byte("import { Api } from '../svc/api'\n\nexport const api = new Api()\nexport const typed: Api = make()\n\nfunction make(): Api { return new Api() }\n")},
		"src/routes/r.ts":    {Data: []byte("import { api, typed } from '../state'\n\nexport function one(): void {\n  api.get()\n}\n\nexport function two(): void {\n  typed.post()\n}\n")},
	})
	for _, c := range []struct {
		a     *facts.Analysis
		id    string
		facts string
	}{
		{py, "app/api.direct", "through=[app/mgr.Manager.switch] supertypes=[]"},
		{py, "app/api.through", "through=[app/mgr.Manager.switch] supertypes=[]"},
		{ts, "src/routes.one", "through=[src/svc.Api.get] supertypes=[]"},
		{ts, "src/routes.two", "through=[src/svc.Api.post] supertypes=[]"},
	} {
		if got := callFacts(t, c.a, c.id); got != c.facts {
			t.Errorf("%s: %s, want %s", c.id, got, c.facts)
		}
		for _, s := range c.a.Symbols {
			if s.ID == c.id && s.Unfollowed != 0 {
				t.Errorf("%s: unfollowed %d, want 0", c.id, s.Unfollowed)
			}
		}
	}
}

// viewpoint: behaviour
// target: python.trace
// A package imported as a module reaches the names its __init__.py imports: models.Account() makes
// the Account the package imports and tasks.run() calls the run it imports, and a call on the
// package is not a call hadome cannot follow.
func TestPythonPackageMembersAreWhatThePackageImports(t *testing.T) {
	a := analyze(t, "python", tree(
		"app/__init__.py", "",
		"app/models/__init__.py", "from app.models.account import Account\n",
		"app/models/account.py", "class Account:\n    pass\n",
		"app/tasks/__init__.py", "from app.tasks.impl import run\n",
		"app/tasks/impl.py", "def run():\n    pass\n",
		"app/api.py", "from app import models, tasks\n\n\ndef create():\n    models.Account()\n\n\ndef start():\n    tasks.run()\n",
	))
	if got := callFacts(t, a, "app/api.create"); got != "through=[app/models/account.Account] supertypes=[]" {
		t.Errorf("create: %s", got)
	}
	if got := reachOf(t, a, "app/api.start"); got != "io=false calls=[app/tasks/impl.run] units=[]" {
		t.Errorf("start: %s", got)
	}
	for _, s := range a.Symbols {
		if (s.ID == "app/api.create" || s.ID == "app/api.start") && s.Unfollowed != 0 {
			t.Errorf("%s: unfollowed %d, want 0", s.ID, s.Unfollowed)
		}
	}
}
