# Invoice App Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Solo-user self-hosted invoicing app (Go + SQLite) that generates invoice PDFs with PIX keys and delivers them via Email, Telegram, and WhatsApp, with recurring schedules and a Telegram admin bot.

**Architecture:** Single Go binary. Stdlib `net/http` server with server-rendered html/template pages plus a JSON API. Repository layer over SQLite (`modernc.org/sqlite`, pure Go). Deliverer interface implemented by three channels. Background goroutines for scheduling and the overdue-reminder confirmation flow. Design doc: `docs/plans/2026-08-21-invoice-app-design.md`.

**Tech Stack:** Go 1.22+, `modernc.org/sqlite`, `github.com/go-pdf/fpdf`, `github.com/tulir/whatsmeow`, stdlib `net/smtp` wrappers, stdlib `html/template`, htmx via CDN.

**Conventions for every task:**
- Work dir: repo root (`/home/jesus/invoice-app`)
- Tests: stdlib `testing`, run with `go test ./...`
- Commit after every green test run. Never commit broken code.
- Money is always `int64` cents. Never float.

---

### Task 1: Scaffold project + HTTP server skeleton

**Files:**
- Create: `go.mod`, `main.go`, `internal/server/server.go`
- Test: `internal/server/server_test.go`

**Step 1: Init module**

```bash
cd /home/jesus/invoice-app && go mod init github.com/jesus/invoice-app && mkdir -p internal/server internal/db internal/pdf internal/deliver internal/schedule web/templates
```

**Step 2: Write failing test**

```go
// internal/server/server_test.go
package server_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthEndpoint(t *testing.T) {
	srv := server.New(server.Config{})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got %d want 200", resp.StatusCode)
	}
	if string(body) != "ok" {
		t.Fatalf("got %q want %q", body, "ok")
	}
}
```

(Note the import: `"github.com/jesus/invoice-app/internal/server"` — fix import block accordingly.)

**Step 3: Run to verify failure**

```bash
go test ./internal/server/
```

Expected: FAIL (package does not exist / undefined server.New).

**Step 4: Implement**

```go
// internal/server/server.go
package server

import (
	"net/http"
)

type Config struct {
	DataDir string
}

type Server struct {
	cfg Config
}

func New(cfg Config) *Server { return &Server{cfg: cfg} }

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	return mux
}

// main.go
package main

import (
	"log"
	"net/http"

	"github.com/jesus/invoice-app/internal/server"
)

func main() {
	srv := server.New(server.Config{DataDir: "./data"})
	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", srv.Handler()))
}
```

**Step 5: Run tests, verify pass**

```bash
go test ./...
```

Expected: all PASS.

**Step 6: Commit**

```bash
git add -A && git commit -m "feat: scaffold http server with healthz"
```

---

### Task 2: SQLite open + migrations

**Files:**
- Create: `internal/db/db.go`, `internal/db/migrations/001_init.sql`
- Modify: `internal/db/embed.go` (embed SQL)
- Test: `internal/db/db_test.go`

**Step 1: Add dep**

```bash
go get modernc.org/sqlite
```

**Step 2: Write failing test**

```go
// internal/db/db_test.go
package db_test

import (
	"path/filepath"
	"testing"

	"github.com/jesus/invoice-app/internal/db"
)

func TestOpenRunsMigrations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	conn, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	var version int
	if err := conn.QueryRow(`SELECT version FROM schema_meta`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version < 1 {
		t.Fatalf("schema version = %d, want >= 1", version)
	}
}
```

**Step 3: Verify failure**, then **Step 4: implement**

