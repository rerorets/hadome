package gate_test

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/adapter"
	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/gate"
)

// A finding gate in shell: every file it is given that says UPDATE is a finding.
const logGate = `#!/bin/sh
[ "$1" = "--self-test" ] && exit ${SELF_TEST_EXIT:-0}
files=$(sed 's/.*"files":\[\([^]]*\)\].*/\1/' | tr ',' '\n' | tr -d '"')
echo "scanning $(pwd)" >&2
printf '{"schema":1,"findings":['
sep=''
for f in $files; do
  grep -q UPDATE "$f" || continue
  printf '%s{"file":"%s","line":1,"anchor":"UPDATE","message":"UPDATE in the log store","why":"w","fix":"f"}' "$sep" "$f"
  sep=','
done
printf ']}\n'
`

// A measure gate in shell: every .sql file is an item; a removed one is legitimate when its name says so.
const coverageGate = `#!/bin/sh
req=$(cat)
case "$req" in
*'"mode":"removed"'*) printf '{"schema":1,"legitimate":["old.sql"]}\n'; exit 0 ;;
esac
printf '{"schema":1,"items":['
sep=''
for f in $(ls *.sql 2>/dev/null); do printf '%s{"key":"%s","detail":"listed"}' "$sep" "$f"; sep=','; done
printf ']}\n'
`

// script writes an executable gate command into a fresh directory and returns that directory.
// The commands are POSIX shell scripts, which Windows cannot run.
func script(t *testing.T, name, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("these gate commands are POSIX shell scripts, and Windows has no /bin/sh to run them")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "gates", name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

type trees struct{ base, head fs.FS }

func (s trees) Base() (engine.Version, error)       { return engine.Version{Label: "base", FS: s.base}, nil }
func (s trees) Head() (engine.Version, error)       { return engine.Version{Label: "head", FS: s.head}, nil }
func (s trees) Renames() (map[string]string, error) { return nil, nil }

func tree(gates string, files ...string) fstest.MapFS {
	t := fstest.MapFS{".hadome.yml": {Data: []byte("schema: 1\ngates:\n" + gates)}}
	for i := 0; i+1 < len(files); i += 2 {
		t[files[i]] = &fstest.MapFile{Data: []byte(files[i+1])}
	}
	return t
}

const logGateYML = "  - id: acme/log-append-only\n    kind: finding\n    run: [gates/log]\n    paths: [\"log/**\"]\n"

func checkWith(t *testing.T, workdir string, src engine.Source) (engine.Report, string, error) {
	t.Helper()
	var diag bytes.Buffer
	var closers []func() error
	e := &engine.Engine{Analyzer: adapter.New(), ConfigRules: func(c config.Config) ([]engine.Rule, error) {
		rules, closer, err := gate.Rules(c, workdir, &diag)
		if closer != nil {
			closers = append(closers, closer.Close)
		}
		return rules, err
	}}
	r, err := e.Check(src)
	for _, c := range closers {
		if cerr := c(); cerr != nil {
			t.Errorf("removing the copies: %v", cerr)
		}
	}
	return r, diag.String(), err
}

// viewpoint: external contract
// target: gate.Rules through engine.Engine.Check
// Gates invariant 1: a gate's findings are compared across versions like a built-in rule's.
func TestAFindingGateFailsOnlyWhatTheChangeAdds(t *testing.T) {
	dir := script(t, "log", logGate)
	base := tree(logGateYML, "log/a.sql", "UPDATE rows", "other/b.sql", "UPDATE x")
	head := tree(logGateYML, "log/a.sql", "UPDATE rows", "other/b.sql", "UPDATE x", "other/e.sql", "UPDATE y",
		"log/sub/c.sql", "UPDATE more", "log/d.sql", "INSERT")
	r, diag, err := checkWith(t, dir, trees{base, head})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Regressions) != 1 || r.Regressions[0].File != "log/sub/c.sql" || r.Regressions[0].Rule != "acme/log-append-only" {
		t.Fatalf("want only the new UPDATE in log/, got %+v", r.Regressions)
	}
	if r.Untouched != 1 {
		t.Errorf("the existing UPDATE in log/ counts as untouched, and other/ is not passed in; got %d", r.Untouched)
	}
	if !strings.Contains(diag, "acme/log-append-only: scanning ") {
		t.Errorf("standard error is passed on with the gate's ID: %q", diag)
	}
}

