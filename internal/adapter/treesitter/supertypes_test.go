//go:build cgo

package treesitter

import (
	"fmt"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/facts"
)

// callFacts writes a symbol's facts for the call diagram.
func callFacts(t *testing.T, a *facts.Analysis, id string) string {
	t.Helper()
	for _, s := range a.Symbols {
		if s.ID == id {
			return fmt.Sprintf("through=%v supertypes=%v", s.Through, s.Supertypes)
		}
	}
	t.Fatalf("no symbol %s", id)
	return ""
}

// viewpoint: behaviour
// target: java.trace
// A Java type's supertypes are the repository's types it names after extends and implements,
// generic or qualified, resolved by its imports and package; a library's type is left out.
func TestJavaSupertypes(t *testing.T) {
	const src = "src/main/java/com/ex/"
	a := analyze(t, "java", fstest.MapFS{
		src + "port/Finder.java": {Data: []byte("package com.ex.port;\n\npublic interface Finder<T> {}\n")},
		src + "port/Store.java":  {Data: []byte("package com.ex.port;\n\npublic interface Store extends Finder<String> {}\n")},
		src + "base/Base.java":   {Data: []byte("package com.ex.base;\n\npublic abstract class Base {}\n")},
		src + "svc/Svc.java": {Data: []byte("package com.ex.svc;\n\nimport com.ex.port.Store;\nimport java.io.Serializable;\n\n" +
			"public class Svc extends com.ex.base.Base implements Store, Serializable {}\n")},
	})
	if got := callFacts(t, a, "com.ex.port.Store"); got != "through=[] supertypes=[com.ex.port.Finder]" {
		t.Errorf("Store: %s", got)
	}
	if got := callFacts(t, a, "com.ex.svc.Svc"); got != "through=[] supertypes=[com.ex.port.Store]" {
		t.Errorf("Svc: %s", got)
	}
}

// viewpoint: behaviour
// target: kotlin.trace
// A Kotlin class's supertypes are the repository's types after the colon, called as a constructor
// or not, and delegated or not.
func TestKotlinSupertypes(t *testing.T) {
	const src = "src/main/kotlin/com/ex/"
	a := analyze(t, "kotlin", fstest.MapFS{
		src + "port/Port.kt": {Data: []byte("package com.ex.port\n\ninterface Finder\ninterface Saver\nopen class Base\n")},
		src + "svc/Svc.kt": {Data: []byte("package com.ex.svc\n\nimport com.ex.port.Base\nimport com.ex.port.Finder\nimport com.ex.port.Saver\n\n" +
			"class Svc(s: Saver) : Base(), Finder, Saver by s\n")},
	})
	if got := callFacts(t, a, "com.ex.svc.Svc"); got != "through=[] supertypes=[com.ex.port.Base com.ex.port.Finder com.ex.port.Saver]" {
		t.Errorf("Svc: %s", got)
	}
}

// viewpoint: behaviour
// target: typescript.trace
// TypeScript's calls through a value's declared type are the methods called on fields, constructor
// parameter properties, parameters and locals whose written type is one of the repository's types,
// interface or class, imported type only or not; an unannotated value and a library's type give
// none. Supertypes are what a class extends and implements, and what an interface extends.
func TestTypeScriptThroughAndSupertypes(t *testing.T) {
	a := analyze(t, "typescript", fstest.MapFS{
		"package.json":  {Data: []byte(`{"name":"app"}`)},
		"src/port/p.ts": {Data: []byte("export interface Finder { find(id: string): string }\nexport interface Store extends Finder {}\n")},
		"src/svc/s.ts": {Data: []byte("import type { Store } from '../port/p'\n\nexport class Base {}\n" +
			"export class Svc extends Base implements Store { find(id: string): string { return id } }\n")},
		"src/api/c.ts": {Data: []byte("import type { Finder, Store } from '../port/p'\nimport { Svc } from '../svc/s'\nimport { Logger } from 'pino'\n\n" +
			"export class Api {\n  private cache: Finder | null = null\n  constructor(private finder: Finder, svc: Svc, private log: Logger, private any, readonly owned: Svc) {}\n" +
			"  show(other: Store, plain): string {\n    const local: Svc = new Svc()\n    this.log.info('x')\n    plain.find('x')\n" +
			"    return this.finder.find('a') + this.cache!.find('b') + other.find('c') + local.find('d') + this.any.find('e') + this.owned.save()\n  }\n}\n")},
	})
	if got := callFacts(t, a, "src/api.Api.show"); got != "through=[src/port.Finder.find src/port.Store.find src/svc.Svc.find src/svc.Svc.save] supertypes=[]" {
		t.Errorf("Api.show: %s", got)
	}
	if got := callFacts(t, a, "src/svc.Svc"); got != "through=[] supertypes=[src/port.Store src/svc.Base]" {
		t.Errorf("Svc: %s", got)
	}
	if got := callFacts(t, a, "src/port.Store"); got != "through=[] supertypes=[src/port.Finder]" {
		t.Errorf("Store: %s", got)
	}
}
