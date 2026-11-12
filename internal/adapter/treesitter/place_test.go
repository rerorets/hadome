//go:build cgo

package treesitter

import (
	"testing"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

// viewpoint: behaviour
// target: treesitter.place.index
// A node's index is the one its place holds when the walk knew it, else its position among its
// parent's children; a node outside the parent, or one at the root, has none.
func TestAPlaceGivesTheIndexOfItsNode(t *testing.T) {
	var py *lang
	for _, l := range Languages() {
		if l.name == "python" {
			py = l
		}
	}
	parser := sitter.NewParser()
	defer parser.Close()
	f, err := py.parse(parser, "a.py", []byte("x = f(a, b)\n"))
	if err != nil {
		t.Fatal(err)
	}
	args := f.root.NamedChild(0).NamedChild(0).ChildByFieldName("right").ChildByFieldName("arguments")
	b := args.Child(3) // ( a , b )
	cases := []struct {
		name string
		at   place
		n    *node
		want int
	}{
		{"known", place{parent: args, i: 3}, b, 3},
		{"searched", place{parent: args, i: -1}, b, 3},
		{"searched for the first child", place{parent: args, i: -1}, args.Child(0), 0},
		{"not among the children", place{parent: args, i: -1}, args, -1},
		{"the root", placeOf(f.root), f.root, -1},
	}
	for _, tc := range cases {
		if got := tc.at.index(tc.n); got != tc.want {
			t.Errorf("%s: index %d, want %d", tc.name, got, tc.want)
		}
	}
	if at := placeOf(b); at.parent == nil || at.parent.Id() != args.Id() || at.i != -1 {
		t.Errorf("placeOf(b) = %+v, want the argument list with no index", at)
	}
}
