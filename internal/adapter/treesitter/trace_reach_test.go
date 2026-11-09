//go:build cgo

package treesitter

import (
	"testing"
	"testing/fstest"
)

// viewpoint: behaviour
// target: python.trace
// A Python function reaches input or output through a module value made from another module value
// that does it; a call of a module function on which a member is called still counts; a member's
// name is not read as a function of the module; a new object of a class of the module calls its
// method; an object hadome does not know calls nothing, and neither does a name of a module that
// does no input or output.
func TestPythonTraceReadsModuleValuesAndObjects(t *testing.T) {
	a := analyze(t, "python", fstest.MapFS{
		"pyproject.toml":  {Data: []byte("[project]\nname = \"app\"\n")},
		"app/__init__.py": {Data: []byte("")},
		"app/store.py": {Data: []byte("import psycopg\nfrom json import dumps\n\nengine = psycopg.connect()\npool = engine\n\n\n" +
			"def save():\n    pass\n\n\ndef get_conn():\n    return 1\n\n\n" +
			"class Local:\n    def put(self):\n        pass\n\n\n" +
			"def add_row():\n    pool.execute('x')\n\n\n" +
			"def add_other():\n    get_conn().execute('x')\n\n\n" +
			"def add_member():\n    other.save()\n\n\n" +
			"def add_local():\n    Local().put()\n\n\n" +
			"def add_json():\n    dumps(1)\n")},
	})
	for id, want := range map[string]string{
		"app/store.add_row":    `io=true calls=[] units=[]`,
		"app/store.add_other":  `io=false calls=[app/store.get_conn] units=[]`,
		"app/store.add_member": `io=false calls=[] units=[]`,
		"app/store.add_local":  `io=false calls=[app/store.Local.put] units=[]`,
		"app/store.add_json":   `io=false calls=[] units=[]`,
	} {
		if got := reachOf(t, a, id); got != want {
			t.Errorf("%s: got %s, want %s", id, got, want)
		}
	}
}

// viewpoint: behaviour
// target: python.trace
// Class views and view sets are found whether the URL configuration names them through a module
// (views.NoteViewSet) or declares them in the same file.
func TestPythonViewClassesByModuleAndFile(t *testing.T) {
	a := analyze(t, "python", fstest.MapFS{
		"pyproject.toml":  {Data: []byte("[project]\nname = \"app\"\n")},
		"app/__init__.py": {Data: []byte("")},
		"app/views.py":    {Data: []byte("class NoteViewSet:\n    def list(self, request):\n        pass\n")},
		"app/urls.py": {Data: []byte("from app import views\n\n\nclass LocalView:\n    def get(self, request):\n        pass\n\n\n" +
			"router.register('notes', views.NoteViewSet)\nurlpatterns = [path('local/', LocalView.as_view())]\n")},
	})
	if got, want := entryList(a), "app/urls.LocalView.get app/views.NoteViewSet.list"; got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

// viewpoint: behaviour
// target: kotlin.trace
// A Kotlin method calling another method of its class by its bare name calls that method.
func TestKotlinTraceReadsBareMethodCalls(t *testing.T) {
	a := analyze(t, "kotlin", fstest.MapFS{
		"src/main/kotlin/com/example/Svc.kt": {Data: []byte("package com.example\n\nclass Svc {\n    fun run() { helper() }\n    fun helper() {}\n}\n")},
	})
	if got, want := reachOf(t, a, "com.example.Svc.run"), `io=false calls=[com.example.Svc.helper] units=[]`; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

// viewpoint: behaviour
// target: java.trace
// A class imported from outside the repository that does no input or output gives nothing.
func TestJavaTraceIgnoresOtherExternalClasses(t *testing.T) {
	a := analyze(t, "java", fstest.MapFS{
		"src/main/java/com/example/Svc.java": {Data: []byte("package com.example;\n\nimport java.util.List;\n\n" +
			"public class Svc {\n  public void run() { List.of(); }\n}\n")},
	})
	if got, want := reachOf(t, a, "com.example.Svc.run"), `io=false calls=[] units=[]`; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

// viewpoint: behaviour
// target: rust.trace
// A Rust function reaches a value of another module through the name its use binds, a method of a
// type through module::Type::f, and a method of a struct of its module made with Type::new; a
// closure axum's router is given is entry code whose calls are listed.
func TestRustTraceReadsValuesTypesAndClosures(t *testing.T) {
	a := analyze(t, "rust", fstest.MapFS{
		"Cargo.toml": {Data: []byte("[package]\nname = \"app\"\n")},
		"src/lib.rs": {Data: []byte("pub mod ids;\npub mod web;\n")},
		"src/ids.rs": {Data: []byte("pub const LIMIT: u32 = 3;\n\npub struct Counter;\n\nimpl Counter {\n    pub fn bump() {}\n}\n")},
		"src/web.rs": {Data: []byte("use crate::ids;\n\npub struct Api;\n\nimpl Api {\n    pub fn new() -> Api { Api }\n    pub fn go(&self) {}\n}\n\n" +
			"pub fn limit() -> u32 { ids::LIMIT }\n\npub fn bump() { crate::ids::Counter::bump(); }\n\n" +
			"pub fn start() { Api::new().go(); }\n\npub fn make() {}\n\n" +
			"pub fn app() { Router::new().route(\"/y\", get(|| async { make() })); }\n")},
	})
	for id, want := range map[string]string{
		"src/web.limit": `io=false calls=[] units=["src/ids"]`,
		"src/web.bump":  `io=false calls=[src/ids.Counter.bump] units=[]`,
		"src/web.start": `io=false calls=[src/web.Api.go src/web.Api.new] units=[]`,
	} {
		if got := reachOf(t, a, id); got != want {
			t.Errorf("%s: got %s, want %s", id, got, want)
		}
	}
	if got, want := entryList(a), "calls:src/web.make"; got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

// viewpoint: behaviour
// target: typescript.trace
// A TypeScript class method and a function expression held by a const are traced; a module
// imported whole calls the function a member names and uses the value another member names, and
// gives nothing when used bare; a default export of a name and a served const holding a function
// are entry points.
func TestTypeScriptTraceReadsMethodsAndNamespaces(t *testing.T) {
	a := analyze(t, "typescript", fstest.MapFS{
		"package.json": {Data: []byte(`{"name":"app"}`)},
		"src/db.ts":    {Data: []byte("import pg from 'pg'\n\nexport const pool = new pg.Pool()\nexport function insert() {}\n")},
		"src/store.ts": {Data: []byte("import * as db from './db'\n\nexport class Repo {\n  save() { db.insert() }\n}\n" +
			"export const put = function () { db.pool.query('x') }\nexport function bare() { return db }\n")},
		"pages/api/notes.ts":      {Data: []byte("function handler() {}\n\nexport default handler\n")},
		"src/routes/y/+server.ts": {Data: []byte("export const GET = async () => {}\n")},
	})
	for id, want := range map[string]string{
		"src.Repo.save": `io=false calls=[src.insert] units=[]`,
		"src.put":       `io=false calls=[] units=["src"]`,
		"src.bare":      `io=false calls=[] units=[]`,
	} {
		if got := reachOf(t, a, id); got != want {
			t.Errorf("%s: got %s, want %s", id, got, want)
		}
	}
	if got, want := entryList(a), "pages/api.handler src/routes/y.GET"; got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}
