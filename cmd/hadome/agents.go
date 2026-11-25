// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

package main

import (
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/rerorets/hadome/internal/engine"
)

const (
	beginMarker = "<!-- hadome:begin -->"
	endMarker   = "<!-- hadome:end -->"
)

// The blocks that tell agents about hadome. The English one is the README quick start's block,
// word for word; a test keeps the two equal.
var (
	//go:embed agents.en.md
	agentBlock string
	//go:embed agents.ja.md
	agentBlockJa string
)

func agents(args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("agents", flag.ContinueOnError)
	fl.SetOutput(stderr)
	lang := fl.String("lang", "en", "language of the block: en or ja")
	write := fl.String("write", "", "put the block into this file, and into each file listed after it, instead of printing it")
	rest, err := parseAnywhere(fl, args)
	if err != nil || (*write == "" && len(rest) != 0) {
		return engine.ExitConfig
	}
	if !validLang(*lang, stderr) {
		return engine.ExitConfig
	}
	block := agentBlock
	if *lang == "ja" {
		block = agentBlockJa
	}
	if *write == "" {
		fmt.Fprint(stdout, block)
		return engine.ExitOK
	}
	paths := append([]string{*write}, rest...)
	// Every file is checked before any is written, so that a failure leaves no file half done.
	texts := make([]string, len(paths))
	targets := make([]string, len(paths))
	// Each file is printed by its absolute path, so that a person sees which directory it is in:
	// CLAUDE.md written from the home directory instead of a repository is read by every project
	// below it.
	shown := make([]string, len(paths))
	done := make([]string, len(paths))
	failed := false
	for i, p := range paths {
		var err error
		if shown[i], err = filepath.Abs(p); err == nil {
			targets[i], err = writeTarget(p)
		}
		if err != nil {
			fmt.Fprintln(stderr, "hadome:", err)
			failed = true
			continue
		}
		if texts[i], done[i], err = withBlock(targets[i], block); err != nil {
			fmt.Fprintln(stderr, "hadome:", err)
			failed = true
		}
	}
	if failed {
		return engine.ExitConfig
	}
	for i := range paths {
		if done[i] != unchanged {
			if err := replaceFile(targets[i], []byte(texts[i])); err != nil {
				fmt.Fprintln(stderr, "hadome:", err)
				return engine.ExitConfig
			}
		}
		fmt.Fprintf(stdout, done[i]+"\n", shown[i])
	}
	return engine.ExitOK
}

// writeTarget returns the file to write for path: path itself, or the file a symbolic link at path
// leads to when that is a regular file under the working directory, as when CLAUDE.md links to the
// instructions another agent reads. Any other link is refused, so that a write never lands outside
// the repository.
func writeTarget(path string) (string, error) {
	if info, err := os.Lstat(path); err != nil || info.Mode()&fs.ModeSymlink == 0 {
		return path, nil
	}
	refused := fmt.Errorf("%s is a symbolic link that does not lead to a regular file in this directory; hadome writes only inside the repository, so name the file itself", path)
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", refused
	}
	root, err := filepath.EvalSymlinks(".")
	if err != nil {
		return "", err
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return "", err
	}
	if root, err = filepath.Abs(root); err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", refused
	}
	if info, err := os.Lstat(target); err != nil || !info.Mode().IsRegular() {
		return "", refused
	}
	return target, nil
}

// What writing the block did to a file, as the line hadome prints for it.
const (
	created   = "ok: wrote %s with the hadome block"
	appended  = "ok: added the hadome block to %s"
	replaced  = "ok: updated the hadome block in %s"
	unchanged = "ok: the hadome block in %s is already up to date"
)

// withBlock returns the file's text with the block in it: between the markers, appended when the
// markers are absent, or alone when the file does not exist. Running it again changes nothing.
// The second result says which of these it did. A missing directory is an error, so that a typo
// never creates one.
func withBlock(path, block string) (string, string, error) {
	if info, err := os.Stat(filepath.Dir(path)); err != nil || !info.IsDir() {
		return "", "", fmt.Errorf("%s: the directory %s does not exist", path, filepath.Dir(path))
	}
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return "", "", fmt.Errorf("%s is not a regular file; hadome writes only to regular files, so name the file itself", path)
	}
	old, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return block, created, nil
	}
	if err != nil {
		return "", "", err
	}
	text := string(old)
	begin, end := strings.Index(text, beginMarker), strings.Index(text, endMarker)
	switch {
	case begin >= 0 && end > begin:
		rest := strings.TrimPrefix(text[end+len(endMarker):], "\n")
		if text = text[:begin] + block + rest; text == string(old) {
			return text, unchanged, nil
		}
		return text, replaced, nil
	case begin >= 0 || end >= 0:
		return "", "", fmt.Errorf("%s has only one of %s and %s; fix it by hand", path, beginMarker, endMarker)
	case text == "":
		return block, appended, nil
	default:
		return strings.TrimRight(text, "\n") + "\n\n" + block, appended, nil
	}
}
