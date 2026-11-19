package mutation_test

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rerorets/hadome/internal/adapter"
	"github.com/rerorets/hadome/internal/mutation"
	"github.com/rerorets/hadome/internal/source"
)

// useGoCache, as the first argument, makes the test binary a test command that uses Go's build
// cache: `<binary> stand-in-go-cache LOG pass|fail` appends to LOG the GOCACHE it was given and
// whether something an earlier run left is in it, writes a build result into it, and passes or
// fails.
const useGoCache = "stand-in-go-cache"

func standInGoCache(log, mode string) int {
	cache := os.Getenv("GOCACHE")
	_, err := os.Stat(filepath.Join(cache, "left"))
	f, err2 := os.OpenFile(log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err2 != nil {
		fmt.Println(err2)
		return 2
	}
	fmt.Fprintf(f, "%s left=%v\n", cache, err == nil)
	f.Close()
	if err := os.MkdirAll(filepath.Join(cache, "00"), 0o755); err != nil {
		fmt.Println(err)
		return 2
	}
	if err := os.WriteFile(filepath.Join(cache, "00", "x-a"), []byte("x"), 0o644); err != nil {
		fmt.Println(err)
		return 2
	}
	if mode == "fail" {
		return 1
	}
	return 0
}

// goCacheRun runs hadome mutate on a change to calc.Sign, with the build cache cache holding what
// an earlier run left, and a test command that logs the cache it is given to the returned log.
func goCacheRun(t *testing.T, cache, mode string, progress io.Writer) (log string, err error) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "left"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	log = filepath.Join(t.TempDir(), "log")
	cfg := fmt.Sprintf("schema: 1\nmutation:\n  test: [%q, %s, %q, %s]\n  timeout: 60s\n", exe, useGoCache, log, mode)
	base, head := version(calc, weak), version(strings.Replace(calc, "return 1", "return 1 // above zero", 1), weak)
	base[".hadome.yml"], head[".hadome.yml"] = cfg, cfg
	an := adapter.New()
	m := &mutation.Mutate{Analyzer: an, Mutator: an, Workdir: t.TempDir(), Trees: t.TempDir(), Progress: progress, GoCache: cache}
	_, err = m.Run(source.Dirs{BaseDir: dir(t, base), HeadDir: dir(t, head)})
	return log, err
}

// viewpoint: invariant
// target: mutation.Mutate.Run
// Invariant 21: every test command of a run is given the same build cache, which holds nothing an
// earlier run left, and once the run has ended the cache is gone, whether it gave a result or
// stopped because the tests fail with nothing broken.
func TestTheTestCommandsBuildCacheIsGoneOnceTheRunEnds(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"pass", "fail"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			cache := filepath.Join(t.TempDir(), "gocache")
			log, err := goCacheRun(t, cache, mode, io.Discard)
			if (err != nil) != (mode == "fail") {
				t.Fatalf("want an error %v, got %v", mode == "fail", err)
			}
			data, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(strings.TrimSpace(string(data)), "\n")
			if mode == "pass" && len(lines) < 2 {
				t.Errorf("want the run before breaking and the mutants logged, got %q", lines)
			}
			for _, l := range lines {
				if want := cache + " left=false"; l != want {
					t.Errorf("want every test command to log %q, got %q", want, l)
				}
			}
			if _, err := os.Stat(cache); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("want the build cache gone after the run, got %v", err)
			}
		})
	}
}
