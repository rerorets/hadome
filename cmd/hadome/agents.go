package main

import (
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
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
	write := fl.String("write", "", "put the block into this file instead of printing it")
	if err := fl.Parse(args); err != nil || fl.NArg() != 0 {
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
	if err := writeBlock(*write, block); err != nil {
		fmt.Fprintln(stderr, "hadome:", err)
		return engine.ExitConfig
	}
	fmt.Fprintf(stdout, "ok: the hadome block in %s is up to date\n", *write)
	return engine.ExitOK
}

// writeBlock replaces the block between the markers, appends it when the markers are absent, or
// creates the file. Running it again changes nothing.
func writeBlock(path, block string) error {
	old, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	text := string(old)
	begin, end := strings.Index(text, beginMarker), strings.Index(text, endMarker)
	switch {
	case begin >= 0 && end > begin:
		rest := strings.TrimPrefix(text[end+len(endMarker):], "\n")
		text = text[:begin] + block + rest
	case begin >= 0 || end >= 0:
		return fmt.Errorf("%s has only one of %s and %s; fix it by hand", path, beginMarker, endMarker)
	case text == "":
		text = block
	default:
		text = strings.TrimRight(text, "\n") + "\n\n" + block
	}
	return os.WriteFile(path, []byte(text), 0o644)
}
