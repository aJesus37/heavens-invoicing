<p align="center">
  <img src="web/static/logo.svg" width="120" alt="Heaven's Invoicing logo">
</p>

<h1 align="center">Heaven's Invoicing</h1>

<p align="center">
  Minimal, self-hosted invoicing for solo/self-hosters — Brazil-friendly PIX.
  <br>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="MIT"></a>
  <img src="https://img.shields.io/badge/Go-1.26-00ADD8?logo=go" alt="Go 1.26">
</p>

Self-hosted invoicing for solo/self-hosters. Create clients, products and monthly invoices, generate PDFs and send **PIX payment reminders** via Email, Telegram and WhatsApp. Clients pay via PIX manually — you mark invoices as paid.

## Features

- **Web UI + JSON API** — server-rendered `html/template` + `htmx`, same SQLite data
- **Invoices, clients, products, recurring** — monthly schedules that clone a draft template; pause/resume without deleting
- **Product picker** — pick products per invoice line, auto-fills description/price, live per-row and grand totals, dynamic rows
- **i18n** — UI `en` / `pt-BR`; per-client language for PDFs and messages
- **Delivery** — **Email (SMTP)**, **Telegram Bot API**, **WhatsApp (whatsmeow)** with separate copyable PIX message (`Chave PIX:` + raw key)
- **Auth** — first run at `/login` sets admin password (bcrypt, `HttpOnly` + `SameSite=Lax` session)
- **Telegram bot** — `/paid <number>`, `/status`, `/upcoming`, `/clients` + hot-reload on Settings save
- **PDF** — pure-Go, description wraps (no truncation), `R$` formatting
- **SQLite** (WAL, single file), zero external services

## Quick start — production (recommended)

Prerequisites: `docker` + `docker compose`.

```sh
# 1. Clone
git clone https://github.com/ajesus37/heavens-invoicing
cd heavens-invoicing

# 2. Run with the published image (GHCR) — no local build
docker compose up -d

# — or — build locally
docker compose up -d --build

# 3. Open
open http://localhost:8010
```

First visit shows **Set admin password** (no env var). After that, all routes require a session.

Data persists in the named volume `invoice-app-data` (`/data/data/app.db` inside the container).

### Updating

```sh
docker compose pull && docker compose up -d
```

### Backup

```sh
docker run --rm -v invoice-app-data:/data -v "$PWD/backup":/backup alpine \
  cp -a /data /backup/heavens-invoicing-$(date +%F)
```

## Quick start — local dev

Prerequisites: **Go 1.26**.

```sh
task run        # http://localhost:8080
# or
go run .

task test       # go test ./...
task test-race  # -race
task vet
```

## Configuration — all via Settings UI

No env files. Open **Settings** (`/settings`):

- **Business** — name/address/PIX shown in PDF header
- **Payment** — default PIX key (per-invoice override, per-client PIX)
- **SMTP** — host/port/user/pass/from
- **Telegram** — bot token + admin chat ID (see help panel in Settings for BotFather/getUpdates steps, hot-reloads on Save)
- **WhatsApp** — **Settings → WhatsApp → Generate QR** → scan in WhatsApp Linked Devices; session persists on disk
- **Language** — `en` / `pt-BR` (admin UI); per-client language controls their PDFs/messages

## Architecture

```
github.com/ajesus37/heavens-invoicing
├── cmd/            (main.go — wire via wire.go)
├── internal/
│   ├── server      — net/http mux (/api, /, gate, /healthz)
│   ├── api         — JSON API /api/*
│   ├── web         — html/template + auth + pairing
│   ├── model/repo  — domain + SQLite
│   ├── db          — open + migrations
│   ├── schedule    — recurring + overdue
│   ├── deliver     — router + Deliverer
│   ├── telegram/whatsapp/email — channels
│   ├── pdf         — fpdf
│   ├── i18n        — en/pt-BR
│   └── auth        — password/sessions
└── web/            — templates + static (css, logo.svg)
```

`GET /healthz` → `ok` for probes.

## Environment variables

| Variable | Meaning | Default |
|----------|---------|---------|
| `PORT` | TCP port inside container | `8080` (compose maps `8010:8010`) |

All other config is via Settings UI.

## Development

```sh
task --list
task build      # → ./bin/invoice-app
task clean
```

`go vet` and `go test -race` must stay green.

---

### AI disclosure

This application was built **heavily with AI assistance**. A human was in the loop for QA, manual browser testing, and final review of security and data-integrity paths. See commit history for the iterative, test-driven development.

---

MIT © 2026 aJesus37 / Heaven Labs — see [LICENSE](LICENSE).
