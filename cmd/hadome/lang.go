package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/rerorets/hadome/internal/config"
)

// validLang reports whether --lang names a language hadome writes in, and says so on stderr when it
// does not. Every command's --lang accepts the same values as `messages` in the configuration.
func validLang(lang string, stderr io.Writer) bool {
	var names []string
	for _, v := range config.MessageLanguages {
		if v.Name == lang {
			return true
		}
		names = append(names, v.Name)
	}
	fmt.Fprintf(stderr, "hadome: --lang must be %s, got %q\n", strings.Join(names, " or "), lang)
	return false
}

// pickLang chooses the language of the wording: the command's --lang, then the configuration's
// `messages`, then English. Neither the environment nor the locale takes part, so that a check
// writes the same words on every machine.
func pickLang(flagValue, configured string) string {
	switch {
	case flagValue != "":
		return flagValue
	case configured != "":
		return configured
	default:
		return "en"
	}
}
