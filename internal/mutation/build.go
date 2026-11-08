package mutation

import (
	"go/build"
	"io"
	"io/fs"
	"path"
	"strings"
)

// buildContext is the Go build that the test command compiles: go/build's default for the
// environment hadome runs in, changed by CGO_ENABLED=, GOOS=, GOARCH= and -tags written in the
// command itself. Anything set inside a script the command calls is not seen.
func buildContext(test []string) build.Context {
	ctx := build.Default
	ctx.BuildTags = append([]string(nil), ctx.BuildTags...)
	for i, a := range test {
		switch {
		case strings.HasPrefix(a, "CGO_ENABLED="):
			ctx.CgoEnabled = strings.TrimPrefix(a, "CGO_ENABLED=") == "1"
		case strings.HasPrefix(a, "GOOS="):
			ctx.GOOS = strings.TrimPrefix(a, "GOOS=")
		case strings.HasPrefix(a, "GOARCH="):
			ctx.GOARCH = strings.TrimPrefix(a, "GOARCH=")
		case strings.HasPrefix(a, "-tags=") || strings.HasPrefix(a, "--tags="):
			ctx.BuildTags = append(ctx.BuildTags, tags(a[strings.Index(a, "=")+1:])...)
		case (a == "-tags" || a == "--tags") && i+1 < len(test):
			ctx.BuildTags = append(ctx.BuildTags, tags(test[i+1])...)
		}
	}
	return ctx
}

// tags splits a -tags value; go accepts commas and, in older releases, spaces.
func tags(v string) []string {
	return strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' })
}

// unbuilt reports whether ctx leaves out the Go file at p in fsys: its //go:build line, its
// _GOOS/_GOARCH name, or a name starting with _ or . keep it out of the build. Files of other
// languages are never reported.
func unbuilt(ctx build.Context, fsys fs.FS, p string) bool {
	if !strings.HasSuffix(p, ".go") {
		return false
	}
	ctx.JoinPath = path.Join
	ctx.OpenFile = func(name string) (io.ReadCloser, error) { return fsys.Open(name) }
	ok, err := ctx.MatchFile(path.Dir(p), path.Base(p))
	return err == nil && !ok
}
