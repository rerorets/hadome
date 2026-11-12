package reuse

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"runtime"
	"slices"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/config"
)

// counted is a result that counts how often it is let go.
type counted struct {
	name     string
	released *int
}

func (c *counted) Release() { *c.released++ }

type nameArg struct {
	Name string
	Src  []byte
}

// calls counts how often each test stage's function runs, by stage.
var (
	callsMu sync.Mutex
	calls   = map[string]int{}
)

func called(id string) {
	callsMu.Lock()
	calls[id]++
	callsMu.Unlock()
}

func callsOf(id string) int {
	callsMu.Lock()
	defer callsMu.Unlock()
	return calls[id]
}

var released int

var upper = NewPure("test/upper", ScopeLanguage, func(a nameArg) (*counted, error) {
	called("test/upper")
	return &counted{name: a.Name + string(a.Src), released: &released}, nil
})

type pathArg struct{ Path string }

// readOne reads one file and the files a directory above it lists; a missing file reads as "".
var readOne = NewReading("test/read", ScopeLanguage, func(rd Reads, a pathArg) (string, error) {
	called("test/read")
	data, err := fs.ReadFile(rd.FS(), a.Path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	if _, err := fs.ReadDir(rd.FS(), "lib"); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	return string(data) + rd.Config().TestFiles[0], nil
})

// summary is a plain result: only the first line of the file.
var summary = NewReading("test/summary", ScopeLanguage, func(rd Reads, a pathArg) (string, error) {
	called("test/summary")
	data, err := fs.ReadFile(rd.FS(), a.Path)
	if err != nil {
		return "", err
	}
	first, _, _ := cutLine(string(data))
	return first, nil
})

// outer reads only another stage's plain result.
var outer = NewReading("test/outer", ScopeLanguage, func(rd Reads, a pathArg) (string, error) {
	called("test/outer")
	s, err := summary.Do(rd.Run(), a)
	return "outer:" + s, err
})

func cutLine(s string) (string, string, bool) {
	for i := range len(s) {
		if s[i] == '\n' {
			return s[:i], s[i+1:], true
		}
	}
	return s, "", false
}

var cfg = config.Analysis{TestFiles: []string{"checks/**"}}

// viewpoint: behaviour
// target: reuse.encode
// Two different arguments never share a key, even when their strings written one after another
// are the same text, and a map's key does not depend on the order it is iterated in.
func TestDifferentArgumentsHaveDifferentKeys(t *testing.T) {
	type two struct{ A, B string }
	type bytesArg struct{ A, B []byte }
	pairs := [][2]any{
		{two{"ab", "c"}, two{"a", "bc"}},
		{bytesArg{[]byte("ab"), []byte("c")}, bytesArg{[]byte("a"), []byte("bc")}},
		{[]string{"a", "b"}, []string{"ab"}},
		{map[string]string{"a": "b"}, map[string]string{"ab": ""}},
		{config.Analysis{TestFiles: []string{"x"}}, config.Analysis{GeneratedFiles: []string{"x"}}},
	}
	for i, p := range pairs {
		a, _ := encode(p[0])
		b, _ := encode(p[1])
		if a == b {
			t.Errorf("pair %d shares a key", i)
		}
	}
	m1 := map[string][]string{}
	m2 := map[string][]string{}
	for _, k := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		m1[k] = []string{k}
	}
	for _, k := range []string{"h", "g", "f", "e", "d", "c", "b", "a"} {
		m2[k] = []string{k}
	}
	if a, b := keyOf(m1), keyOf(m2); a != b {
		t.Error("the same map built in another order has another key")
	}
}

func keyOf(v any) Hash {
	h, _ := encode(v)
	return h
}

// viewpoint: failure
// target: reuse.NewPure
// A stage whose argument holds a pointer, an interface or a function, or whose name is taken,
// stops hadome as it starts.
func TestAStageThatCannotBeKeyedIsRefused(t *testing.T) {
	type withPointer struct{ P *int }
	type withInterface struct{ V any }
	type withFunc struct{ F func() }
	refused := func(name string, register func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Errorf("%s was registered", name)
			}
		}()
		register()
	}
	refused("a pointer", func() { NewPure("test/pointer", ScopePair, func(withPointer) (int, error) { return 0, nil }) })
	refused("an interface", func() { NewPure("test/iface", ScopePair, func(withInterface) (int, error) { return 0, nil }) })
	refused("a function", func() { NewReading("test/func", ScopePair, func(Reads, withFunc) (int, error) { return 0, nil }) })
	refused("a taken name", func() { NewPure("test/upper", ScopePair, func(nameArg) (int, error) { return 0, nil }) })
}

