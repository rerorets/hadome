//go:build cgo

package treesitter

import (
	"strings"
)

// The helpers a Java or Kotlin test calls by bare names, and the top-level functions of Kotlin test
// sources among them.

// helper finds the body of a helper a test calls by a bare name: a method of the test's class, one
// of a class it extends, or a top-level function of the test sources. Kotlin resolves a bare name
// to a member before a top-level function.
func (w *jvmWalk) helper(name string) jvmCode {
	if body, ok := w.jf.helpers[name]; ok {
		return jvmCode{w.jf, body}
	}
	if h, ok := w.inherited[name]; ok {
		return h
	}
	if d, ok := w.jf.topHelpers[name]; ok {
		return jvmCode{w.jf.fileOf(d.f), ktBody(d.n)}
	}
	return jvmCode{}
}

// fileOf is the reading of a Kotlin test file whose top-level function a test of this file calls.
func (jf *jvmFile) fileOf(f *file) *jvmFile {
	if f == jf.f {
		return jf
	}
	if jf.others == nil {
		jf.others = map[*file]*jvmFile{}
	}
	if o, ok := jf.others[f]; ok {
		return o
	}
	o := jf.kt.testFile(f, jf.t)
	jf.others[f] = o
	return o
}

// bindTopHelpers binds the top-level functions of test code that the file's tests can call by bare
// names: those of its package, and those it imports, which then name no target of the product.
func (jf *jvmFile) bindTopHelpers() {
	jf.topHelpers = map[string]jvmDecl{}
	for name, d := range jf.kt.testFunctions[jf.f.unit] {
		jf.topHelpers[name] = d
	}
	ktImports(jf.f, func(name string, wildcard bool, _ *node) {
		if wildcard {
			for fn, d := range jf.kt.testFunctions[name] {
				jf.topHelpers[fn] = d
			}
			return
		}
		fn := lastElem(name)
		if d, ok := jf.kt.testFunctions[strings.TrimSuffix(name, "."+fn)][fn]; ok {
			jf.topHelpers[fn] = d
			delete(jf.names, fn)
		}
	})
}
