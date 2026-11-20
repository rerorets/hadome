// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

package golang

import (
	"testing"
	"testing/fstest"
)

// viewpoint: behaviour
// target: golang.Adapter.Analyze
// Design "Build conditions": a name declared in files split by build conditions is read in the one a
// build can compile with the calling test, by //go:build or by the file name; a test any build can
// compile reads the first in path order.
func TestNamesAreReadInTheFileABuildCompilesWithTheTest(t *testing.T) {
	got := callsByTest(t, fstest.MapFS{
		"go.mod": {Data: []byte("module example.com/app\n")},
		"store/fast.go": {Data: []byte("//go:build cgo\n\npackage store\n\ntype Fast struct{}\n\nfunc (Fast) Put() {}\n\n" +
			"func Open() *Fast { return &Fast{} }\n\nvar Shared Fast\n")},
		"store/slow.go": {Data: []byte("//go:build !cgo\n\npackage store\n\ntype Slow struct{}\n\nfunc (Slow) Put() {}\n\n" +
			"func Open() *Slow { return &Slow{} }\n\nvar Shared Slow\n")},
		"store/os_linux.go":   {Data: []byte("package store\n\ntype Disk struct{}\n\nfunc (Disk) Sync() {}\n\nfunc Native() Disk { return Disk{} }\n")},
		"store/os_windows.go": {Data: []byte("package store\n\ntype Volume struct{}\n\nfunc (Volume) Sync() {}\n\nfunc Native() Volume { return Volume{} }\n")},
		"store/cgo_test.go": {Data: []byte("//go:build cgo\n\npackage store\n\nimport \"testing\"\n\n" +
			"func TestFast(t *testing.T) { Open().Put(); Shared.Put() }\n")},
		"store/nocgo_test.go": {Data: []byte("//go:build !cgo\n\npackage store\n\nimport \"testing\"\n\n" +
			"func TestSlow(t *testing.T) { Open().Put(); Shared.Put() }\n")},
		"store/sync_windows_test.go": {Data: []byte("package store\n\nimport \"testing\"\n\nfunc TestVolume(t *testing.T) { Native().Sync() }\n")},
		"store/any_test.go":          {Data: []byte("package store\n\nimport \"testing\"\n\nfunc TestAny(t *testing.T) { Open().Put() }\n")},
	})
	want := "TestAny calls=[example.com/app/store.Fast.Put×1 example.com/app/store.Open×1] unresolved=0\n" +
		"TestFast calls=[example.com/app/store.Fast.Put×2 example.com/app/store.Open×1] unresolved=0\n" +
		"TestSlow calls=[example.com/app/store.Open×1 example.com/app/store.Slow.Put×2] unresolved=0\n" +
		"TestVolume calls=[example.com/app/store.Native×1 example.com/app/store.Volume.Sync×1] unresolved=0"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}