```sql
-- internal/db/migrations/001_init.sql
CREATE TABLE clients (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    email TEXT,
    phone TEXT,
    telegram_chat_id TEXT,
    pix_key TEXT,
    address TEXT NOT NULL DEFAULT '',
    notes TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE products (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    unit_price INTEGER NOT NULL,
    currency TEXT NOT NULL DEFAULT 'BRL',
    active INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE invoices (
    id TEXT PRIMARY KEY,
    client_id TEXT NOT NULL REFERENCES clients(id),
    number INTEGER NOT NULL UNIQUE,
    status TEXT NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft','sent','paid','overdue','cancelled')),
    issue_date DATE NOT NULL,
    due_date DATE NOT NULL,
    subtotal INTEGER NOT NULL DEFAULT 0,
    total INTEGER NOT NULL DEFAULT 0,
    notes TEXT NOT NULL DEFAULT '',
    pix_key TEXT,
    pdf_path TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE invoice_items (
    id TEXT PRIMARY KEY,
    invoice_id TEXT NOT NULL REFERENCES invoices(id) ON DELETE CASCADE,
    product_id TEXT REFERENCES products(id),
    description TEXT NOT NULL,
    unit_price INTEGER NOT NULL,
    quantity INTEGER NOT NULL DEFAULT 1,
    total INTEGER NOT NULL
);

CREATE TABLE recurring_schedules (
    id TEXT PRIMARY KEY,
    client_id TEXT NOT NULL REFERENCES clients(id),
    invoice_template_id TEXT NOT NULL REFERENCES invoices(id),
    frequency TEXT NOT NULL CHECK (frequency IN ('weekly','monthly','quarterly','yearly')),
    next_send_date DATE NOT NULL,
    last_sent_date DATE,
    delivery_method TEXT NOT NULL CHECK (delivery_method IN ('email','whatsapp','telegram','all')),
    active INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
```

```go
// internal/db/db.go
package db

import (
	"database/sql"
	"embed"
	"fmt"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

func Open(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)", path)
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if _, err := conn.Exec(`CREATE TABLE IF NOT EXISTS schema_meta (version INTEGER NOT NULL)`); err != nil {
		return nil, err
	}
	var version int
	err = conn.QueryRow(`SELECT COALESCE(MAX(version),0) FROM schema_meta`).Scan(&version)
	if err != nil {
		return nil, err
	}
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return nil, err
	}
	for i, e := range entries {
		v := i + 1
		if v <= version {
			continue
		}
		sqlBytes, err := migrationFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return nil, err
		}
		tx, err := conn.Begin()
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(string(sqlBytes)); err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("migration %d (%s): %w", v, e.Name(), err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_meta (version) VALUES (?)`, v); err != nil {
			tx.Rollback()
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
	}
	return conn, nil
}
```

Wire into `main.go`: create `./data` dir, call `db.Open("./data/app.db")`.

**Step 5:** `go test ./...` → PASS. **Step 6: Commit** `feat: sqlite with embedded migrations`.

---

### Task 3: Client model + repository (CRUD)

**Files:**
- Create: `internal/model/client.go`, `internal/repo/client.go`
- Test: `internal/repo/client_test.go`

**Step 2: Failing test**

```go
// internal/repo/client_test.go
package repo_test

import (
	"testing"
	"time"

	"github.com/jesus/invoice-app/internal/db"
	"github.com/jesus/invoice-app/internal/model"
	"github.com/jesus/invoice-app/internal/repo"
)

func openTestDB(t *testing.T) *repo.Repos {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return repo.New(conn)
}

