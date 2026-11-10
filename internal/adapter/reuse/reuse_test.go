package reuse

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
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
	if err := s.Expect(head); err != nil {
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

var gate = make(chan struct{})
var entered = make(chan struct{}, 2)

var blocking = NewPure("test/blocking", ScopePair, func(a nameArg) (string, error) {
	called("test/blocking")
	entered <- struct{}{}
	<-gate
	return a.Name, nil
})

// viewpoint: behaviour
// target: reuse.Session.do
// Two calls of a stage with the same key at once run it once and share its result.
func TestConcurrentCallsWithOneKeyRunTheStageOnce(t *testing.T) {
	s := NewSession(nil, true)
	defer s.End()
	r := s.Run("base", fstest.MapFS{}, cfg)
	results := make(chan string, 2)
	go func() { v, _ := blocking.Do(r, nameArg{Name: "x"}); results <- v }()
	<-entered
	go func() { v, _ := blocking.Do(r, nameArg{Name: "x"}); results <- v }()
	close(gate)
	a, b := <-results, <-results
	if a != "x" || b != "x" || callsOf("test/blocking") != 1 {
		t.Errorf("results %q %q, ran %d times; want one run shared", a, b, callsOf("test/blocking"))
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
	rc := &recorder{run: r, seen: map[string]bool{}}
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
		if err := s.Expect(head); err != nil {
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
	if err := s.Expect(h); err != nil {
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