// viewpoint: behaviour
// target: reuse.Pure.Do
// A pure stage runs once for one argument in a session, whichever version calls it, and again for
// another argument. Without reuse it runs on every call and returns the same result.
func TestAPureStageRunsOncePerArgument(t *testing.T) {
	for _, reuse := range []bool{true, false} {
		s := NewSession(nil, reuse)
		base, head := s.Run("base", fstest.MapFS{}, cfg), s.Run("head", fstest.MapFS{}, cfg)
		before := callsOf("test/upper")
		a, _ := upper.Do(base, nameArg{Name: "a", Src: []byte("1")})
		b, _ := upper.Do(head, nameArg{Name: "a", Src: []byte("1")})
		c, _ := upper.Do(head, nameArg{Name: "a", Src: []byte("2")})
		want := 2
		if !reuse {
			want = 3
		}
		if got := callsOf("test/upper") - before; got != want {
			t.Errorf("reuse %v: ran %d times, want %d", reuse, got, want)
		}
		if a.name != "a1" || b.name != "a1" || c.name != "a2" {
			t.Errorf("reuse %v: results %q %q %q", reuse, a.name, b.name, c.name)
		}
		s.End()
	}
}

// viewpoint: behaviour
// target: reuse.Reading.Do
// A reading stage's result serves another version only when every read it recorded gives the same
// answer there: the file it read, a file it found missing, a directory it listed, and the
// configuration.
func TestAReadingStageServesAVersionOnlyWhenEveryReadAnswersTheSame(t *testing.T) {
	base := fstest.MapFS{"app/a.txt": {Data: []byte("x")}, "lib/b.txt": {Data: []byte("b")}}
	cases := []struct {
		name string
		head fstest.MapFS
		cfg  config.Analysis
		path string
		hit  bool
	}{
		{"same", fstest.MapFS{"app/a.txt": {Data: []byte("x")}, "lib/b.txt": {Data: []byte("b")}}, cfg, "app/a.txt", true},
		{"unread file changed", fstest.MapFS{"app/a.txt": {Data: []byte("x")}, "lib/b.txt": {Data: []byte("changed")}, "other.txt": {}}, cfg, "app/a.txt", true},
		{"read file changed", fstest.MapFS{"app/a.txt": {Data: []byte("y")}, "lib/b.txt": {Data: []byte("b")}}, cfg, "app/a.txt", false},
		{"listed directory changed", fstest.MapFS{"app/a.txt": {Data: []byte("x")}, "lib/b.txt": {Data: []byte("b")}, "lib/c.txt": {}}, cfg, "app/a.txt", false},
		{"configuration changed", fstest.MapFS{"app/a.txt": {Data: []byte("x")}, "lib/b.txt": {Data: []byte("b")}}, config.Analysis{TestFiles: []string{"other/**"}}, "app/a.txt", false},
		{"missing file appeared", fstest.MapFS{"app/a.txt": {Data: []byte("x")}, "lib/b.txt": {Data: []byte("b")}, "app/new.txt": {Data: []byte("n")}}, cfg, "app/new.txt", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewSession(nil, true)
			defer s.End()
			b, h := s.Run("base", base, cfg), s.Run("head", tc.head, tc.cfg)
			if _, err := readOne.Do(b, pathArg{tc.path}); err != nil {
				t.Fatal(err)
			}
			got, err := readOne.Do(h, pathArg{tc.path})
			if err != nil {
				t.Fatal(err)
			}
			fresh, err := readOne.Do(NewSession(nil, true).Run("head", tc.head, tc.cfg), pathArg{tc.path})
			if err != nil {
				t.Fatal(err)
			}
			if got != fresh {
				t.Errorf("head got %q, reading head alone gives %q", got, fresh)
			}
			if st := s.Stats()["test/read"]; (st.Hits == 1) != tc.hit {
				t.Errorf("stats %+v, want a hit: %v", st, tc.hit)
			}
		})
	}
}

