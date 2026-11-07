package suggest_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/adapter"
	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/suggest"
)

const api = `package api

// Store keeps rows.
type Store struct{}

func CreatePost() {}
func Address() {}
func createDraft() {}
func DeleteRow(id string) {}
func (s *Store) ExportRows() []string { return nil }
func (s *Store) ImportRows(rows []string) {}
func ExportAll() {}
func GetRows(userID string) []string { return nil }
func GetConfig(name string) string { return "" }

type cache struct{}

func (c *cache) CreateEntry() {}

// CreateRequest is an interface, not an operation.
type CreateRequest interface{ Body() string }
`

func candidates(t *testing.T, yml string) []string {
	t.Helper()
	tree := fstest.MapFS{
		"go.mod":             {Data: []byte("module example.com/app\n")},
		"api/api.go":         {Data: []byte(api)},
		"api/helper_test.go": {Data: []byte("package api\n\nfunc CreateFixture() {}\n")},
	}
	if yml != "" {
		tree[".hadome.yml"] = &fstest.MapFile{Data: []byte(yml)}
	}
	c, err := config.Load(tree, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a, err := adapter.New().Analyze(engine.Version{FS: tree}, c)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, cd := range suggest.Candidates(engine.Input{Facts: a, Config: c}) {
		out = append(out, cd.Key())
	}
	return out
}

// viewpoint: behaviour
// target: suggest.Candidates
// Candidates come from exported names and scope parameters of non-test code, written as
// configuration writes targets.
func TestCandidatesFollowNamesAndScopes(t *testing.T) {
	got := strings.Join(candidates(t, ""), "\n")
	want := strings.Join([]string{
		"erasable api.DeleteRow",
		"idempotent api.CreatePost",
		"isolated api.GetRows",
		"round-trip api.Store.ExportRows,api.Store.ImportRows",
	}, "\n")
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// viewpoint: behaviour
// target: suggest.Candidates
// A candidate the configuration already names, however it writes the target, is not suggested again.
func TestConfiguredInvariantsAreNotSuggested(t *testing.T) {
	got := candidates(t, "schema: 1\ninvariants:\n  - type: idempotent\n    target: example.com/app/api.CreatePost\n"+
		"  - type: round-trip\n    targets: [api.Store.ImportRows, api.Store.ExportRows]\n")
	for _, c := range got {
		if strings.HasPrefix(c, "idempotent") || strings.HasPrefix(c, "round-trip") {
			t.Errorf("configured invariant suggested again: %s", c)
		}
	}
	if len(got) != 2 {
		t.Errorf("the others remain: %v", got)
	}
}

// viewpoint: behaviour
// target: suggest.Candidates
// Add and Register suggest idempotent only in a unit that does input or output, since they also
// name functions that only compute a value; Create needs no such unit.
func TestAddAndRegisterNeedAUnitThatDoesIO(t *testing.T) {
	tree := fstest.MapFS{
		"go.mod": {Data: []byte("module example.com/app\n")},
		"dates/dates.go": {Data: []byte("package dates\n\nimport \"time\"\n\n" +
			"func AddDays(t time.Time, n int) time.Time { return t.AddDate(0, 0, n) }\n" +
			"func RegisterFormat(name string) string { return name }\n" +
			"func CreateRange(n int) []int { return make([]int, n) }\n")},
		"members/members.go": {Data: []byte("package members\n\nimport \"database/sql\"\n\n" +
			"func AddMember(db *sql.DB, name string) error { return nil }\n" +
			"func RegisterDevice(db *sql.DB, id string) error { return nil }\n")},
	}
	a, err := adapter.New().Analyze(engine.Version{FS: tree}, config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, cd := range suggest.Candidates(engine.Input{Facts: a, Config: config.Config{}}) {
		got = append(got, cd.Key())
	}
	want := "idempotent dates.CreateRange\nidempotent members.AddMember\nidempotent members.RegisterDevice"
	if strings.Join(got, "\n") != want {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), want)
	}
}