func TestClientCRUD(t *testing.T) {
	r := openTestDB(t)
	ctx := context.Background()

	c := &model.Client{Name: "Acme", Email: "a@acme.com", Phone: "+5511999999999", PIXKey: "a@acme.com"}
	created, err := r.Clients.Create(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.CreatedAt.IsZero() {
		t.Fatal("expected ID and timestamps set")
	}

	got, err := r.Clients.Get(ctx, created.ID)
	if err != nil || got.Name != "Acme" {
		t.Fatalf("get: %v %+v", err, got)
	}

	got.Name = "Acme SA"
	if err := r.Clients.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	got2, _ := r.Clients.Get(ctx, created.ID)
	if got2.Name != "Acme SA" {
		t.Fatal("update failed")
	}

	list, err := r.Clients.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v %d", err, len(list))
	}

	if err := r.Clients.Delete(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Clients.Get(ctx, created.ID); err == nil {
		t.Fatal("expected not found after delete")
	}
}
```

**Step 4: Implement** `model.Client` struct (fields mirror table; pointers for nullable strings; `time.Time` for timestamps). `repo.New(conn)` returns `Repos{Clients: NewClientRepo(conn), ...}`. ClientRepo implements Create/Get/Update/List/Delete with hand-written SQL; UUIDs via `crypto/rand` helper in `internal/model/id.go`. Not-found returns `repo.ErrNotFound` sentinel.

**Step 5:** tests pass. **Step 6:** commit `feat: client repository`.

---

### Task 4: Product repository (CRUD)

Same pattern as Task 3. Files: `internal/model/product.go`, `internal/repo/product.go`, `internal/repo/product_test.go`. Include `Active bool` toggle in Update. One extra test: `ListActive(ctx)` returns only active products (used when composing invoices). Commit: `feat: product repository`.

---

### Task 5: Invoice + items repository

**Files:** `internal/model/invoice.go`, `internal/repo/invoice.go`, `internal/repo/invoice_test.go`

Behavior:
- `CreateInvoice(ctx, inv)` inserts invoice + items in one transaction; computes `subtotal = Σ item.total`, `total = subtotal`; assigns next `number` (`SELECT COALESCE(MAX(number),0)+1 FROM invoices`) inside the tx.
- `GetInvoice` loads items too.
- `UpdateStatus(ctx, id, status)` validates transition (any→any except draft→sent requires ≥1 item).
- `CloneAsTemplate` used later by scheduler: `CloneFromTemplate(ctx, templateID, issueDate, dueDate) (*Invoice, error)` copies client/items/pix_key/notes, new number, status=draft.
- List filters by status/client.

Tests cover: number auto-increment across two creates, totals computed, clone produces independent copy, status validation rejects unknown statuses. Commit: `feat: invoice repository`.

---

### Task 6: PDF generation

**Files:** `internal/pdf/invoice.go`, `internal/pdf/testutil.go`
Dep: `go get github.com/go-pdf/fpdf`

**Failing test:** generate PDF for a fixture invoice (1 client, 2 items, PIX key `pagamentos@heaven-labs.com`), assert: non-empty output, starts with `%PDF`, contains no error. Write to temp dir.

**Implementation notes:**
- A4 portrait, margins 15mm. Header: sender name/address left, "Fatura #N" right. Bill-to block. Table columns Qty|Description|Unit Price|Total with light gray header row. Totals right-aligned below. PIX key line bold above notes footer.
- Currency formatting helper `formatBRL(cents int64) string` → `R$ 1.234,56` (pt-BR grouping). Unit-test this formatter separately: `123456 → R$ 1.234,56`, `5 → R$ 0,05`.
- Fonts: fpdf built-in Helvetica handles ASCII; PIX keys are ASCII-safe so acceptable for MVP.

Commit: `feat: invoice pdf generation`.

---

### Task 7: Settings store + Email deliverer

**Files:** `internal/repo/settings.go`, `internal/deliver/deliverer.go`, `internal/deliver/email.go`, tests for both.

SettingsRepo: `Get(key) (string, error)`, `Set(key, value)`, well-known key constants (`smtp_host`, `smtp_port`, `smtp_user`, `smtp_pass`, `smtp_from`, `telegram_bot_token`, `admin_telegram_chat_id`, `default_pix_key`).

Deliverer interface (per design doc):

```go
type Deliverer interface {
	Name() string
	SendInvoice(ctx context.Context, c model.Client, inv model.Invoice, pdf []byte) error
	SendReminder(ctx context.Context, c model.Client, inv model.Invoice) error
}
```

EmailDeliverer: takes an interface `sendMail(from string, to []string, subject, body string, attachments map[string][]byte) error` so tests inject a fake; production impl wraps `net/smtp` with a MIME multipart builder (text part + `application/pdf` attachment). Subject/body from settings-overridable templates with `{{.Client.Name}} {{.Invoice.Number}} {{.PIXKey}}` placeholders.

Tests: fake records calls; assert attachment filename `fatura-<number>.pdf`, placeholder substitution. Commit: `feat: settings store and email deliverer`.

---

### Task 8: Telegram deliverer

**Files:** `internal/deliver/telegram.go`, `internal/deliver/telegram_test.go`

No SDK — thin wrapper over Bot API via `net/http`:
- `sendMessage(chatID, text)`
- `sendDocument(chatID, filename, bytes, caption)`

Constructor takes an `*http.Client` and base URL (default `https://api.telegram.org/bot<token>`); tests point at `httptest.Server` asserting multipart form fields (`chat_id`, caption text, document bytes). Requires client's `TelegramChatID` set; return descriptive error if empty.

Commit: `feat: telegram deliverer`.

---

### Task 9: Telegram admin bot (commands + notify)

**Files:** `internal/telegram/adminbot.go`, `internal/telegram/adminbot_test.go`, `internal/telegram/notify.go`

Long-polling loop (`getUpdates` with offset) started as goroutine when bot token configured. Commands parsed from `/command args`:

- `/paid <number>` → `repo.Invoices.UpdateStatus(paid)` → reply confirm or error text
- `/status` → list sent+overdue unpaid invoices
- `/upcoming` → due within 7 days
- `/clients` → list names

`Notify(text)` posts to `admin_telegram_chat_id`; failures logged, never panic the process.

Tests drive handler funcs directly with fake repos + recorded notifier (skip polling loop; it's thin glue). Commit: `feat: telegram admin bot`.

---

### Task 10: WhatsApp deliverer (whatsmeow)

**Files:** `internal/deliver/whatsapp.go`, `internal/whatsapp/session.go`

- Session store: whatsmeow SQLStore backed by our SQLite file (`store/sqlstore` with its own upgrade). Device identity persists in `data/` — survives restarts.
- `Connect()` returns QR string channel on first run; `/settings` page shows QR (rendered as PNG via `github.com/skip2/go-qrcode`) until paired; shows "connected" state after.
- SendInvoice: normalize phone (`+55…` → JID `55<s>@s.whatsapp.net`), upload+send PDF document with caption "Fatura #N".
- Guarded behind `wa_connected` setting check; if disconnected, send endpoints return clear error surfaced to UI/admin.

Tests limited to phone→JID normalization and caption formatting (whatsmeow itself untested; integration verified manually at end of plan). Commit: `feat: whatsapp deliverer`.

---

### Task 11: Delivery orchestration + API endpoints

**Files:** `internal/deliver/router.go`, modify `internal/server/server.go`

Router picks deliverers per `delivery_method`: `all` fans out to every channel the client has configured (email if email set, etc.), collects per-channel results, marks invoice `sent` only if ≥1 succeeded; failures reported to admin via Telegram Notify.

JSON API wired into mux (stdlib pattern `GET /api/clients`, `POST /api/invoices/{id}/send`, etc. — full route list in design doc). Handlers: decode JSON, validate, call repo/deliverer, encode JSON or problem+status. Every endpoint gets a table-driven handler test with fake repos/deliverers (no DB in handler tests).

Commit(s): `feat: delivery router`, `feat: json api`.

---

### Task 12: Scheduler + overdue confirmation flow

**Files:** `internal/schedule/scheduler.go`, `internal/schedule/scheduler_test.go`

Daily ticker (interval injectable for tests):

1. **Recurring fire:** schedules where `active AND next_send_date <= today` → clone template (`CloneFromTemplate`, due date = issue + 30d default), deliver via router, advance `next_send_date` by frequency (monthly uses `AddDate(0,1,0)` clamping day-of-month ≤28).
2. **Overdue flow:** invoices `status='sent' AND due_date < today - graceDays` → ask admin via Telegram ("Invoice #N … Paid? → /paid N"), register pending confirmation keyed by invoice ID; if unresolved after 24h → `SendReminder` to client, mark invoice `overdue`.
3. Admin `/paid` during pending window cancels reminder (checked before firing).

All time-dependent logic takes a `now func() time.Time` injected in tests; run scheduler tick synchronously in tests via exported `Tick(ctx)`. Commit: `feat: recurring scheduler and overdue flow`.

---

### Task 13: Web UI

**Files:** `web/templates/*.html`, `web/static/`, modify server to render pages

Server-rendered `html/template` with a shared layout (nav: Dashboard/Clients/Products/Invoices/Recurring/Settings). htmx via CDN for form posts without JS build step. Pages per design doc §Web UI; forms post to JSON API endpoints via htmx; invoice detail includes actions: Send (channel picker), Mark paid, Download PDF; settings page includes SMTP/Telegram/default PIX form + WhatsApp connect state/QR image.

Handler tests: GET pages render 200 and contain expected markers (e.g., invoice detail renders number). Manual smoke checklist at end:
```bash
go run . # visit http://localhost:8080
# create client+product+invoice → generate/send PDF → mark paid via /paid N
```
Commit: `feat: web ui`.

---

## Final verification

1. `go vet ./... && go test ./...` clean.
2. Deploy to homelab box (192.168.10.16): single binary + systemd unit or docker-compose (golang image, volume `/data`). Suggest port 8000 alongside existing services, or move Invoice Ninja off first.
3. Link WhatsApp via settings QR; configure SMTP + Telegram token; create one recurring schedule; observe one full cycle manually (set `next_send_date` yesterday to force immediate fire).
