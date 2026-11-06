//go:build cgo

package tests

import (
	"testing/fstest"

	"github.com/rerorets/hadome/internal/engine"
)

func init() { languageGuardsCases = driveCases() }

// driveCases are tests that drive the application through an HTTP test client: they call no
// function of the repository, but exercise it.
func driveCases() []engine.Case {
	ts := func(test string) fstest.MapFS {
		return fstest.MapFS{
			"package.json":     {Data: []byte(`{"name":"app"}`)},
			"src/rows.ts":      {Data: []byte("export function rows(): number[] { return [1, 2] }\n")},
			"src/rows.test.ts": {Data: []byte(test)},
		}
	}
	const base = "import { rows } from './rows'\n\nit('lists', () => { expect(rows()).toHaveLength(2) })\n"
	const drive = "import request from 'supertest'\n\nit('serves rows', async () => {\n  await request('http://localhost:3000').get('/rows').expect(200)\n})\n"
	const notDrive = "import { request } from 'undici'\n\nit('serves rows', async () => {\n  await request('http://localhost:3000')\n})\n"
	return []engine.Case{
		{Name: "a test that drives the application through an HTTP test client passes", Base: ts(base), Head: ts(base + "\n" + drive)},
		{Name: "a call that is only named like a client is not one", Base: ts(base), Head: ts(base + "\n" + notDrive),
			WantFail: []string{"tests/guards-nothing"}},
	}
}
