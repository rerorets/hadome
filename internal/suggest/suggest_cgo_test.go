//go:build cgo

package suggest_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/adapter"
	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/suggest"
)

// viewpoint: behaviour
// target: suggest.Candidates
// In a TypeScript repository, the entry points are what the framework serves by the file's name
// (SvelteKit's +server.ts and the actions of +page.server.ts) and the handlers given to a router
// with a path. A function that writes through the database handle of a module is a candidate when
// one of them reaches it; one that only builds a value is not, even in a module that does input or
// output, and neither is one that writes but nothing reaches.
func TestTypeScriptIdempotentCandidatesFollowEntryPoints(t *testing.T) {
	tree := fstest.MapFS{
		"package.json":         {Data: []byte(`{"name":"app"}`)},
		"src/lib/server/db.ts": {Data: []byte("import pg from 'pg'\n\nexport const db = new pg.Pool()\n")},
		"src/lib/server/orders.ts": {Data: []byte("import { db } from './db'\n\n" +
			"export async function createOrder(id: string) { await db.query('insert', [id]) }\n" +
			"export async function addNote(id: string) { await db.query('insert', [id]) }\n" +
			"export async function insertBackfill(id: string) { await db.query('insert', [id]) }\n" +
			"export function createAuthCookie(user: string) { return `session=${user}` }\n" +
			"export function registerHelpers() { return [createAuthCookie] }\n")},
		"src/routes/orders/+server.ts": {Data: []byte("import { createOrder } from '$lib/server/orders'\n\n" +
			"export async function POST({ request }) { await createOrder(await request.text()); return new Response() }\n")},
		"src/routes/notes/+page.server.ts": {Data: []byte("import { addNote, createAuthCookie } from '$lib/server/orders'\n\n" +
			"export const actions = { default: async ({ request }) => { createAuthCookie('u'); await addNote(await request.text()) } }\n")},
		"svelte.config.js": {Data: []byte("export default {}\n")},
		"tsconfig.json":    {Data: []byte(`{"compilerOptions":{"paths":{"$lib":["./src/lib"],"$lib/*":["./src/lib/*"]}}}`)},
	}
	a, err := adapter.New().Analyze(engine.Version{FS: tree}, config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, cd := range suggest.Candidates(engine.Input{Facts: a, Config: config.Config{}}) {
		if cd.Type == "idempotent" {
			got = append(got, cd.Key())
		}
	}
	want := "idempotent server.addNote\nidempotent server.createOrder"
	if strings.Join(got, "\n") != want {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), want)
	}
}

// viewpoint: behaviour
// target: suggest.Candidates
// An Express-style router call with a path makes the function handed to it an entry point, and a
// function written in place there reaches what it calls.
func TestTypeScriptRouterHandlersAreEntryPoints(t *testing.T) {
	tree := fstest.MapFS{
		"package.json": {Data: []byte(`{"name":"app"}`)},
		"src/store.ts": {Data: []byte("import pg from 'pg'\n\nconst pool = new pg.Pool()\n\n" +
			"export async function createUser(name: string) { await pool.query('insert', [name]) }\n" +
			"export async function addTag(name: string) { await pool.query('insert', [name]) }\n" +
			"export async function submitReport(name: string) { await pool.query('insert', [name]) }\n")},
		"src/app.ts": {Data: []byte("import express from 'express'\nimport { createUser, addTag } from './store'\n\n" +
			"const app = express()\n" +
			"app.post('/users', createUser)\n" +
			"app.post('/tags', async (req, res) => { await addTag(req.body) })\n")},
	}
	a, err := adapter.New().Analyze(engine.Version{FS: tree}, config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, cd := range suggest.Candidates(engine.Input{Facts: a, Config: config.Config{}}) {
		got = append(got, cd.Key())
	}
	want := "idempotent src.addTag\nidempotent src.createUser"
	if strings.Join(got, "\n") != want {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), want)
	}
}
