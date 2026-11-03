//go:build cgo

package treesitter

import "testing"

// viewpoint: behaviour
// target: the Java reader
// The facts rules see from a Maven-style project: packages as units, imports of the project's own
// and of other packages, public and documented members, and JUnit tests with their skips, mocks
// and calls.
func TestJavaFacts(t *testing.T) {
	a := analyze(t, "java", tree(
		"src/main/java/com/acme/store/Store.java", `package com.acme.store;

import java.sql.Connection;
import com.acme.util.Strings;

/** Store keeps rows. */
public class Store {
  /** Adds a row. */
  public void add(String row) { if (row != null && !row.isEmpty()) { rows.add(row); } }
  void hidden() {}
  public static Store create(String tenantId, int size) { switch (size) { case 1: return null; default: return new Store(); } }
}

interface Repo {
  void get();
  void put();
}
`,
		"src/main/java/com/acme/util/Strings.java", "package com.acme.util;\n\npublic class Strings {}\n",
		"src/test/java/com/acme/store/StoreTest.java", `package com.acme.store;

import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Disabled;
import org.mockito.Mockito;
import static org.junit.jupiter.api.Assertions.assertTrue;

class StoreTest {
  @Test
  void createsTwice() throws Exception {
    Store.create("t1", 1);
    Store.create("t2", 1);
    Store s = new Store();
    s.add("x");
    Thread.sleep(10);
    assertTrue(true);
  }

  @Disabled("needs a database")
  @Test
  void later() {
    var s = new Store();
    for (int i = 0; i < 2; i++) { s.add("y"); }
    Thing thing = make();
    thing.run();
  }

  Store helper() { return Store.create("t1", 2); }
}
`))
	expect(t, summary(a), `
file src/main/java/com/acme/store/Store.java source unit=com.acme.store at=src/main/java/com/acme/store
file src/main/java/com/acme/util/Strings.java source unit=com.acme.util at=src/main/java/com/acme/util
file src/test/java/com/acme/store/StoreTest.java test unit=com.acme.store at=src/test/java/com/acme/store
import com.acme.store -> com.acme.util internal=true io=false
import com.acme.store -> java.sql internal=false io=true
symbol interface com.acme.store.Repo exported=false doc=false params=0 cx=0 methods=2 scoped=false
symbol method com.acme.store.Repo.get exported=false doc=false params=0 cx=1 methods=0 scoped=false
symbol method com.acme.store.Repo.put exported=false doc=false params=0 cx=1 methods=0 scoped=false
symbol method com.acme.store.Store.add exported=true doc=true params=1 cx=3 methods=0 scoped=false
symbol method com.acme.store.Store.create exported=true doc=false params=2 cx=2 methods=0 scoped=true
symbol method com.acme.store.Store.hidden exported=false doc=false params=0 cx=1 methods=0 scoped=false
symbol type com.acme.store.Store exported=true doc=true params=0 cx=0 methods=0 scoped=false
symbol type com.acme.util.Strings exported=true doc=false params=0 cx=0 methods=0 scoped=false
test "StoreTest.createsTwice" calls=[com.acme.store.Store×1/0 com.acme.store.Store.add×1/1 com.acme.store.Store.create×2/2] hazards=[mock sleep always-true] skip=false:"" unresolved=0
test "StoreTest.later" calls=[com.acme.store.Store×1/0 com.acme.store.Store.add×2/1] hazards=[mock] skip=true:"needs a database" unresolved=1
`)
}
