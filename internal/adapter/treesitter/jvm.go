//go:build cgo

package treesitter

import (
	"path"
	"regexp"
	"strings"
	"unicode"

	"github.com/rerorets/hadome/internal/facts"
)

// jvm is what Java and Kotlin share: a unit is a package, imports name packages and classes, and
// test code lives under src/test/.
type jvm struct {
	packages map[string]string          // package -> its directory
	classes  map[string]map[string]bool // package -> class names of product code
	scopes   map[string]int
}

var jvmTestDir = regexp.MustCompile(`(^|/)src/[A-Za-z]*[tT]est[A-Za-z]*/`)

func (j *jvm) reset() {
	j.packages, j.classes, j.scopes = map[string]string{}, map[string]map[string]bool{}, map[string]int{}
}

// place sets a file's unit from its package declaration, and records the package and its classes.
func (j *jvm) place(f *file, pkg string, classes []string) {
	f.test = jvmTestDir.MatchString(f.path) || f.testGlob
	f.unitPath = path.Dir(f.path)
	f.unit = pkg
	if pkg == "" {
		f.unit = f.unitPath // the default package
	}
	if _, ok := j.packages[f.unit]; !ok || !f.test {
		j.packages[f.unit] = f.unitPath
	}
	if f.test {
		return
	}
	if j.classes[f.unit] == nil {
		j.classes[f.unit] = map[string]bool{}
	}
	for _, c := range classes {
		j.classes[f.unit][c] = true
	}
}

// resolve turns an imported name ("a.b.Store", "a.b" with a wildcard, "a.b.Store.create" for a
// static import) into its package and whether it is the repository's, and the class it names.
func (j *jvm) resolve(name string, wildcard bool) (pkg, class string, internal bool) {
	parts := strings.Split(name, ".")
	for i := len(parts); i > 0; i-- {
		p := strings.Join(parts[:i], ".")
		if _, ok := j.packages[p]; ok {
			if i < len(parts) {
				class = parts[i]
			}
			return p, class, true
		}
	}
	if wildcard {
		return name, "", false
	}
	for i, part := range parts { // the package ends before the first capitalised element
		if part != "" && unicode.IsUpper(rune(part[0])) {
			return strings.Join(parts[:i], "."), part, false
		}
	}
	return strings.Join(parts[:len(parts)-1], "."), "", false
}

// jvmTables are the tables Java and Kotlin share, with what each language adds.
func jvmTables(extraIO, extraMocks []string, extraCalls map[string]string) *tables {
	t := &tables{
		calls: map[string]facts.HazardCategory{},
		mocks: append([]string{"org.mockito", "org.easymock", "org.jmock"}, extraMocks...),
		io: append([]string{"java.io", "java.nio.file", "java.nio.channels", "java.net", "java.sql", "javax.sql",
			"java.lang.ProcessBuilder", "okhttp3", "org.apache.http", "redis.clients", "com.mongodb"}, extraIO...),
	}
	for _, c := range []string{"Instant.now", "LocalDate.now", "LocalDateTime.now", "LocalTime.now", "ZonedDateTime.now",
		"OffsetDateTime.now", "System.currentTimeMillis", "System.nanoTime", "Clock.systemUTC", "Clock.systemDefaultZone", "new Date"} {
		t.calls[c] = facts.HazardClock
	}
	t.calls["Thread.sleep"] = facts.HazardSleep
	for k, v := range extraCalls {
		t.calls[k] = facts.HazardCategory(v)
	}
	return t
}

// jvmDynamic are the calls that load the class their argument names.
var jvmDynamic = set("Class.forName", "java.lang.Class.forName")
