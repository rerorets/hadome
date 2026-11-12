//go:build cgo

package treesitter

import (
	"fmt"
	"testing"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/rerorets/hadome/internal/adapter/reuse"
)

// copySamples are files of every grammar. Each has fields, anonymous nodes, nesting, and a syntax
// error, so that error nodes and missing nodes are copied too.
var copySamples = []struct{ lang, path, src string }{
	{"typescript", "src/store.ts", `import { a } from './a'
export function append(x: number): number {
  const y = <number>x
  if (y > 1 && a(y)) { return y } else { return 0 }
}
class Store { private rows: string[] = []; get size() { return this.rows.length } }
const broken = (
`},
	{"typescript", "src/view.tsx", `export const View = ({ name }: { name: string }) => (
  <div className="v">{name ?? "none"} &amp; <b>{1 + 2}</b></div>
)
function f(x) { return x +; }
`},
	{"python", "app/store.py", `import os
from . import a

class Store:
    """Keeps rows."""
    def append(self, x: int) -> int:
        if x > 1 and a.ok(x):
            return [y for y in range(x) if y]
        return {"k": x, **os.environ}

def broken(:
    pass
`},
	{"rust", "src/store.rs", `use crate::a::b;
pub struct Store { rows: Vec<String> }
impl Store {
    pub fn append(&mut self, x: i32) -> i32 {
        match x { 0 => 1, n if n > 2 => n * 2, _ => { println!("{}", x); 0 } }
    }
}
fn broken( { }
`},
	{"java", "src/main/java/acme/Store.java", `package acme;
import java.util.List;
public class Store {
    private final List<String> rows = new java.util.ArrayList<>();
    /** Appends. */
    public int append(int x) { return x > 1 ? x : switch (x) { case 0 -> 1; default -> 2; }; }
    void broken( { }
}
`},
	{"kotlin", "src/main/kotlin/acme/Store.kt", `package acme
import acme.a.B
class Store(private val rows: MutableList<String> = mutableListOf()) {
    fun append(x: Int): Int = when { x > 1 -> x; else -> rows.size }
    val size get() = rows.size
}
fun broken( { }
`},
}

// langOf returns the language named name.
func langOf(t *testing.T, name string) *lang {
	t.Helper()
	for _, l := range Languages() {
		if l.name == name {
			return l
		}
	}
	t.Fatalf("no language %s", name)
	return nil
}

// viewpoint: invariant
// target: treesitter.parseWith
// A copied tree is tree-sitter's visible tree: walking a fresh C tree of the same file node by node,
// every node has the copy's kind, namedness, field, byte range, start and end, children, parent and
// siblings, in the same order. The copy is read after parseWith returned, when its C trees are
// closed.
func TestACopyIsTheVisibleTree(t *testing.T) {
	for _, s := range copySamples {
		t.Run(s.path, func(t *testing.T) {
			l := langOf(t, s.lang)
			parser := sitter.NewParser()
			defer parser.Close()
			pr, err := parseWith(l.r, parser, s.path, []byte(s.src))
			if err != nil {
				t.Fatal(err)
			}
			if len(pr.lifted) > 0 {
				t.Fatalf("the sample was repaired; pick one the grammar reads as it is")
			}
			if err := parser.SetLanguage(l.r.grammar(s.path)); err != nil {
				t.Fatal(err)
			}
			tree := parser.Parse([]byte(s.src), nil)
			defer tree.Close()
			want := tree.RootNode()
			if !want.HasError() {
				t.Fatalf("the sample has no syntax error; it must, to cover error nodes")
			}
			seen := compareCopy(t, want, "", pr.root, nil)
			if seen != int(want.DescendantCount()) {
				t.Errorf("compared %d nodes, the tree has %d", seen, want.DescendantCount())
			}
		})
	}
}

