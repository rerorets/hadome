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

// cgoCandidates analyses a tree and returns its idempotent candidates' keys.
func cgoCandidates(t *testing.T, tree fstest.MapFS) string {
	t.Helper()
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
	return strings.Join(got, "\n")
}

// viewpoint: behaviour
// target: suggest.Candidates
// In Python, a function a route decorator, a Celery task or a Django URL pattern reaches is a
// candidate when it reaches input or output: through the module's connection, or through a method
// of its own class called on self. One that writes but nothing reaches is not, and neither is one
// that only builds a value.
func TestPythonIdempotentCandidatesFollowEntryPoints(t *testing.T) {
	got := cgoCandidates(t, fstest.MapFS{
		"pyproject.toml":  {Data: []byte("[project]\nname = \"app\"\n")},
		"app/__init__.py": {Data: []byte("")},
		"app/db.py":       {Data: []byte("import psycopg\n\nconn = psycopg.connect()\n")},
		"app/orders.py": {Data: []byte("from app.db import conn\n\n\n" +
			"def create_order(order_id):\n    conn.execute('insert', [order_id])\n\n\n" +
			"def add_note(note):\n    conn.execute('insert', [note])\n\n\n" +
			"def insert_backfill(row):\n    conn.execute('insert', [row])\n\n\n" +
			"def create_token(user):\n    return 'session=' + user\n\n\n" +
			"class Ledger:\n    def submit_entry(self, entry):\n        self._write(entry)\n\n" +
			"    def _write(self, entry):\n        conn.execute('insert', [entry])\n")},
		"app/api.py": {Data: []byte("from fastapi import FastAPI\nfrom app import orders\n\napp = FastAPI()\n\n\n" +
			"@app.post('/orders')\ndef post_order(order_id: str):\n    orders.create_token(order_id)\n    orders.create_order(order_id)\n")},
		"app/tasks.py": {Data: []byte("from celery import shared_task\nfrom app.orders import Ledger\n\n\n" +
			"@shared_task\ndef book(entry):\n    Ledger().submit_entry(entry)\n")},
		"app/views.py": {Data: []byte("from app.orders import add_note\n\n\ndef note(request):\n    add_note(request.body)\n")},
		"app/urls.py":  {Data: []byte("from django.urls import path\nfrom app import views\n\nurlpatterns = [path('notes/', views.note)]\n")},
	})
	want := "idempotent orders.Ledger.submit_entry\nidempotent orders.add_note\nidempotent orders.create_order"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// viewpoint: behaviour
// target: suggest.Candidates
// In Java, a method a Spring request mapping or a message listener reaches through a field of a
// declared type is a candidate when it reaches input or output, here through a Spring Data
// repository. One that writes but nothing reaches is not, and neither is one that only builds a
// value.
func TestJavaIdempotentCandidatesFollowEntryPoints(t *testing.T) {
	src := func(body string) *fstest.MapFile {
		return &fstest.MapFile{Data: []byte("package com.example.orders;\n\n" + body)}
	}
	got := cgoCandidates(t, fstest.MapFS{
		"src/main/java/com/example/orders/Order.java": src("public class Order {}\n"),
		"src/main/java/com/example/orders/OrderRepository.java": src("import org.springframework.data.jpa.repository.JpaRepository;\n\n" +
			"public interface OrderRepository extends JpaRepository<Order, Long> {}\n"),
		"src/main/java/com/example/orders/OrderService.java": src("public class OrderService {\n" +
			"  private final OrderRepository repo;\n" +
			"  public OrderService(OrderRepository repo) { this.repo = repo; }\n" +
			"  public void createOrder(Order o) { repo.save(o); }\n" +
			"  public void addAudit(Order o) { repo.save(o); }\n" +
			"  public void insertBackfill(Order o) { repo.save(o); }\n" +
			"  public String createToken(String user) { return \"t-\" + user; }\n}\n"),
		"src/main/java/com/example/orders/OrderController.java": src("import org.springframework.web.bind.annotation.PostMapping;\n\n" +
			"public class OrderController {\n  private final OrderService service;\n" +
			"  public OrderController(OrderService service) { this.service = service; }\n" +
			"  @PostMapping(\"/orders\")\n  public void post(Order o) { service.createToken(\"u\"); service.createOrder(o); }\n}\n"),
		"src/main/java/com/example/orders/AuditListener.java": src("import org.springframework.kafka.annotation.KafkaListener;\n\n" +
			"public class AuditListener {\n  private final OrderService service;\n" +
			"  public AuditListener(OrderService service) { this.service = service; }\n" +
			"  @KafkaListener(topics = \"audit\")\n  public void on(Order o) { service.addAudit(o); }\n}\n"),
	})
	want := "idempotent com.example.orders.OrderService.addAudit\nidempotent com.example.orders.OrderService.createOrder"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// viewpoint: behaviour
// target: suggest.Candidates
// In Kotlin, Ktor's routing lambdas and a scheduled job are entry points, and a class's constructor
// properties are typed fields its methods call through.
func TestKotlinIdempotentCandidatesFollowEntryPoints(t *testing.T) {
	src := func(body string) *fstest.MapFile {
		return &fstest.MapFile{Data: []byte("package com.example.orders\n\n" + body)}
	}
	got := cgoCandidates(t, fstest.MapFS{
		"src/main/kotlin/com/example/orders/Store.kt": src("import java.sql.Connection\n\n" +
			"class Store(private val conn: Connection) {\n" +
			"    fun createOrder(id: String) { conn.prepareStatement(\"insert\").execute() }\n" +
			"    fun addAudit(id: String) { conn.prepareStatement(\"insert\").execute() }\n" +
			"    fun insertBackfill(id: String) { conn.prepareStatement(\"insert\").execute() }\n" +
			"    fun createToken(user: String): String = \"t-\" + user\n}\n"),
		"src/main/kotlin/com/example/orders/Routes.kt": src("import io.ktor.server.routing.*\n\n" +
			"fun Application.routes(store: Store) {\n    routing {\n        post(\"/orders\") {\n" +
			"            store.createToken(\"u\")\n            store.createOrder(\"o\")\n        }\n    }\n}\n"),
		"src/main/kotlin/com/example/orders/Jobs.kt": src("import org.springframework.scheduling.annotation.Scheduled\n\n" +
			"class Jobs(private val store: Store) {\n    @Scheduled(cron = \"0 0 * * * *\")\n    fun nightly() { store.addAudit(\"n\") }\n}\n"),
	})
	want := "idempotent com.example.orders.Store.addAudit\nidempotent com.example.orders.Store.createOrder"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// viewpoint: behaviour
// target: suggest.Candidates
// TypeScript's queue workers and schedules are entry points: BullMQ's new Worker, Bull's process,
// node-cron's schedule, and kafkajs's eachMessage.
func TestTypeScriptQueueConsumersAreEntryPoints(t *testing.T) {
	got := cgoCandidates(t, fstest.MapFS{
		"package.json": {Data: []byte(`{"name":"app"}`)},
		"src/store.ts": {Data: []byte("import pg from 'pg'\n\nconst pool = new pg.Pool()\n\n" +
			"export async function createInvoice(id: string) { await pool.query('insert', [id]) }\n" +
			"export async function addReminder(id: string) { await pool.query('insert', [id]) }\n" +
			"export async function insertDigest(id: string) { await pool.query('insert', [id]) }\n" +
			"export async function submitEvent(id: string) { await pool.query('insert', [id]) }\n" +
			"export async function createUnused(id: string) { await pool.query('insert', [id]) }\n")},
		"src/workers.ts": {Data: []byte("import { Worker } from 'bullmq'\nimport cron from 'node-cron'\n" +
			"import { createInvoice, addReminder, insertDigest, submitEvent } from './store'\n\n" +
			"new Worker('invoices', async (job) => { await createInvoice(job.data) })\n" +
			"queue.process(async (job) => { await addReminder(job.data) })\n" +
			"cron.schedule('0 * * * *', async () => { await insertDigest('d') })\n" +
			"const eachMessage = async ({ message }) => { await submitEvent(message.value) }\n" +
			"consumer.run({ eachMessage })\n")},
	})
	want := "idempotent src.addReminder\nidempotent src.createInvoice\nidempotent src.insertDigest\nidempotent src.submitEvent"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// viewpoint: behaviour
// target: suggest.Candidates
// In Rust, a handler axum's router is given and a function an actix-web route attribute serves are
// entry points; a method called on a parameter of a declared type, which runs a query through a
// crate that does input or output, is a candidate when one of them reaches it.
func TestRustIdempotentCandidatesFollowEntryPoints(t *testing.T) {
	got := cgoCandidates(t, fstest.MapFS{
		"Cargo.toml": {Data: []byte("[package]\nname = \"app\"\n")},
		"src/lib.rs": {Data: []byte("pub mod audit;\npub mod store;\npub mod web;\n")},
		"src/store.rs": {Data: []byte("use sqlx::PgPool;\n\npub struct Store {\n    pool: PgPool,\n}\n\nimpl Store {\n" +
			"    pub async fn create_order(&self, id: &str) { sqlx::query(id).execute(&self.pool).await.unwrap(); }\n" +
			"    pub async fn add_audit(&self, id: &str) { sqlx::query(id).execute(&self.pool).await.unwrap(); }\n" +
			"    pub async fn insert_backfill(&self, id: &str) { sqlx::query(id).execute(&self.pool).await.unwrap(); }\n" +
			"    pub fn create_token(&self, user: &str) -> String { format!(\"t-{}\", user) }\n}\n")},
		"src/web.rs": {Data: []byte("use axum::{routing::post, Router};\nuse crate::store::Store;\n\n" +
			"pub async fn orders(store: Store) {\n    store.create_token(\"u\");\n    store.create_order(\"o\").await;\n}\n\n" +
			"pub fn app() -> Router {\n    Router::new().route(\"/orders\", post(orders))\n}\n")},
		"src/audit.rs": {Data: []byte("use actix_web::post;\nuse crate::store::Store;\n\n" +
			"#[post(\"/audit\")]\npub async fn audit(store: Store) {\n    store.add_audit(\"a\").await;\n}\n")},
	})
	want := "idempotent store.Store.add_audit\nidempotent store.Store.create_order"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}