// viewpoint: behaviour
// target: reuse.Reading.Do
// A reading stage that read only another stage's plain result serves head when that result is
// the same, even though the file it came from changed.
func TestAStageReadingAnUnchangedPlainResultIsNotRunAgain(t *testing.T) {
	s := NewSession(nil, true)
	defer s.End()
	b := s.Run("base", fstest.MapFS{"a.txt": {Data: []byte("first\nbody")}}, cfg)
	h := s.Run("head", fstest.MapFS{"a.txt": {Data: []byte("first\nanother body")}}, cfg)
	if _, err := outer.Do(b, pathArg{"a.txt"}); err != nil {
		t.Fatal(err)
	}
	got, err := outer.Do(h, pathArg{"a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "outer:first" {
		t.Errorf("head got %q", got)
	}
	if st := s.Stats(); st["test/outer"].Hits != 1 || st["test/summary"].Misses != 2 {
		t.Errorf("stats %+v, want the outer stage served from base and the summary read twice", st)
	}
}

// viewpoint: behaviour
// target: reuse.Session.Settle
// A result computed from a file content head does not have is let go when base's language ends;
// one head can use is kept until head's language ends. Each is let go exactly once.
func TestOnlyResultsHeadCanUseAreKept(t *testing.T) {
	released = 0
	s := NewSession(nil, true)
	base := s.Run("base", fstest.MapFS{}, cfg)
	head := s.Run("head", fstest.MapFS{"same.txt": {Data: []byte("kept")}}, cfg)
	if err := s.Expect(head, pathsOf(head)); err != nil {
		t.Fatal(err)
	}
	if _, err := upper.Do(base, nameArg{Name: "same", Src: []byte("kept")}); err != nil {
		t.Fatal(err)
	}
	if _, err := upper.Do(base, nameArg{Name: "gone", Src: []byte("only in base")}); err != nil {
		t.Fatal(err)
	}
	s.Settle()
	if got := s.Remembered(); !slices.Equal(got, []string{"test/upper"}) || released != 1 {
		t.Errorf("after base: remembered %v, %d let go; want the result head can use, 1 let go", got, released)
	}
	if _, err := upper.Do(head, nameArg{Name: "same", Src: []byte("kept")}); err != nil {
		t.Fatal(err)
	}
	if st := s.Stats()["test/upper"]; st.Hits != 1 {
		t.Errorf("stats %+v, want head served from base", st)
	}
	s.EndLanguage()
	s.End()
	if got := s.Remembered(); len(got) != 0 || released != 2 {
		t.Errorf("after the end: remembered %v, %d let go; want none, 2", got, released)
	}
}

// viewpoint: behaviour
// target: reuse.Session.End
// A ScopePair result outlives the language and is let go only when the session ends.
func TestAPairResultIsKeptUntilTheEnd(t *testing.T) {
	stage := pairStage
	s := NewSession(nil, true)
	r := s.Run("base", fstest.MapFS{}, cfg)
	if _, err := stage.Do(r, nameArg{Name: "p"}); err != nil {
		t.Fatal(err)
	}
	s.EndLanguage()
	if got := s.Remembered(); !slices.Equal(got, []string{"test/pair"}) {
		t.Errorf("after the language: %v", got)
	}
	s.End()
	if got := s.Remembered(); len(got) != 0 {
		t.Errorf("after the end: %v", got)
	}
}

var pairStage = NewPure("test/pair", ScopePair, func(a nameArg) (string, error) { return a.Name, nil })

// gate and entered hold the blocking stage; each test that uses them makes them anew.
var gate, entered chan struct{}

var blocking = NewPure("test/blocking", ScopePair, func(a nameArg) (string, error) {
	called("test/blocking")
	entered <- struct{}{}
	<-gate
	return a.Name, nil
})

// waitFor returns once the stage has had a call waiting for another, letting other goroutines run
// until then.
func waitFor(s *Session, id string) {
	for s.Stats()[id].Waits == 0 {
		runtime.Gosched()
	}
}

// viewpoint: behaviour
// target: reuse.Session.do
// Two calls of a stage with the same key at once run it once and share its result: the second
// waits for the first, then gets what it computed.
func TestConcurrentCallsWithOneKeyRunTheStageOnce(t *testing.T) {
	s := NewSession(nil, true)
	defer s.End()
	r := s.Run("base", fstest.MapFS{}, cfg)
	gate, entered = make(chan struct{}), make(chan struct{}, 2)
	before := callsOf("test/blocking")
	results := make(chan string, 2)
	go func() { v, _ := blocking.Do(r, nameArg{Name: "x"}); results <- v }()
	<-entered
	go func() { v, _ := blocking.Do(r, nameArg{Name: "x"}); results <- v }()
	waitFor(s, "test/blocking")
	close(gate)
	a, b := <-results, <-results
	if ran := callsOf("test/blocking") - before; a != "x" || b != "x" || ran != 1 {
		t.Errorf("results %q %q, ran %d times; want one run shared", a, b, ran)
	}
	if st := s.Stats()["test/blocking"]; st.Misses != 1 || st.Hits != 1 || st.Waits != 1 {
		t.Errorf("stats %+v, want one run, one wait, one result shared", st)
	}
}

// innerGate and innerEntered hold firstLine on a file that says "block"; made anew by each test.
var innerGate, innerEntered chan struct{}

// firstLine reads a file's first line, and stops before answering when the file says "block".
var firstLine = NewReading("test/first-line", ScopeLanguage, func(rd Reads, a pathArg) (string, error) {
	data, err := fs.ReadFile(rd.FS(), a.Path)
	if err != nil {
		return "", err
	}
	first, rest, _ := cutLine(string(data))
	if rest == "block" {
		innerEntered <- struct{}{}
		<-innerGate
	}
	return first, nil
})

// aboveFirstLine reads only another stage's first line.
var aboveFirstLine = NewReading("test/above-first-line", ScopeLanguage, func(rd Reads, a pathArg) (string, error) {
	first, err := firstLine.Do(rd.Run(), a)
	return "above:" + first, err
})

// viewpoint: behaviour
// target: reuse.Session.do
// A call that comes while another call checks base's result against head waits for that check,
// and is then served the same result.
func TestACallWaitsForTheCheckOfTheSameKey(t *testing.T) {
	s := NewSession(nil, true)
	defer s.End()
	base := s.Run("base", fstest.MapFS{"a.txt": {Data: []byte("first\nbody")}}, cfg)
	head := s.Run("head", fstest.MapFS{"a.txt": {Data: []byte("first\nblock")}}, cfg)
	innerGate, innerEntered = make(chan struct{}), make(chan struct{}, 1)
	if _, err := aboveFirstLine.Do(base, pathArg{"a.txt"}); err != nil {
		t.Fatal(err)
	}
	results := make(chan string, 2)
	go func() { v, _ := aboveFirstLine.Do(head, pathArg{"a.txt"}); results <- v }()
	<-innerEntered
	go func() { v, _ := aboveFirstLine.Do(head, pathArg{"a.txt"}); results <- v }()
	waitFor(s, "test/above-first-line")
	close(innerGate)
	if a, b := <-results, <-results; a != "above:first" || b != "above:first" {
		t.Errorf("results %q %q", a, b)
	}
	if st := s.Stats()["test/above-first-line"]; st.Misses != 1 || st.Hits != 2 {
		t.Errorf("stats %+v, want base's result served to both calls on head", st)
	}
}

// opened reads through Open, as code that streams files does: a file, a directory's entries, and a
// file that may be missing.
var opened = NewReading("test/open", ScopeLanguage, func(rd Reads, a pathArg) (string, error) {
	fsys := rd.FS()
	f, err := fsys.Open(a.Path)
	if err != nil {
		return "", err
	}
	data, err := io.ReadAll(f)
	f.Close()
	if err != nil {
		return "", err
	}
	d, err := fsys.Open("lib")
	if err != nil {
		return "", err
	}
	entries, err := d.(fs.ReadDirFile).ReadDir(-1)
	d.Close()
	if err != nil {
		return "", err
	}
	missing := "missing"
	if m, err := fsys.Open("app/new.txt"); err == nil {
		m.Close()
		missing = "present"
	}
	return fmt.Sprintf("%s %d %s", data, len(entries), missing), nil
})

// viewpoint: behaviour
// target: reuse.recordingFS.Open
// Files opened, directories opened and listed, and files found missing through Open are recorded
// like any other read: head is served only when each gives the same answer.
func TestReadsThroughOpenAreRecorded(t *testing.T) {
	base := fstest.MapFS{"app/a.txt": {Data: []byte("x")}, "lib/b.txt": {Data: []byte("b")}}
	cases := []struct {
		name string
		head fstest.MapFS
		hit  bool
	}{
		{"same", fstest.MapFS{"app/a.txt": {Data: []byte("x")}, "lib/b.txt": {Data: []byte("b")}}, true},
		{"opened file changed", fstest.MapFS{"app/a.txt": {Data: []byte("y")}, "lib/b.txt": {Data: []byte("b")}}, false},
		{"opened directory changed", fstest.MapFS{"app/a.txt": {Data: []byte("x")}, "lib/b.txt": {Data: []byte("b")}, "lib/c.txt": {}}, false},
		{"missing file appeared", fstest.MapFS{"app/a.txt": {Data: []byte("x")}, "lib/b.txt": {Data: []byte("b")}, "app/new.txt": {}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewSession(nil, true)
			defer s.End()
			if _, err := opened.Do(s.Run("base", base, cfg), pathArg{"app/a.txt"}); err != nil {
				t.Fatal(err)
			}
			got, err := opened.Do(s.Run("head", tc.head, cfg), pathArg{"app/a.txt"})
			if err != nil {
				t.Fatal(err)
			}
			fresh, err := opened.Do(NewSession(nil, true).Run("head", tc.head, cfg), pathArg{"app/a.txt"})
			if err != nil {
				t.Fatal(err)
			}
			if got != fresh {
				t.Errorf("head got %q, reading head alone gives %q", got, fresh)
			}
			if st := s.Stats()["test/open"]; (st.Hits == 1) != tc.hit {
				t.Errorf("stats %+v, want a hit: %v", st, tc.hit)
			}
		})
	}
}

// viewpoint: failure
// target: reuse.recordingFS.Open
// A path a file system must refuse is refused through the recorded file system too.
func TestAnInvalidPathIsRefused(t *testing.T) {
	r := Lone(fstest.MapFS{"a.txt": {}})
	rc := &recorder{run: r}
	if _, err := rc.FS().Open("../a.txt"); !errors.Is(err, fs.ErrInvalid) {
		t.Errorf("opening ../a.txt gave %v, want an invalid path", err)
	}
	if _, err := rc.FS().Open("a.txt"); err != nil {
		t.Errorf("opening a.txt gave %v", err)
	}
}

// viewpoint: behaviour
// target: reuse.Session.Settle
// A reading stage's result is kept for head when every file it read, and every file it found
// missing, can be the same in head; head is then served without running the stage.
func TestAReadingStageHeadCanUseIsKept(t *testing.T) {
	for _, path := range []string{"app/a.txt", "app/new.txt"} {
		s := NewSession(nil, true)
		files := fstest.MapFS{"app/a.txt": {Data: []byte("x")}, "lib/b.txt": {Data: []byte("b")}}
		base, head := s.Run("base", files, cfg), s.Run("head", files, cfg)
		if err := s.Expect(head, pathsOf(head)); err != nil {
			t.Fatal(err)
		}
		if _, err := readOne.Do(base, pathArg{path}); err != nil {
			t.Fatal(err)
		}
		s.Settle()
		if _, err := readOne.Do(head, pathArg{path}); err != nil {
			t.Fatal(err)
		}
		if st := s.Stats()["test/read"]; st.Hits != 1 {
			t.Errorf("%s: stats %+v, want head served from base", path, st)
		}
		s.End()
	}
}

// counting is a file system that counts how often each file is read.
type counting struct {
	fstest.MapFS
	reads map[string]int
}

func (c counting) ReadFile(name string) ([]byte, error) {
	c.reads[name]++
	return c.MapFS.ReadFile(name)
}

func (c counting) ReadDir(name string) ([]fs.DirEntry, error) {
	c.reads[name+"/"]++
	return c.MapFS.ReadDir(name)
}

// viewpoint: behaviour
// target: reuse.Session.Expect
// Checking remembered results against head reads no head file twice, and lists no head directory
// twice: head's files are read once, when the session learns head, and an answer found while
// checking one result serves the check of the next.
func TestCheckingAgainstHeadReadsEachFileOnce(t *testing.T) {
	s := NewSession(nil, true)
	defer s.End()
	files := fstest.MapFS{"app/a.txt": {Data: []byte("x")}, "lib/b.txt": {Data: []byte("b")}}
	head := counting{MapFS: files, reads: map[string]int{}}
	b, h := s.Run("base", files, cfg), s.Run("head", head, cfg)
	if err := s.Expect(h, pathsOf(h)); err != nil {
		t.Fatal(err)
	}
	listed := head.reads["lib/"]
	for _, p := range []string{"app/a.txt", "lib/b.txt"} {
		if _, err := readOne.Do(b, pathArg{p}); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		for _, p := range []string{"app/a.txt", "lib/b.txt"} {
			if _, err := readOne.Do(h, pathArg{p}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if head.reads["app/a.txt"] != 1 || head.reads["lib/b.txt"] != 1 {
		t.Errorf("head's files were read %v times, want once each", head.reads)
	}
	if got := head.reads["lib/"] - listed; got != 1 {
		t.Errorf("head's lib was listed %d times while checking, want once", got)
	}
}

// pathsOf lists every file of the run's version, as the analyzer lists a version's files.
func pathsOf(r *Run) []string {
	var out []string
	_ = fs.WalkDir(r.fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			out = append(out, p)
		}
		return err
	})
	return out
}

// probe looks a file up and says whether it found one, whatever stopped it from finding one.
var probe = NewReading("test/probe", ScopeLanguage, func(rd Reads, a pathArg) (string, error) {
	if _, err := fs.Stat(rd.FS(), a.Path); err != nil {
		return "none", nil
	}
	return "some", nil
})

// refusing is a file system that cannot look at one file, for a reason other than its absence.
type refusing struct {
	fstest.MapFS
	path string
}

func (f refusing) Stat(name string) (fs.FileInfo, error) {
	if name == f.path {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrPermission}
	}
	return f.MapFS.Stat(name)
}

func (f refusing) Open(name string) (fs.File, error) {
	if name == f.path {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
	}
	return f.MapFS.Open(name)
}

// viewpoint: behaviour
// target: reuse.Session.validate
// A result whose read failed for another reason than a missing file is never served to another
// version, even one where the file is missing and the result would read the same: the failure is
// no answer the other version can be checked against.
func TestAResultWhoseReadFailedIsNotReused(t *testing.T) {
	s := NewSession(nil, true)
	defer s.End()
	base := s.Run("base", refusing{MapFS: fstest.MapFS{"a.txt": {}}, path: "locked.txt"}, cfg)
	head := s.Run("head", fstest.MapFS{"a.txt": {}}, cfg)
	if got, err := probe.Do(base, pathArg{"locked.txt"}); err != nil || got != "none" {
		t.Fatalf("base: %q %v", got, err)
	}
	if got, err := probe.Do(head, pathArg{"locked.txt"}); err != nil || got != "none" {
		t.Fatalf("head: %q %v", got, err)
	}
	if st := s.Stats()["test/probe"]; st.Hits != 0 || st.Misses != 2 {
		t.Errorf("stats %+v, want the stage run for each version", st)
	}
}

// viewpoint: behaviour
// target: reuse.Run.stat
// A stage that looked at a file or a directory that is there is served to a version where both are
// the same.
func TestAStageThatLookedAtExistingFilesIsReused(t *testing.T) {
	files := fstest.MapFS{"a.txt": {Data: []byte("x")}, "lib/b.txt": {}}
	for _, path := range []string{"a.txt", "lib"} {
		s := NewSession(nil, true)
		if got, err := probe.Do(s.Run("base", files, cfg), pathArg{path}); err != nil || got != "some" {
			t.Fatalf("%s: base %q %v", path, got, err)
		}
		if _, err := probe.Do(s.Run("head", files, cfg), pathArg{path}); err != nil {
			t.Fatal(err)
		}
		if st := s.Stats()["test/probe"]; st.Hits != 1 {
			t.Errorf("%s: stats %+v, want head served from base", path, st)
		}
		s.End()
	}
}

// node is a recursive argument: a tree of names.
type node struct {
	Name string
	Kids []node
}

var nodeNames = NewPure("test/node", ScopePair, func(n node) (int, error) { return len(n.Kids), nil })

// viewpoint: behaviour
// target: reuse.encode
// Values of one type that differ anywhere have different keys: in a boolean, a number, a string, a
// byte slice, a list, an array, a map's key or value, or where one list ends and the next begins.
// A recursive type is made of values only too.
func TestValuesThatDifferAnywhereHaveDifferentKeys(t *testing.T) {
	type two struct{ A, B string }
	type lists struct{ A, B []string }
	pairs := [][2]any{
		{two{"aB", ""}, two{"a", "B"}},
		{two{"a", "b"}, two{"b", "a"}},
		{[]bool{true, false}, []bool{false, true}},
		{[]int{1, 2}, []int{2, 1}},
		{[]uint{1, 2}, []uint{2, 1}},
		{[]float64{1, 2}, []float64{2, 1}},
		{[][]byte{[]byte("a")}, [][]byte{[]byte("b")}},
		{[2]int{1, 2}, [2]int{2, 1}},
		{[][]int{{1}}, [][]int{{2}}},
		{lists{A: []string{"x"}}, lists{B: []string{"x"}}},
		{map[string]string{"a": "x"}, map[string]string{"b": "x"}},
		{map[string]string{"a": "x"}, map[string]string{"a": "y"}},
		{node{Name: "r", Kids: []node{{Name: "a"}}}, node{Name: "r", Kids: []node{{Name: "b"}}}},
	}
	for i, p := range pairs {
		if keyOf(p[0]) == keyOf(p[1]) {
			t.Errorf("pair %d shares a key: %v and %v", i, p[0], p[1])
		}
	}
	s := NewSession(nil, true)
	defer s.End()
	if n, err := nodeNames.Do(s.Run("base", fstest.MapFS{}, cfg), node{Name: "r", Kids: []node{{Name: "a"}}}); err != nil || n != 1 {
		t.Errorf("a recursive argument gave %d %v", n, err)
	}
}

// viewpoint: external contract
// target: reuse.recordingFS
// The recorded file system behaves as a file system: opening, reading, stating and listing, a
// directory read a few entries at a time included.
func TestTheRecordedFileSystemIsAFileSystem(t *testing.T) {
	r := Lone(fstest.MapFS{"a.txt": {Data: []byte("x")}, "lib/b.txt": {Data: []byte("b")}, "lib/c.txt": {}, "lib/d/e.txt": {}})
	if err := fstest.TestFS((&recorder{run: r}).FS(), "a.txt", "lib/b.txt", "lib/c.txt", "lib/d/e.txt"); err != nil {
		t.Error(err)
	}
}

// viewpoint: failure
// target: reuse.Session.Expect
// Learning head fails when a file head is said to have cannot be read.
func TestExpectingAFileHeadDoesNotHaveFails(t *testing.T) {
	s := NewSession(nil, true)
	defer s.End()
	if err := s.Expect(s.Run("head", fstest.MapFS{}, cfg), []string{"missing.txt"}); err == nil {
		t.Error("no error for a file head does not have")
	}
}

// viewpoint: failure
// target: reuse.Session.validate
// A result is not served to head when checking it fails there: a stage it read fails on head, or
// a file it read cannot be read on head.
func TestAResultIsNotServedWhenCheckingItFails(t *testing.T) {
	s := NewSession(nil, true)
	defer s.End()
	present := fstest.MapFS{"a.txt": {Data: []byte("first\nbody")}}
	if _, err := outer.Do(s.Run("base", present, cfg), pathArg{"a.txt"}); err != nil {
		t.Fatal(err)
	}
	if _, err := outer.Do(s.Run("head", fstest.MapFS{}, cfg), pathArg{"a.txt"}); err == nil {
		t.Error("head without the file was served base's result instead of failing")
	}
	if _, err := probe.Do(s.Run("base", present, cfg), pathArg{"a.txt"}); err != nil {
		t.Fatal(err)
	}
	got, err := probe.Do(s.Run("head", refusing{MapFS: present, path: "a.txt"}, cfg), pathArg{"a.txt"})
	if err != nil || got != "none" {
		t.Errorf("head that cannot read the file got %q %v, want it read again: none", got, err)
	}
}

// probeTwo looks one file up, then reads another.
var probeTwo = NewReading("test/probe-two", ScopeLanguage, func(rd Reads, a pathArg) (string, error) {
	_, err := fs.Stat(rd.FS(), a.Path)
	data, _ := fs.ReadFile(rd.FS(), "a.txt")
	return fmt.Sprintf("%v %s", err == nil, data), nil
})

// viewpoint: failure
// target: reuse.recorder
// A stage goes on reading after a read that failed.
func TestAStageReadsOnAfterAFailedRead(t *testing.T) {
	r := Lone(refusing{MapFS: fstest.MapFS{"a.txt": {Data: []byte("x")}}, path: "locked.txt"})
	if got, err := probeTwo.Do(r, pathArg{"locked.txt"}); err != nil || got != "false x" {
		t.Errorf("got %q %v", got, err)
	}
}

// viewpoint: behaviour
// target: reuse.Session.EndLanguage
// A result let go is never served again: the stage runs again for the same argument.
func TestAResultLetGoIsNotServed(t *testing.T) {
	s := NewSession(nil, true)
	defer s.End()
	r := s.Run("base", fstest.MapFS{}, cfg)
	if _, err := upper.Do(r, nameArg{Name: "gone"}); err != nil {
		t.Fatal(err)
	}
	s.EndLanguage()
	if _, err := upper.Do(r, nameArg{Name: "gone"}); err != nil {
		t.Fatal(err)
	}
	if st := s.Stats()["test/upper"]; st.Misses != 2 || st.Hits != 0 {
		t.Errorf("stats %+v, want the stage run again", st)
	}
}

// viewpoint: behaviour
// target: reuse.Lone
// A lone run remembers within itself, and keeps its version's label.
func TestALoneRunRemembers(t *testing.T) {
	r := Lone(fstest.MapFS{})
	before := callsOf("test/upper")
	for range 2 {
		if _, err := upper.Do(r, nameArg{Name: "lone"}); err != nil {
			t.Fatal(err)
		}
	}
	if got := callsOf("test/upper") - before; got != 1 {
		t.Errorf("the stage ran %d times, want once", got)
	}
	if got := NewSession(nil, true).Run("base", fstest.MapFS{}, cfg).Label(); got != "base" {
		t.Errorf("label %q", got)
	}
}

// summing is a version that knows its files' hashes without reading them, as the file system the
// engine wraps around a version does.
type summing struct{ counting }

func (s summing) Sum(name string) ([32]byte, error) {
	f, ok := s.MapFS[name]
	if !ok {
		return [32]byte{}, &fs.PathError{Op: "sum", Path: name, Err: fs.ErrNotExist}
	}
	return sha256.Sum256(f.Data), nil
}

// viewpoint: behaviour
// target: reuse.Session.Expect
// Design "Remembering only what head can hit": a head that knows its files' hashes is learned
// without reading any file, and those hashes answer the check of remembered results against head,
// so head's results are served from base without head's files being read at all.
func TestAHeadThatKnowsItsHashesIsNotRead(t *testing.T) {
	s := NewSession(nil, true)
	defer s.End()
	files := fstest.MapFS{"app/a.txt": {Data: []byte("x")}, "lib/b.txt": {Data: []byte("b")}}
	head := summing{counting{MapFS: files, reads: map[string]int{}}}
	b, h := s.Run("base", files, cfg), s.Run("head", head, cfg)
	if err := s.Expect(h, pathsOf(h)); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"app/a.txt", "lib/b.txt"} {
		if _, err := readOne.Do(b, pathArg{p}); err != nil {
			t.Fatal(err)
		}
		if _, err := readOne.Do(h, pathArg{p}); err != nil {
			t.Fatal(err)
		}
	}
	if head.reads["app/a.txt"] != 0 || head.reads["lib/b.txt"] != 0 {
		t.Errorf("head's files were read %v times, want none", head.reads)
	}
	if err := s.Expect(s.Run("head", summing{counting{MapFS: fstest.MapFS{}, reads: map[string]int{}}}, cfg), []string{"missing.txt"}); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("learning a head whose file has no hash: got %v, want not-exist", err)
	}
}
