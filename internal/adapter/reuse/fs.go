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
// answer of the same read on another version compare with ==.
const (
	absent = "absent"
	isDir  = "dir"
)

// answerOf writes the answer of a read that failed with err: absent for a missing file, the error
// for any other failure.
func answerOf(err error) string {
	if errors.Is(err, fs.ErrNotExist) {
		return absent
	}
	return "error " + err.Error()
}

func contentAnswer(h Hash) string { return "content " + hex.EncodeToString(h[:]) }

// contentOf returns the hash an answer of contentAnswer holds, or false.
func contentOf(answer string) (Hash, bool) {
	const prefix = "content "
	if len(answer) != len(prefix)+64 || answer[:len(prefix)] != prefix {
		return Hash{}, false
	}
	var h Hash
	if _, err := hex.Decode(h[:], []byte(answer[len(prefix):])); err != nil {
		return Hash{}, false
	}
	return h, true
}

// readFile reads a file of the run's version and returns its content and the answer: its content's
// hash, or why it could not be read.
func (r *Run) readFile(name string) ([]byte, string, error) {
	data, err := fs.ReadFile(r.fsys, name)
	if err != nil {
		return nil, answerOf(err), err
	}
	answer := contentAnswer(sha256.Sum256(data))
	r.remember("file", name, answer)
	return data, answer, nil
}

// stat answers whether name exists, and as what: a directory, or a file of some size.
func (r *Run) stat(name string) (fs.FileInfo, string, error) {
	info, err := fs.Stat(r.fsys, name)
	if err != nil {
		return nil, answerOf(err), err
	}
	if info.IsDir() {
		return info, isDir, nil
	}
	return info, "file " + strconv.FormatInt(info.Size(), 10), nil
}

// readDir lists a directory and answers with the hash of its entries' names and kinds.
func (r *Run) readDir(name string) ([]fs.DirEntry, string, error) {
	entries, err := fs.ReadDir(r.fsys, name)
	if err != nil {
		return nil, answerOf(err), err
	}
	h := sha256.New()
	for _, e := range entries {
		kind := "f"
		if e.IsDir() {
			kind = "d"
		}
		h.Write([]byte(kind + e.Name() + "\x00"))
	}
	return entries, "entries " + hex.EncodeToString(h.Sum(nil)), nil
}

// answer is the answer the run's version gives to a recorded read, for checking a remembered
// result against another version.
func (r *Run) answer(op, name string) string {
	if a, ok := r.recalled(op, name); ok {
		return a
	}
	var a string
	switch op {
	case "file":
		_, a, _ = r.readFile(name)
	case "stat":
		_, a, _ = r.stat(name)
	case "dir":
		_, a, _ = r.readDir(name)
	case "config":
		a = r.cfgAnswer
	default:
		panic("reuse: unknown read " + op)
	}
	r.remember(op, name, a)
	return a
}

// recordingFS is a version read through a reading stage: every read it serves is recorded with
// its answer.
type recordingFS struct{ rec *recorder }

func (f recordingFS) record(op, name, answer string) {
	f.rec.add(read{op: op, arg: name, answer: answer})
}

func (f recordingFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	run := f.rec.run
	info, answer, err := run.stat(name)
	if err != nil {
		f.record("stat", name, answer)
		return nil, &fs.PathError{Op: "open", Path: name, Err: err}
	}
	if info.IsDir() {
		entries, answer, err := run.readDir(name)
		f.record("dir", name, answer)
		if err != nil {
			return nil, &fs.PathError{Op: "open", Path: name, Err: err}
		}
		return &dirFile{info: info, entries: entries}, nil
	}
	data, answer, err := run.readFile(name)
	f.record("file", name, answer)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: err}
	}
	return &memFile{info: info, r: bytes.NewReader(data)}, nil
}

func (f recordingFS) ReadFile(name string) ([]byte, error) {
	data, answer, err := f.rec.run.readFile(name)
	f.record("file", name, answer)
	return data, err
}

func (f recordingFS) Stat(name string) (fs.FileInfo, error) {
	info, answer, err := f.rec.run.stat(name)
	f.record("stat", name, answer)
	return info, err
}

func (f recordingFS) ReadDir(name string) ([]fs.DirEntry, error) {
	entries, answer, err := f.rec.run.readDir(name)
	f.record("dir", name, answer)
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
