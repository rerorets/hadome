package reuse

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"strconv"
)

// Answers are what a read of a version gave, written as strings so that a recorded answer and the
// answer of the same read on another version compare with ==. A missing file answers with the empty
// string, which no other answer is.
const isDir = "dir"

// answerOf answers a read that failed with err. A missing file is an answer, the empty one; any
// other failure is none (false): what made a read fail may not be there when it is read again, and
// its message depends on where the version is read from, so a result that met one is not reused.
func answerOf(err error) (string, bool) {
	if errors.Is(err, fs.ErrNotExist) {
		return "", true
	}
	return "", false
}

func contentAnswer(h Hash) string { return "content " + hex.EncodeToString(h[:]) }

// readFile reads a file of the run's version and returns its content, the content's hash, and the
// answer: the hash, or that the file is missing. ok is false when the read failed otherwise.
func (r *Run) readFile(name string) (data []byte, sum Hash, answer string, ok bool, err error) {
	data, err = fs.ReadFile(r.fsys, name)
	if err != nil {
		answer, ok = answerOf(err)
		return nil, Hash{}, answer, ok, err
	}
	sum = sha256.Sum256(data)
	answer = contentAnswer(sum)
	r.remember("file", name, answer)
	return data, sum, answer, true, nil
}

// stat answers whether name exists, and as what: a directory, or a file of some size.
func (r *Run) stat(name string) (info fs.FileInfo, answer string, ok bool, err error) {
	info, err = fs.Stat(r.fsys, name)
	if err != nil {
		answer, ok = answerOf(err)
		return nil, answer, ok, err
	}
	if info.IsDir() {
		return info, isDir, true, nil
	}
	return info, "file " + strconv.FormatInt(info.Size(), 10), true, nil
}

// readDir lists a directory and answers with the hash of its entries' names and kinds.
func (r *Run) readDir(name string) (entries []fs.DirEntry, answer string, ok bool, err error) {
	entries, err = fs.ReadDir(r.fsys, name)
	if err != nil {
		answer, ok = answerOf(err)
		return nil, answer, ok, err
	}
	h := sha256.New()
	for _, e := range entries {
		kind := "f"
		if e.IsDir() {
			kind = "d"
		}
		h.Write([]byte(kind + e.Name() + "\x00"))
	}
	return entries, "entries " + hex.EncodeToString(h.Sum(nil)), true, nil
}

// answer is the answer the run's version gives to a recorded read, for checking a remembered
// result against another version. ok is false when the read gives no answer.
func (r *Run) answer(op, name string) (string, bool) {
	if a, ok := r.recalled(op, name); ok {
		return a, true
	}
	var a string
	ok := true
	switch op {
	case "file":
		_, _, a, ok, _ = r.readFile(name)
	case "stat":
		_, a, ok, _ = r.stat(name)
	case "dir":
		_, a, ok, _ = r.readDir(name)
	case "config":
		a = r.cfgAnswer
	}
	if ok {
		r.remember(op, name, a)
	}
	return a, ok
}

// recordingFS is a version read through a reading stage: every read it serves is recorded with
// its answer.
type recordingFS struct{ rec *recorder }

// record records a read and its answer; a read without an answer (ok false) makes the stage's
// result one that is never reused.
func (f recordingFS) record(op, name, answer string, ok bool) {
	if !ok {
		f.rec.unanswered()
		return
	}
	f.rec.add(read{op: op, arg: name, answer: answer})
}

// recordContent records a file read, and the content it read among what the result was computed
// from.
func (f recordingFS) recordContent(name string, sum Hash, answer string, ok bool, err error) {
	f.record("file", name, answer, ok)
	if err == nil {
		f.rec.read(sum)
	}
}

func (f recordingFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	run := f.rec.run
	info, answer, ok, err := run.stat(name)
	if err != nil {
		f.record("stat", name, answer, ok)
		return nil, &fs.PathError{Op: "open", Path: name, Err: err}
	}
	if info.IsDir() {
		entries, answer, ok, err := run.readDir(name)
		f.record("dir", name, answer, ok)
		if err != nil {
			return nil, &fs.PathError{Op: "open", Path: name, Err: err}
		}
		return &dirFile{info: info, entries: entries}, nil
	}
	data, sum, answer, ok, err := run.readFile(name)
	f.recordContent(name, sum, answer, ok, err)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: err}
	}
	return &memFile{info: info, r: bytes.NewReader(data)}, nil
}

func (f recordingFS) ReadFile(name string) ([]byte, error) {
	data, sum, answer, ok, err := f.rec.run.readFile(name)
	f.recordContent(name, sum, answer, ok, err)
	return data, err
}

func (f recordingFS) Stat(name string) (fs.FileInfo, error) {
	info, answer, ok, err := f.rec.run.stat(name)
	f.record("stat", name, answer, ok)
	return info, err
}

func (f recordingFS) ReadDir(name string) ([]fs.DirEntry, error) {
	entries, answer, ok, err := f.rec.run.readDir(name)
	f.record("dir", name, answer, ok)
	return entries, err
}

type memFile struct {
	info fs.FileInfo
	r    *bytes.Reader
}

func (m *memFile) Stat() (fs.FileInfo, error) { return m.info, nil }
func (m *memFile) Read(p []byte) (int, error) { return m.r.Read(p) }
func (m *memFile) Close() error               { return nil }

type dirFile struct {
	info    fs.FileInfo
	entries []fs.DirEntry
	at      int
}

func (d *dirFile) Stat() (fs.FileInfo, error) { return d.info, nil }
func (d *dirFile) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: d.info.Name(), Err: errors.New("is a directory")}
}
func (d *dirFile) Close() error { return nil }

func (d *dirFile) ReadDir(n int) ([]fs.DirEntry, error) {
	rest := d.entries[d.at:]
	if n <= 0 {
		d.at = len(d.entries)
		return rest, nil
	}
	if len(rest) == 0 {
		return nil, io.EOF
	}
	if n > len(rest) {
		n = len(rest)
	}
	d.at += n
	return rest[:n], nil
}
