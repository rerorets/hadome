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

import "os"

// Store keeps rows.
type Store struct{}

func CreatePost() { _ = os.WriteFile("post", nil, 0o600) }
func Address() {}
func Deleted() bool { return false }
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

// goCandidates analyses a Go module and returns its candidates' keys.
func goCandidates(t *testing.T, files map[string]string) string {
	t.Helper()
	tree := fstest.MapFS{"go.mod": {Data: []byte("module example.com/app\n")}}
	for p, src := range files {
		tree[p] = &fstest.MapFile{Data: []byte(src)}
	}
	a, err := adapter.New().Analyze(engine.Version{FS: tree}, config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, cd := range suggest.Candidates(engine.Input{Facts: a, Config: config.Config{}}) {
		got = append(got, cd.Key())
	}
	return strings.Join(got, "\n")
}

// viewpoint: behaviour
// target: suggest.Candidates
// In a repository with no entry point, a function is an idempotent candidate when its name says it
// creates and it reaches input or output: itself (a method of a *sql.DB), through a function it
// calls at any depth, or through a variable of a package that reaches input or output through
// what it imports. A function that only computes a value is not one, even in a unit that does
// input or output.
func TestIdempotentCandidatesReachInputOrOutput(t *testing.T) {
	got := goCandidates(t, map[string]string{
		"dates/dates.go": "package dates\n\nimport \"time\"\n\n" +
			"func AddDays(t time.Time, n int) time.Time { return t.AddDate(0, 0, n) }\n" +
			"func RegisterFormat(name string) string { return name }\n" +
			"func CreateRange(n int) []int { return make([]int, n) }\n",
		"members/members.go": "package members\n\nimport \"database/sql\"\n\n" +
			"func AddMember(db *sql.DB, name string) error { _, err := db.Exec(\"insert\", name); return err }\n" +
			"func RegisterDevice(db *sql.DB, id string) error { return insert(db, id) }\n" +
			"func CreateToken(id string) string { return \"t-\" + id }\n" +
			"func insert(db *sql.DB, id string) error { _, err := db.Exec(\"insert\", id); return err }\n",
		"env/env.go":           "package env\n\nimport \"os\"\n\nfunc Get(k string) string { return os.Getenv(k) }\n",
		"settings/settings.go": "package settings\n\nimport \"example.com/app/env\"\n\nvar Path = env.Get(\"SETTINGS\")\n",
		"members/chain.go": "package members\n\nimport \"database/sql\"\n\n" +
			"func AddChain(db *sql.DB) { step1(db) }\nfunc step1(db *sql.DB) { step2(db) }\n" +
			"func step2(db *sql.DB) { step3(db) }\nfunc step3(db *sql.DB) { _, _ = db.Exec(\"insert\") }\n",
		"members/settings.go": "package members\n\nimport \"example.com/app/settings\"\n\n" +
			"func AddSetting() string { return settings.Path }\n",
	})
	want := "idempotent members.AddChain\nidempotent members.AddMember\nidempotent members.AddSetting\nidempotent members.RegisterDevice"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// viewpoint: behaviour
// target: suggest.Candidates
// In a repository with entry points, an idempotent candidate must also be reached from one: a
// function registered for a path, or one a function literal registered for a path calls. A
// function that writes but no entry point reaches is not one, and neither is the function that
// only registers the routes.
func TestIdempotentCandidatesAreReachedFromAnEntryPoint(t *testing.T) {
	got := goCandidates(t, map[string]string{
		"store/store.go": "package store\n\nimport \"database/sql\"\n\n" +
			"type Store struct{ db *sql.DB }\n\n" +
			"func (s *Store) CreateOrder() error { _, err := s.db.Exec(\"insert\"); return err }\n" +
			"func (s *Store) AddAudit() error { _, err := s.db.Exec(\"insert\"); return err }\n" +
			"func (s *Store) InsertBackfill() error { _, err := s.db.Exec(\"insert\"); return err }\n",
		"web/web.go": "package web\n\nimport (\n\t\"net/http\"\n\n\t\"example.com/app/store\"\n)\n\n" +
			"type Handler struct{ store *store.Store }\n\n" +
			"func (h *Handler) Orders(w http.ResponseWriter, r *http.Request) { _ = h.store.CreateOrder() }\n\n" +
			"func Register(mux *http.ServeMux, h *Handler) {\n" +
			"\tmux.HandleFunc(\"/orders\", h.Orders)\n" +
			"\tmux.HandleFunc(\"/audit\", func(w http.ResponseWriter, r *http.Request) { _ = h.store.AddAudit() })\n}\n",
	})
	want := "idempotent store.Store.AddAudit\nidempotent store.Store.CreateOrder"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// viewpoint: behaviour
// target: suggest.Candidates
// Go's queue subscriptions and schedules are entry points: a function given to NATS's Subscribe or
// QueueSubscribe with a subject, or to robfig/cron's AddFunc with a schedule.
func TestGoQueueConsumersAndSchedulesAreEntryPoints(t *testing.T) {
	got := goCandidates(t, map[string]string{
		"store/store.go": "package store\n\nimport \"database/sql\"\n\nvar db *sql.DB\n\n" +
			"func CreateInvoice() { _, _ = db.Exec(\"insert\") }\n" +
			"func AddReminder() { _, _ = db.Exec(\"insert\") }\n" +
			"func InsertDigest() { _, _ = db.Exec(\"insert\") }\n" +
			"func CreateUnused() { _, _ = db.Exec(\"insert\") }\n",
		"jobs/jobs.go": "package jobs\n\nimport \"example.com/app/store\"\n\n" +
			"type conn interface{ Subscribe(string, func()) }\n\n" +
			"func Start(nc conn, q interface{ QueueSubscribe(string, string, func()) }, c interface{ AddFunc(string, func()) }) {\n" +
			"\tnc.Subscribe(\"invoices\", store.CreateInvoice)\n" +
			"\tq.QueueSubscribe(\"reminders\", \"workers\", store.AddReminder)\n" +
			"\tc.AddFunc(\"@daily\", func() { store.InsertDigest() })\n}\n",
	})
	want := "idempotent store.AddReminder\nidempotent store.CreateInvoice\nidempotent store.InsertDigest"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// viewpoint: behaviour
// target: suggest.Candidates
// Two candidates whose targets would read the same (store.DeleteRow in shops/store and in
// orders/store) are written with the shortest paths that tell them apart, so they stay two.
func TestCandidatesThatWouldReadTheSameAreToldApart(t *testing.T) {
	tree := fstest.MapFS{
		"go.mod":                {Data: []byte("module example.com/app\n")},
		"shops/store/store.go":  {Data: []byte("package store\n\nfunc DeleteRow(id string) {}\n")},
		"orders/store/store.go": {Data: []byte("package store\n\nfunc DeleteRow(id string) {}\n")},
	}
	a, err := adapter.New().Analyze(engine.Version{FS: tree}, config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, cd := range suggest.Candidates(engine.Input{Facts: a, Config: config.Config{}}) {
		got = append(got, cd.Key())
	}
	want := "erasable orders/store.DeleteRow\nerasable shops/store.DeleteRow"
	if strings.Join(got, "\n") != want {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), want)
	}
}
