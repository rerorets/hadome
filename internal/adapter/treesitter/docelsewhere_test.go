//go:build cgo

package treesitter

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// elsewhere lists the symbols of one language with whether convention documents them elsewhere.
func elsewhere(t *testing.T, lang string, files ...string) string {
	t.Helper()
	var out []string
	for _, s := range analyze(t, lang, tree(files...)).Symbols {
		out = append(out, fmt.Sprintf("%s elsewhere=%t", s.ID, s.DocElsewhere))
	}
	sort.Strings(out)
	return strings.Join(out, "\n")
}

// viewpoint: behaviour
// target: the Kotlin, Java and TypeScript readers
// An override is documented at the declaration it overrides, and a TypeScript constructor at its
// class; a method that merely shares a name with one is not.
func TestOverridesAreDocumentedElsewhere(t *testing.T) {
	expect(t, elsewhere(t, "kotlin", "src/main/kotlin/acme/store/Rows.kt", `package acme.store

class Rows : Base() {
    override fun toString(): String = "rows"
    fun size(): Int = 0
}
`), `
acme.store.Rows elsewhere=false
acme.store.Rows.size elsewhere=false
acme.store.Rows.toString elsewhere=true`)

	expect(t, elsewhere(t, "java", "src/main/java/acme/store/Rows.java", `package acme.store;

public class Rows {
    public Rows() {}

    @Override
    public String toString() { return "rows"; }

    public int size() { return 0; }
}
`), `
acme.store.Rows elsewhere=false
acme.store.Rows.Rows elsewhere=false
acme.store.Rows.size elsewhere=false
acme.store.Rows.toString elsewhere=true`)

	expect(t, elsewhere(t, "typescript", "src/store/rows.ts", `export class Rows extends Base {
  constructor(private n: number) { super() }
  override describe(): string { return "rows" }
  size(): number { return this.n }
}
`), `
src/store.Rows elsewhere=false
src/store.Rows.constructor elsewhere=true
src/store.Rows.describe elsewhere=true
src/store.Rows.size elsewhere=false`)
}