// compareCopy compares the copy got with the C node want, which is in field field of its parent, and
// their children; parent is the copy's parent as reached from the root. It returns the nodes it
// compared.
func compareCopy(t *testing.T, want *sitter.Node, field string, got, parent *node) int {
	t.Helper()
	at := fmt.Sprintf("%s at %d-%d", want.Kind(), want.StartByte(), want.EndByte())
	if got == nil {
		t.Errorf("%s: no copy", at)
		return 0
	}
	check := func(what string, g, w any) {
		if g != w {
			t.Errorf("%s: %s = %v, want %v", at, what, g, w)
		}
	}
	check("kind", got.Kind(), want.Kind())
	check("named", got.IsNamed(), want.IsNamed())
	check("field", fieldOf(got), field)
	check("start byte", got.StartByte(), want.StartByte())
	check("end byte", got.EndByte(), want.EndByte())
	check("start", got.StartPosition(), point{Row: want.StartPosition().Row, Column: want.StartPosition().Column})
	check("end", got.EndPosition(), point{Row: want.EndPosition().Row, Column: want.EndPosition().Column})
	check("child count", got.ChildCount(), want.ChildCount())
	check("named child count", got.NamedChildCount(), want.NamedChildCount())
	check("parent", got.Parent(), parent)
	seen := 1
	var prev *node
	for i := uint(0); i < want.ChildCount(); i++ {
		c := got.Child(i)
		if c == nil {
			t.Errorf("%s: no child %d", at, i)
			break
		}
		if c.PrevSibling() != prev {
			t.Errorf("%s: child %d's previous sibling is not child %d", at, i, int(i)-1)
		}
		if prev != nil && prev.NextSibling() != c {
			t.Errorf("%s: child %d's next sibling is not child %d", at, int(i)-1, i)
		}
		if name := want.FieldNameForChild(uint32(i)); name != "" {
			check("child by field "+name, got.ChildByFieldName(name) != nil, true)
			check("field of child", got.FieldNameForChild(uint32(i)), name)
		}
		seen += compareCopy(t, want.Child(i), want.FieldNameForChild(uint32(i)), c, got)
		prev = c
	}
	if prev != nil && prev.NextSibling() != nil {
		t.Errorf("%s: the last child has a next sibling", at)
	}
	for i := uint(0); i < want.NamedChildCount(); i++ {
		w, g := want.NamedChild(i), got.NamedChild(i)
		if g == nil || g.StartByte() != w.StartByte() || g.EndByte() != w.EndByte() || g.Kind() != w.Kind() {
			t.Errorf("%s: named child %d differs", at, i)
		}
	}
	for _, name := range []string{"name", "body", "value", "left", "right", "function", "arguments", "type"} {
		w, g := want.ChildByFieldName(name), got.ChildByFieldName(name)
		if (w == nil) != (g == nil) || (w != nil && (g.StartByte() != w.StartByte() || g.Kind() != w.Kind())) {
			t.Errorf("%s: child by field %s differs", at, name)
		}
	}
	return seen
}

// fieldOf returns the field n is in, as its parent names it.
func fieldOf(n *node) string {
	if n.Parent() == nil {
		return ""
	}
	for i := uint(0); i < n.Parent().ChildCount(); i++ {
		if n.Parent().Child(i) == n {
			return n.Parent().FieldNameForChild(uint32(i))
		}
	}
	return ""
}

// viewpoint: invariant
// target: treesitter.node.Id
// A node's ID is its identity: the same node reached by two paths has one ID, two nodes of a copy
// never share one, and the nodes a repair took out of an earlier copy share none with the copy read
// again.
func TestANodesIDIsItsIdentity(t *testing.T) {
	l := langOf(t, "kotlin")
	parser := sitter.NewParser()
	defer parser.Close()
	pr, err := parseWith(l.r, parser, "src/main/kotlin/acme/Store.kt", []byte(`package acme

@Suppress("unused")
@JvmName("y")
fun helper(): Int = 1

class Store { fun append(x: Int): Int = x + 1 }
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(pr.lifted) == 0 {
		t.Fatal("the sample was not repaired; it must be, to have lifted nodes")
	}
	ids := map[uintptr]bool{}
	nodes := 0
	walk(pr.root, func(n *node) bool {
		nodes++
		ids[n.Id()] = true
		if p := n.Parent(); p != nil {
			if !sameNode(p, n) {
				t.Errorf("%s at %d: its parent's child at its place has another ID", n.Kind(), n.StartByte())
			}
		}
		return true
	})
	if len(ids) != nodes {
		t.Errorf("%d nodes have %d IDs", nodes, len(ids))
	}
	for _, x := range pr.lifted {
		walk(x, func(n *node) bool {
			if ids[n.Id()] {
				t.Errorf("lifted %s at %d has the ID of a node of the copy read again", n.Kind(), n.StartByte())
			}
			return true
		})
	}
}

// sameNode reports whether p has a child with n's ID.
func sameNode(p, n *node) bool {
	for i := uint(0); i < p.ChildCount(); i++ {
		if p.Child(i).Id() == n.Id() {
			return true
		}
	}
	return false
}

// viewpoint: invariant
// target: treesitter.parseWith
// What parseWith returns holds no C tree: the reuse session finds nothing to release in it.
func TestAParsedFileHoldsNoCTree(t *testing.T) {
	l := langOf(t, "python")
	parser := sitter.NewParser()
	defer parser.Close()
	pr, err := parseWith(l.r, parser, "a.py", []byte("x = 1\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := any(pr).(reuse.Releaser); ok {
		t.Error("a parsed file has something to release")
	}
}

// viewpoint: invariant
// target: treesitter.nodeNames
// Every grammar a language reads its files with has names for its kinds and fields, so every node
// of every file gets its kind.
func TestEveryGrammarHasNames(t *testing.T) {
	for _, s := range copySamples {
		g := langOf(t, s.lang).r.grammar(s.path)
		if len(nodeNames.kinds[g]) == 0 || len(nodeNames.fields[g]) == 0 {
			t.Errorf("%s: the grammar of %s has no names", s.lang, s.path)
		}
	}
	for _, p := range []string{"a.mts", "a.cts", "a.js", "a.jsx", "a.mjs", "a.cjs"} {
		if len(nodeNames.kinds[langOf(t, "typescript").r.grammar(p)]) == 0 {
			t.Errorf("typescript: the grammar of %s has no names", p)
		}
	}
}
