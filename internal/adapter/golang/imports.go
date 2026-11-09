package golang

import (
	"go/token"
	"strconv"
	"strings"

	"github.com/rerorets/hadome/internal/facts"
)

// builtinIO lists the units that perform input or output. An entry ending in "/..." covers the unit
// and everything under it. `time` is left out: it also holds plain values such as time.Duration.
// "C" is cgo, through which a unit can do anything.
var builtinIO = []string{
	"C", "os", "os/exec", "os/signal", "os/user", "io/ioutil", "syscall", "plugin", "log/syslog",
	"net", "net/http", "net/http/httptrace", "net/mail", "net/rpc/...", "net/smtp", "crypto/tls", "crypto/rand",
	"database/sql", "github.com/jmoiron/sqlx", "golang.org/x/sys/...", "golang.org/x/net/...",
	"github.com/jackc/pgx/...", "github.com/lib/pq", "github.com/go-sql-driver/mysql", "github.com/mattn/go-sqlite3",
	"modernc.org/sqlite", "go.mongodb.org/mongo-driver/...", "github.com/redis/go-redis/...",
	"google.golang.org/grpc/...", "cloud.google.com/go/...", "github.com/aws/aws-sdk-go-v2/...",
}

// ioTable answers whether a unit performs input or output.
type ioTable struct {
	exact    map[string]bool
	prefixes []string // "example.com/x/" for an entry "example.com/x/..."
}

func newIOTable(added []string) ioTable {
	t := ioTable{exact: map[string]bool{}}
	for _, e := range append(append([]string{}, builtinIO...), added...) {
		if base, ok := strings.CutSuffix(e, "/..."); ok {
			t.exact[base] = true
			t.prefixes = append(t.prefixes, base+"/")
		} else {
			t.exact[e] = true
		}
	}
	return t
}

func (t ioTable) has(unit string) bool {
	if t.exact[unit] {
		return true
	}
	for _, p := range t.prefixes {
		if strings.HasPrefix(unit, p) {
			return true
		}
	}
	return false
}

// importsOf lists the imports of one non-test file.
func importsOf(fset *token.FileSet, pf parsedFile, idx *index, io ioTable) []facts.Import {
	var out []facts.Import
	for _, s := range pf.file.Imports {
		p, err := strconv.Unquote(s.Path.Value)
		if err != nil {
			continue
		}
		out = append(out, facts.Import{
			Path: pf.path, Line: fset.Position(s.Pos()).Line, FromUnit: pf.pkgPath, ToUnit: p,
			Internal: idx.internal(p), IO: io.has(p),
		})
	}
	return out
}
