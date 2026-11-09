//go:build cgo

package treesitter

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/facts"
)

// entryList writes the entries of an analysis as "<symbol>" or "calls:<ids>", sorted.
func entryList(a *facts.Analysis) string {
	var out []string
	for _, e := range a.Entries {
		if e.Symbol != "" {
			out = append(out, e.Symbol)
		} else {
			out = append(out, "calls:"+strings.Join(e.Calls, ","))
		}
	}
	sort.Strings(out)
	return strings.Join(out, " ")
}

// reachOf writes what one symbol reaches.
func reachOf(t *testing.T, a *facts.Analysis, id string) string {
	t.Helper()
	for _, s := range a.Symbols {
		if s.ID == id {
			return fmt.Sprintf("io=%v calls=%v units=%q", s.IO, s.Calls, s.Units)
		}
	}
	t.Fatalf("no symbol %s", id)
	return ""
}

// viewpoint: behaviour
// target: typescript.trace
// TypeScript's framework entry points are the exports a file's name says the framework serves: a
// Next.js route's verbs and an API page's default export, a 'use server' file's functions, a
// SvelteKit server file's fallback; sqs-consumer's handleMessage is a consumer's handler.
func TestTypeScriptFrameworkEntryPoints(t *testing.T) {
	a := analyze(t, "typescript", fstest.MapFS{
		"package.json":            {Data: []byte(`{"name":"app"}`)},
		"app/orders/route.ts":     {Data: []byte("export async function POST() {}\nexport function helper() {}\n")},
		"pages/api/notes.ts":      {Data: []byte("export default function handler() {}\n")},
		"lib/actions.ts":          {Data: []byte("'use server'\n\nexport async function save() {}\n")},
		"lib/plain.ts":            {Data: []byte("export async function save() {}\n")},
		"src/routes/x/+server.ts": {Data: []byte("export function fallback() {}\nexport function other() {}\n")},
		"src/sqs.ts": {Data: []byte("import { Consumer } from 'sqs-consumer'\n\nexport async function take() {}\n\n" +
			"Consumer.create({ queueUrl: 'q', handleMessage: take })\n")},
	})
	want := "app/orders.POST lib/actions.save pages/api.handler src.take src/routes/x.fallback"
	if got := entryList(a); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

// viewpoint: behaviour
// target: python.trace
// Django's class-based views (as_view) and the view sets a router registers are entry points by
// their request methods; a decorator without a path, or a route decorator that is a bare name, is
// none.
func TestPythonClassViewsAreEntryPoints(t *testing.T) {
	a := analyze(t, "python", fstest.MapFS{
		"pyproject.toml":  {Data: []byte("[project]\nname = \"app\"\n")},
		"app/__init__.py": {Data: []byte("")},
		"app/views.py": {Data: []byte("class OrderView:\n    def post(self, request):\n        pass\n\n    def helper(self):\n        pass\n\n\n" +
			"class NoteViewSet:\n    def create(self, request):\n        pass\n\n\n" +
			"def get(path):\n    return lambda f: f\n\n\n@get('/plain')\ndef plain():\n    pass\n\n\n" +
			"@app.post\ndef unrouted():\n    pass\n")},
		"app/urls.py": {Data: []byte("from django.urls import path\nfrom app.views import OrderView, NoteViewSet\n\n" +
			"urlpatterns = [path('orders/', OrderView.as_view())]\nrouter.register('notes', NoteViewSet)\n")},
	})
	want := "app/views.NoteViewSet.create app/views.OrderView.post"
	if got := entryList(a); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

// viewpoint: behaviour
// target: java.trace
// A Java method reaches a static method of a class of its own package, a method through a field
// read as this.field, and input or output through a class imported from a package that does it;
// a method without an entry annotation is no entry point.
func TestJavaTraceReadsFieldsAndStaticCalls(t *testing.T) {
	src := func(body string) *fstest.MapFile {
		return &fstest.MapFile{Data: []byte("package com.example.app;\n\n" + body)}
	}
	a := analyze(t, "java", fstest.MapFS{
		"src/main/java/com/example/app/Ids.java": src("public class Ids {\n  public static String next() { return \"1\"; }\n}\n"),
		"src/main/java/com/example/app/Store.java": src("import java.sql.DriverManager;\n\npublic class Store {\n" +
			"  public void open() { DriverManager.getConnection(\"x\"); }\n}\n"),
		"src/main/java/com/example/app/Service.java": src("public class Service {\n  private Store store;\n" +
			"  public void run() { Ids.next(); this.store.open(); helper(); }\n  private void helper() {}\n" +
			"  @Deprecated\n  public void old() {}\n}\n"),
	})
	if got, want := reachOf(t, a, "com.example.app.Service.run"),
		"io=false calls=[com.example.app.Ids.next com.example.app.Service.helper com.example.app.Store.open] units=[]"; got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if got := reachOf(t, a, "com.example.app.Store.open"); !strings.HasPrefix(got, "io=true") {
		t.Errorf("want io=true, got %s", got)
	}
	if got := entryList(a); got != "" {
		t.Errorf("want no entry points, got %s", got)
	}
}

// viewpoint: behaviour
// target: rust.trace
// A Rust method reaches Self::f and functions of another module through a path or a use, and a
// handler given to actix-web's .to is an entry point; a module-level const's initial value counts
// where it is used.
func TestRustTraceReadsPathsAndActixTo(t *testing.T) {
	a := analyze(t, "rust", fstest.MapFS{
		"Cargo.toml": {Data: []byte("[package]\nname = \"app\"\n")},
		"src/lib.rs": {Data: []byte("pub mod ids;\npub mod web;\n")},
		"src/ids.rs": {Data: []byte("pub fn next() -> u32 { 1 }\n")},
		"src/web.rs": {Data: []byte("use crate::ids;\n\nconst ROOT: &str = std::fs::read_to_string(\"x\");\n\npub struct Api;\n\nimpl Api {\n" +
			"    pub fn run(&self) { Self::helper(); ids::next(); crate::ids::next(); }\n" +
			"    fn helper() {}\n}\n\npub fn root() -> &'static str { ROOT }\n\n" +
			"pub async fn create() {}\n\npub fn app() { web::resource(\"/x\").route(web::post().to(create)); }\n")},
	})
	if got, want := reachOf(t, a, "src/web.Api.run"), "io=false calls=[src/ids.next src/web.Api.helper] units=[]"; got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if got := reachOf(t, a, "src/web.root"); !strings.HasPrefix(got, "io=true") {
		t.Errorf("want io=true through ROOT, got %s", got)
	}
	if got, want := entryList(a), "src/web.create"; got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}