// viewpoint: external contract
// target: gate.Rules through engine.Engine.Check
// A measure gate's lost items are regressions unless the command calls them legitimate.
func TestAMeasureGateAsksAboutRemovedItems(t *testing.T) {
	dir := script(t, "coverage", coverageGate)
	yml := "  - id: acme/coverage\n    kind: measure\n    run: [gates/coverage]\n"
	base := tree(yml, "kept.sql", "", "old.sql", "", "gone.sql", "")
	head := tree(yml, "kept.sql", "")
	r, _, err := checkWith(t, dir, trees{base, head})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Regressions) != 1 || r.Regressions[0].Anchor != "gone.sql" {
		t.Fatalf("want gone.sql only (old.sql is legitimate), got %+v", r.Regressions)
	}
}

// viewpoint: failure handling
// target: gate.Rules through engine.Engine.Check
// Gates invariant 2: a gate that cannot run, or does not speak the protocol, never lets a check pass.
// guards: broken-gate-stops
func TestABrokenGateStopsTheCheck(t *testing.T) {
	cases := map[string]string{
		"exits non-zero":    "#!/bin/sh\ncat >/dev/null\nexit 1\n",
		"prints no JSON":    "#!/bin/sh\ncat >/dev/null\necho hello\n",
		"wrong schema":      "#!/bin/sh\ncat >/dev/null\necho '{\"schema\":2,\"findings\":[]}'\n",
		"unknown field":     "#!/bin/sh\ncat >/dev/null\necho '{\"schema\":1,\"findigns\":[]}'\n",
		"finding no anchor": "#!/bin/sh\ncat >/dev/null\necho '{\"schema\":1,\"findings\":[{\"file\":\"a\"}]}'\n",
		"never finishes":    "#!/bin/sh\nexec tail -f /dev/null\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			dir := script(t, "log", body)
			yml := "  - id: acme/log-append-only\n    kind: finding\n    run: [gates/log]\n    timeout: 200ms\n"
			if _, _, err := checkWith(t, dir, trees{tree(yml), tree(yml)}); err == nil {
				t.Fatal("the check must not run to a result")
			}
		})
	}
}

// viewpoint: external contract
// target: gate rules' SelfTest
// hadome self-test runs each gate's own --self-test.
func TestSelfTestRunsTheGatesOwn(t *testing.T) {
	dir := script(t, "log", logGate)
	c, err := config.Load(tree(logGateYML), dir)
	if err != nil {
		t.Fatal(err)
	}
	rules, closer, err := gate.Rules(c, dir, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	defer closer.Close()
	e := &engine.Engine{Analyzer: adapter.New(), Rules: rules}
	if failed := e.SelfTest(); len(failed) != 0 {
		t.Fatalf("a passing gate self-test: %+v", failed)
	}
	t.Setenv("SELF_TEST_EXIT", "1")
	if failed := e.SelfTest(); len(failed) != 1 || failed[0].Rule != "acme/log-append-only" {
		t.Fatalf("a failing gate self-test must be reported: %+v", failed)
	}
}

// viewpoint: failure handling
// target: gate.Rules through engine.Engine.Check
// A check runs each gate's own --self-test before using it. A gate that fails its self-test stops
// the check, so a broken gate cannot report that nothing regressed.
func TestACheckDoesNotUseAGateThatFailsItsSelfTest(t *testing.T) {
	dir := script(t, "log", logGate)
	src := trees{tree(logGateYML), tree(logGateYML, "log/append.go", "UPDATE")}
	if r, _, err := checkWith(t, dir, src); err != nil || len(r.Regressions) != 1 {
		t.Fatalf("a gate that passes its self-test is used: %v %+v", err, r.Regressions)
	}
	t.Setenv("SELF_TEST_EXIT", "1")
	_, _, err := checkWith(t, dir, src)
	if err == nil || !strings.Contains(err.Error(), "acme/log-append-only failed its own self-test") {
		t.Fatalf("a gate that fails its self-test must stop the check: %v", err)
	}
}
