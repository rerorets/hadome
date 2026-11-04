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
	if err := fl.Parse(args); err != nil || (*write == "" && fl.NArg() != 0) {
		return engine.ExitConfig
	}
	block := agentBlock
	switch *lang {
	case "en":
	case "ja":
		block = agentBlockJa
	default:
		fmt.Fprintf(stderr, "hadome: --lang must be en or ja, got %q\n", *lang)
		return engine.ExitConfig
	}
	if *write == "" {
		fmt.Fprint(stdout, block)
		return engine.ExitOK
	}
	paths := append([]string{*write}, fl.Args()...)
	// Every file is checked before any is written, so that a failure leaves no file half done.
	texts := make([]string, len(paths))
	failed := false
	for i, p := range paths {
		var err error
		if texts[i], err = withBlock(p, block); err != nil {
			fmt.Fprintln(stderr, "hadome:", err)
			failed = true
		}
	}
	if failed {
		return engine.ExitConfig
	}
	for i, p := range paths {
		if err := os.WriteFile(p, []byte(texts[i]), 0o644); err != nil {
			fmt.Fprintln(stderr, "hadome:", err)
			return engine.ExitConfig
		}
		fmt.Fprintf(stdout, "ok: the hadome block in %s is up to date\n", p)
	}
	return engine.ExitOK
}

// withBlock returns the file's text with the block in it: between the markers, appended when the
// markers are absent, or alone when the file does not exist. Running it again changes nothing.
// A missing directory is an error, so that a typo never creates one.
func withBlock(path, block string) (string, error) {
	if info, err := os.Stat(filepath.Dir(path)); err != nil || !info.IsDir() {
		return "", fmt.Errorf("%s: the directory %s does not exist", path, filepath.Dir(path))
	}
	old, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	text := string(old)
	begin, end := strings.Index(text, beginMarker), strings.Index(text, endMarker)
	switch {
	case begin >= 0 && end > begin:
		rest := strings.TrimPrefix(text[end+len(endMarker):], "\n")
		text = text[:begin] + block + rest
	case begin >= 0 || end >= 0:
		return "", fmt.Errorf("%s has only one of %s and %s; fix it by hand", path, beginMarker, endMarker)
	case text == "":
		text = block
	default:
		text = strings.TrimRight(text, "\n") + "\n\n" + block
	}
	return text, nil
}
