// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml/parser"
)

// loader reads one file and the files it extends, depth first, so earlier layers come first.
type loader struct {
	layers   []string
	visiting map[string]bool
	lines    map[string]int // of the first layer read: .hadome.yml itself
	// extendable, when not nil, holds the only entries of the product layer's extends that are read.
	extendable map[string]bool
	// extends is the product layer's own extends, kept so that versions can be compared on it.
	extends []any
}

// layer reads one file. The errors of a file outside the repository give keys and lines but never
// its values: the file is whatever the configuration names, and errors end up in public CI logs.
func (l *loader) layer(name string, data []byte, dir string, outside bool) (map[string]any, error) {
	v := &validator{file: name, lines: map[string]int{}, outside: outside}
	product := l.lines == nil
	if product {
		l.lines = v.lines
	}
	f, err := parser.ParseBytes(data, 0)
	if err != nil {
		problem := "is not valid YAML: " + firstLine(err.Error())
		if outside {
			problem = "is not valid YAML"
		}
		return nil, Errors{{File: name, Line: 1, Key: "(file)", Problem: problem}}
	}
	raw := map[string]any{}
	switch {
	case len(f.Docs) > 1:
		v.errs = append(v.errs, Error{File: name, Line: 1, Key: "(file)", Problem: "must hold one YAML document, not several"})
	case len(f.Docs) == 0 || f.Docs[0].Body == nil:
		v.fail(nil, "", "is empty", Spec)
	default:
		if m, ok := v.value(f.Docs[0].Body, "", Spec).(map[string]any); ok {
			raw = m
		}
	}
	if len(v.errs) > 0 {
		return nil, v.errs
	}
	exts, _ := raw["extends"].([]any)
	if product {
		l.extends = exts
	}
	merged, err := l.extendAll(name, exts, dir, v, product)
	if err != nil {
		return nil, err
	}
	delete(raw, "extends")
	l.layers = append(l.layers, name)
	return merge(merged, raw, Spec), nil
}

// extendAll reads the layers a file extends and merges them, earliest first. Of the product layer's,
// it reads only the extendable ones; a URL is refused whether it would be read or not.
func (l *loader) extendAll(name string, exts []any, dir string, v *validator, product bool) (map[string]any, error) {
	merged := map[string]any{}
	for i, e := range exts {
		ref, line := e.(string), v.lines[fmt.Sprintf("extends[%d]", i)]
		if strings.Contains(ref, "://") {
			return nil, extendsError(name, line, fmt.Sprintf("%s is a URL; hadome never reads configuration from the network. "+
				"Check the shared file out locally and point at it", v.shown(strconv.Quote(ref))))
		}
		if product && l.extendable != nil && !l.extendable[ref] {
			continue
		}
		base, err := l.extend(name, ref, dir, line, v)
		if err != nil {
			return nil, err
		}
		merged = merge(merged, base, Spec)
	}
	return merged, nil
}

func extendsError(from string, line int, problem string) error {
	return Errors{{File: from, Line: line, Key: "extends", Problem: problem, Example: "extends: [../shared/hadome.yml]"}}
}

func (l *loader) extend(from, ref, dir string, line int, v *validator) (map[string]any, error) {
	p := ref
	if !filepath.IsAbs(p) {
		p = filepath.Join(dir, p)
	}
	p = filepath.Clean(p)
	if l.visiting[p] {
		return nil, extendsError(from, line, fmt.Sprintf("%s extends itself through a cycle", v.shown(strconv.Quote(ref))))
	}
	data, err := readShared(p)
	if err != nil {
		return nil, extendsError(from, line, fmt.Sprintf("cannot read %s: %v", v.shown(strconv.Quote(ref)), err))
	}
	l.visiting[p] = true
	defer delete(l.visiting, p)
	return l.layer(p, data, filepath.Dir(p), true)
}

// readShared reads a shared layer. It follows symbolic links, since shared files live outside the
// repository where a link is an ordinary way to place one, but it reads only a regular file and no
// more than MaxFileSize: a FIFO would block, and a device could be read without end.
func readShared(p string) ([]byte, error) {
	info, err := os.Stat(p)
	if err != nil {
		return nil, errors.Unwrap(err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	if info.Size() > MaxFileSize {
		return nil, errors.New(tooLarge)
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, errors.Unwrap(err)
	}
	defer f.Close()
	// The size checked above bounds what is read, even if the file grows meanwhile.
	return io.ReadAll(io.LimitReader(f, info.Size()))
}

// MaxFileSize is the most hadome reads of one configuration file.
const MaxFileSize = 1 << 20

const tooLarge = "larger than 1 MiB, the most hadome reads of a configuration file"

func firstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	return s
}

// merge lays over on top of under. Objects merge key by key; lists of objects with an identity
// replace entries with the same identity and keep the rest; lists marked OmittedIsWidest are
// replaced as a whole; other lists are joined without duplicates; anything else is replaced.
func merge(under, over map[string]any, k Key) map[string]any {
	out := map[string]any{}
	for name, val := range under {
		out[name] = val
	}
	for name, val := range over {
		f := field(k, name)
		switch {
		case f.Kind == KindObject && len(f.Variants) == 0 && out[name] != nil:
			out[name] = merge(out[name].(map[string]any), val.(map[string]any), f)
		case f.Kind == KindList && !f.OmittedIsWidest && out[name] != nil:
			out[name] = joinLists(out[name].([]any), val.([]any), *f.Elem, f.Identity)
		default:
			out[name] = val
		}
	}
	return out
}

func field(k Key, name string) Key {
	for _, f := range k.Fields {
		if f.Name == name {
			return f
		}
	}
	return Key{}
}

func joinLists(under, over []any, elem Key, identity []string) []any {
	var out []any
	at := map[string]int{}
	for _, list := range [][]any{under, over} {
		for _, val := range list {
			id := identityOf(val, elem, identity)
			if i, ok := at[id]; ok {
				out[i] = val
				continue
			}
			at[id] = len(out)
			out = append(out, val)
		}
	}
	return out
}

// identityOf returns the string that identifies a list entry across layers and versions.
func identityOf(val any, elem Key, identity []string) string {
	m, ok := val.(map[string]any)
	if !ok || elem.Kind != KindObject {
		return fmt.Sprint(val)
	}
	var parts []string
	for _, name := range identity {
		if v, ok := m[name]; ok {
			parts = append(parts, name+"="+fmt.Sprint(v))
		}
	}
	return strings.Join(parts, " ")
}
