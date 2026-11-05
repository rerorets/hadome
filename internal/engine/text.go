package engine

import (
	"fmt"

	"github.com/rerorets/hadome/internal/messages"
)

// Text is wording that becomes a string only when it is written out, in the language chosen then.
// Format is the English format; the language's catalog maps it to the same wording in another
// language. Args that are themselves a Text are written in the same language.
type Text struct {
	Format string
	Args   []any
}

// T is wording built like fmt.Sprintf(format, args...), written out in the language chosen later.
func T(format string, args ...any) Text { return Text{Format: format, Args: args} }

// Verbatim is wording hadome did not write, such as a product-specific gate's message. It is never
// translated: its format holds no words, and the catalog has no entry without words.
func Verbatim(s string) Text { return T("%s", s) }

// In writes the wording in lang ("en" or "ja"). Wording the catalog lacks, or has as empty, comes
// out in English.
func (t Text) In(lang string) string { return t.in(lang, messages.Catalog(lang)) }

func (t Text) in(lang string, catalog map[string]string) string {
	format := t.Format
	if translated := catalog[format]; translated != "" {
		format = translated
	}
	if len(t.Args) == 0 {
		return format
	}
	args := make([]any, len(t.Args))
	for i, a := range t.Args {
		if inner, ok := a.(Text); ok {
			a = inner.in(lang, catalog)
		}
		args[i] = a
	}
	return fmt.Sprintf(format, args...)
}
