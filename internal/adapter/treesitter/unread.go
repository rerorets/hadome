//go:build !cgo

package treesitter

import (
	"io/fs"

	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
)

// unread claims a language's files and lists them as unread: this build has no reader for them.
type unread struct{ lang string }

// Languages returns, in a build without cgo, readers that list each language's files as unread.
func Languages() []unread {
	out := make([]unread, len(names))
	for i, n := range names {
		out[i] = unread{lang: n}
	}
	return out
}

// Name is the language's name in configuration.
func (u unread) Name() string { return u.lang }

// Claims reports whether the file is in the language.
func (u unread) Claims(p string) bool { return claims(u.lang, p) }

// Analyze lists the files, marked unread.
func (u unread) Analyze(v engine.Version, _ config.Config, paths []string) (*facts.Analysis, error) {
	a := &facts.Analysis{}
	for _, p := range paths {
		src, err := fs.ReadFile(v.FS, p)
		if err != nil {
			return nil, err
		}
		a.Files = append(a.Files, facts.File{Path: p, Language: u.lang, Role: facts.RoleOther,
			Lines: facts.CountLines(src), Unread: true})
	}
	return a, nil
}
