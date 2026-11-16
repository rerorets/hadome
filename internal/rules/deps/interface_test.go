//go:build cgo

package deps

import (
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/adapter"
	"github.com/rerorets/hadome/internal/engine"
)

// interfaceRule is deps/call-not-in-diagram with the cases of calls through an interface. They are
// written in Java, which only a build with cgo reads, so they are not among the rule's self-test
// cases, which every build runs.
type interfaceRule struct{ callNotInDiagram }

func (interfaceRule) Cases() []engine.Case { return interfaceCases() }

// viewpoint: behaviour
// target: deps/call-not-in-diagram
// A call through an interface needs arrows to the units of its implementations in product code,
// and to no others.
func TestCallsThroughAnInterfaceNeedArrowsToTheImplementations(t *testing.T) {
	e := &engine.Engine{Analyzer: adapter.New(), Rules: []engine.Rule{interfaceRule{}}, Workdir: "."}
	for _, f := range e.SelfTest() {
		t.Errorf("%s: %s", f.Name, f.Err)
	}
}

// javaCalls is a Java project with a call diagram: the controller declares Finder and holds one,
// and the service implements it. Extra files are keyed by path.
func javaCalls(diagram string, extra ...string) fstest.MapFS {
	const src = "src/main/java/com/ex/"
	t := fstest.MapFS{
		".hadome.yml":                  {Data: []byte("schema: 1\nrules:\n  deps:\n    calls: docs/calls.md\n")},
		"docs/calls.md":                {Data: []byte("```mermaid\nflowchart LR\n  c[\"" + src + "controller\"]\n  s[\"" + src + "service\"]\n  r[\"" + src + "repo\"]\n" + diagram + "\n```\n")},
		src + "controller/Finder.java": {Data: []byte("package com.ex.controller;\n\npublic interface Finder {\n    String find(String id);\n}\n")},
		src + "controller/UserController.java": {Data: []byte("package com.ex.controller;\n\npublic class UserController {\n" +
			"    private final Finder finder;\n    public UserController(Finder finder) { this.finder = finder; }\n}\n")},
		src + "service/UserService.java": {Data: []byte("package com.ex.service;\n\nimport com.ex.controller.Finder;\n\n" +
			"public class UserService implements Finder {\n    public String find(String id) { return id; }\n}\n")},
	}
	for i := 0; i+1 < len(extra); i += 2 {
		t[extra[i]] = &fstest.MapFile{Data: []byte(extra[i+1])}
	}
	return t
}

func interfaceCases() []engine.Case {
	const (
		controller = "src/main/java/com/ex/controller/UserController.java"
		calling    = "package com.ex.controller;\n\npublic class UserController {\n    private final Finder finder;\n" +
			"    public UserController(Finder finder) { this.finder = finder; }\n    public String show(String id) { return finder.find(id); }\n}\n"
		memory = "package com.ex.repo;\n\nimport com.ex.controller.Finder;\n\n" +
			"public class MemoryFinder implements Finder {\n    public String find(String id) { return \"\"; }\n}\n"
		fake = "package com.ex.controller;\n\npublic class FakeFinder implements Finder {\n    public String find(String id) { return \"\"; }\n}\n"
	)
	toService := "  c --> s"
	return []engine.Case{
		{Name: "a call through an interface needs an arrow to the implementation's unit", Base: javaCalls(""),
			Head: javaCalls("", controller, calling), WantFail: []string{"deps/call-not-in-diagram"}},
		{Name: "a call through an interface along an arrow to the implementation passes", Base: javaCalls(toService),
			Head: javaCalls(toService, controller, calling)},
		{Name: "with implementations in two units, an arrow to one alone fails", Base: javaCalls(toService),
			Head:     javaCalls(toService, controller, calling, "src/main/java/com/ex/repo/MemoryFinder.java", memory),
			WantFail: []string{"deps/call-not-in-diagram"}},
		{Name: "an implementation in a test file does not count", Base: javaCalls(toService),
			Head: javaCalls(toService, controller, calling, "src/test/java/com/ex/repo/MemoryFinder.java", memory)},
		{Name: "an implementation in the caller's own unit passes", Base: javaCalls(toService),
			Head: javaCalls(toService, controller, calling, "src/main/java/com/ex/controller/FakeFinder.java", fake)},
	}
}
