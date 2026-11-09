//go:build cgo

package tests

import (
	"testing/fstest"

	"github.com/rerorets/hadome/internal/engine"
)

func init() {
	languageGuardsCases = append(driveCases(), supportCases()...)
	languageScopeCases = collidingCases()
	languageUnknownCases = append(missingImportCases(), browserCases()...)
}

// supportCases are tests that reach the product through a helper of a test support file, which
// is test code and no target of its own.
func supportCases() []engine.Case {
	tr := func(test string) fstest.MapFS {
		return fstest.MapFS{
			"src/rows.ts":      {Data: []byte("export function rows(): number[] { return [1, 2] }\n")},
			"test/helpers.ts":  {Data: []byte("import { rows } from '../src/rows'\n\nexport function firstRow() { return rows()[0] }\n")},
			"src/rows.test.ts": {Data: []byte(test)},
		}
	}
	const base = "import { rows } from './rows'\n\nit('lists', () => { expect(rows()).toHaveLength(2) })\n"
	const helper = "import { firstRow } from '../test/helpers'\n\nit('starts at one', () => { expect(firstRow()).toBe(1) })\n"
	withClock := func(path, test string) fstest.MapFS {
		t := tr(base)
		t["test/clock.ts"] = &fstest.MapFile{Data: []byte("export function forceZone(zone: string) {\n  process.env.TZ = zone\n}\n")}
		t[path] = &fstest.MapFile{Data: []byte(test)}
		return t
	}
	const ofHelper = "import { forceZone } from './clock'\n\nit('sets the zone', () => {\n  forceZone('UTC')\n  expect(process.env.TZ).toBe('UTC')\n})\n"
	const onlyHelper = "import { forceZone } from '../test/clock'\n\nit('runs in UTC', () => {\n  forceZone('UTC')\n  expect(process.env.TZ).toBe('UTC')\n})\n"
	return []engine.Case{
		{Name: "a test that reaches the code through a helper of a test support file passes", Base: tr(base), Head: tr(base + "\n" + helper)},
		{Name: "the own test of a test support file passes", Base: tr(base), Head: withClock("test/clock.test.ts", ofHelper)},
		{Name: "a product test that calls only a helper of a test support file fails", Base: tr(base),
			Head: withClock("src/zone.test.ts", onlyHelper), WantFail: []string{"tests/guards-nothing"}},
	}
}

// collidingCases are two files of one TypeScript directory that declare the same name: each is a
// target of its own, so the tests of one are not covered by the tests of the other.
func collidingCases() []engine.Case {
	tr := func(withB bool) fstest.MapFS {
		t := fstest.MapFS{
			"src/store/a.ts":      {Data: []byte("export function load(): string { return 'a' }\n")},
			"src/store/b.ts":      {Data: []byte("export function load(): string { return 'b' }\n")},
			"src/store/a.test.ts": {Data: []byte("import { load } from './a'\n\nit('loads a', () => { expect(load()).toBe('a') })\n")},
		}
		if withB {
			t["src/store/b.test.ts"] = &fstest.MapFile{Data: []byte("import { load } from './b'\n\nit('loads b', () => { expect(load()).toBe('b') })\n")}
		}
		return t
	}
	return []engine.Case{
		{Name: "deleting the only test of one of two same-named functions of a directory fails", Base: tr(true), Head: tr(false),
			WantFail: []string{"tests/scope-shrunk"}},
	}
}

// missingImportCases are tests whose import looks like the repository's own but names no file.
func missingImportCases() []engine.Case {
	tr := func(test string) fstest.MapFS {
		return fstest.MapFS{
			"src/rows.ts":      {Data: []byte("export function rows(): number[] { return [1, 2] }\n")},
			"src/rows.test.ts": {Data: []byte(test)},
		}
	}
	const base = "import { rows } from './rows'\n\nit('lists', () => { expect(rows()).toHaveLength(2) })\n"
	const missing = "import { cells } from './cells'\n\nit('lists cells', () => { expect(cells()).toHaveLength(2) })\n"
	return []engine.Case{
		{Name: "a test that calls only names of a local import naming no file needs review", Base: tr(base),
			Head: tr(base + "\n" + missing), WantReview: []string{"tests/target-unknown"}},
	}
}

// browserCases are Playwright tests that take the browser page: they drive the application from
// outside, through a helper of a test support file too.
func browserCases() []engine.Case {
	tr := func(test string) fstest.MapFS {
		return fstest.MapFS{
			"src/rows.ts":      {Data: []byte("export function rows(): number[] { return [1, 2] }\n")},
			"src/rows.test.ts": {Data: []byte("import { rows } from './rows'\n\nit('lists', () => { expect(rows()).toHaveLength(2) })\n")},
			"e2e/helpers.ts":   {Data: []byte("import type { Page } from '@playwright/test'\n\nexport async function signIn(page: Page) { await page.goto('/sign-in') }\n")},
			"e2e/home.spec.ts": {Data: []byte(test)},
		}
	}
	const base = "import { test, expect } from '@playwright/test'\n\ntest('opens', async ({ page }) => { await page.goto('/'); await expect(page).toHaveTitle(/x/) })\n"
	const helper = "\nimport { signIn } from './helpers'\n\ntest('signs in', async ({ page }) => { await signIn(page); await expect(page).toHaveURL(/home/) })\n"
	return []engine.Case{
		{Name: "a Playwright test that takes the browser page passes", Base: tr(""), Head: tr(base)},
		{Name: "a Playwright test that hands the page to a helper of a test support file passes", Base: tr(base), Head: tr(base + helper)},
	}
}

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
	const hono = "import { app } from './app'\n\nit('serves rows in memory', async () => {\n  expect((await app.request('/rows')).status).toBe(200)\n})\n"
	withApp := func(test string) fstest.MapFS {
		t := ts(test)
		t["src/app.ts"] = &fstest.MapFile{Data: []byte("export const app = { request: async (path: string) => ({ status: 200, path }) }\n")}
		return t
	}
	return []engine.Case{
		{Name: "a test that drives the application through an HTTP test client passes", Base: ts(base), Head: ts(base + "\n" + drive)},
		{Name: "a test that hands a request in memory to the application passes", Base: withApp(base), Head: withApp(base + "\n" + hono)},
		{Name: "a call that is only named like a client is not one", Base: ts(base), Head: ts(base + "\n" + notDrive),
			WantFail: []string{"tests/guards-nothing"}},
	}
}
