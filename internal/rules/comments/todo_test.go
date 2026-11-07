package comments_test

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/adapter"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/rules/comments"
)

type trees struct{ base, head fs.FS }

func (s trees) Base() (engine.Version, error)       { return engine.Version{Label: "base", FS: s.base}, nil }
func (s trees) Head() (engine.Version, error)       { return engine.Version{Label: "head", FS: s.head}, nil }
func (s trees) Renames() (map[string]string, error) { return nil, nil }

// viewpoint: external contract
// target: comments/todo-format through engine.Engine.Check
// The fix's example follows the configured form: an agent copies it, so an example of another form
// would fail the same rule again. A form no common example matches gets no example at all.
func TestTodoFixExampleFollowsTheConfiguredForm(t *testing.T) {
	for _, tc := range []struct {
		name, form, example string
	}{
		{"the default form", "", "TODO(stage 2): handle errors"},
		{"an issue-number form", `^TODO\(#[0-9]+\): \S`, "TODO(#123): handle errors"},
		{"a form no example matches", `^TODO\[[a-z]+\] \S`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conf := "schema: 1\n"
			if tc.form != "" {
				conf += "rules:\n  comments:\n    todo: '" + tc.form + "'\n"
			}
			tree := func(body string) fstest.MapFS {
				return fstest.MapFS{
					"go.mod":         {Data: []byte("module example.com/app\n")},
					".hadome.yml":    {Data: []byte(conf)},
					"store/store.go": {Data: []byte("// Package store keeps rows.\npackage store\n" + body)},
				}
			}
			e := &engine.Engine{Analyzer: adapter.New(), Rules: comments.All()}
			r, err := e.Check(trees{base: tree(""), head: tree("\n// TODO handle errors\n")})
			if err != nil {
				t.Fatal(err)
			}
			if len(r.Regressions) != 1 {
				t.Fatalf("want one regression, got %+v", r.Regressions)
			}
			fix := r.Regressions[0].Fix.In("en")
			if tc.example == "" {
				if strings.Contains(fix, "for example") {
					t.Errorf("fix %q shows an example the form does not accept", fix)
				}
				return
			}
			if !strings.Contains(fix, "(for example "+tc.example+")") {
				t.Errorf("fix %q does not show %q", fix, tc.example)
			}
			form := tc.form
			if form == "" {
				form = `^TODO\([^)]+\): \S`
			}
			if !regexp.MustCompile(form).MatchString(tc.example) {
				t.Fatalf("the test's own example %q does not match %s", tc.example, form)
			}
		})
	}
}
