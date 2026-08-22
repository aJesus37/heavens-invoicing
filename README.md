# invoice-app

A minimal, self-hosted invoicing app for a solo user (Brazil-friendly). Generates
monthly PDF invoices and sends them as **PIX payment reminders** over Email,
Telegram, and WhatsApp. No payment processing — clients pay via PIX manually and
you mark invoices paid.

## Features

- Server-rendered web UI + JSON API (same data, no extra services)
- Invoices, clients, products, and recurring monthly schedules
- **i18n**: UI in `en` / `pt-BR`; per-client language for delivered messages and PDFs
- Delivery channels: **Email (SMTP)**, **Telegram (Bot API)**, **WhatsApp (whatsmeow)**
- **Auth**: first-run sets an admin password at `/login` (bcrypt hash, session cookie)
- **Recurring pause**: toggle a schedule on/off without deleting it
- **Invoice cancel**: mark an invoice cancelled (kept for history)
- **Admin Telegram bot**: `/paid <number>`, `/status`, `/upcoming`, `/clients`, plus
  delivery and payment notifications
- Single SQLite file (WAL mode), zero external services

## Quick start (local dev)

Prerequisites: **Go 1.26**.

```sh
# Run directly (serves on http://localhost:8080 by default)
task run
# or:
go run .

# Run the test suite
task test
```

Open <http://localhost:8010>. On first run there is no admin password, so
`/login` shows a **set admin password** form. After setting it, all routes (web
and `/api`) require a session.

## Configuring delivery channels

All configuration is done in the web UI at **Settings** (`/configuracoes`) — no
env vars or config files needed:

- **Email (SMTP)**: host, port, user, password, "from" address.
- **Telegram**: bot token + your admin chat id. Once saved, the admin bot
  starts responding in that chat.
- **WhatsApp**: click "connect" on the Settings page to show a **pairing QR
  code** (`/configuracoes/whatsapp/qr.png`); scan it with WhatsApp to link the
  device. The session is stored on disk under the data directory.

Default PIX key and default invoice notes are also set in Settings. Per-client
language and PIX key live on the client record.

## Production deploy (docker compose)

The app is built as a single static binary (no CGO) and runs on `scratch`. A
named volume `invoice-app-data` persists the SQLite DB at `/data`.

```sh
# Build and start in the background
task docker:build
task docker:up
# or directly:
docker compose up -d --build
```

The container listens on `PORT` (default `8010`) and is published on the host as
`${PORT:-8010}:8010`. Data lives in the `invoice-app-data` volume mounted at
`/data`.

### Backup

The entire state is the SQLite database file (plus the WhatsApp session) under
`/data`. Back it up by copying the volume, e.g.:

```sh
docker compose down
docker run --rm -v invoice-app-data:/data -v "$PWD/backup":/backup alpine \
  cp -a /data /backup/invoice-app-$(date +%F)
docker compose up -d
```

## Environment variables

| Variable  | Meaning | Default |
|-----------|---------|---------|
| `PORT`    | TCP port the app listens on (host:port inside container) | `8080` |
| `DATA_DIR`| **Reserved for future use.** The app does not currently read this; data is stored at `./data` relative to the working directory. In the container the working directory is `/`, so `./data` resolves to `/data` (the volume mount). | `/data` |

No other environment variables are read by the app. SMTP, Telegram, WhatsApp,
PIX, locale, and sender info are all stored via the Settings UI.

## Dev workflow

```sh
task list        # show all tasks
task build       # go build -> ./bin/invoice-app
task test        # go test ./...
task test-race   # go test -race ./...
task vet         # go vet ./...
task clean       # remove ./bin
```

`go build ./...` and `go vet ./...` must stay green.

## Architecture

Single Go module (`github.com/jesus/invoice-app`). Key packages under `internal/`:

- `server` — assembles the `net/http` mux (`/api`, `/`, auth gate, `/healthz`)
- `api` — JSON API under `/api`
- `web` — `html/template` UI + auth/session handling
- `model` / `repo` — domain types and SQLite persistence
- `db` — SQLite connection + embedded migrations
- `schedule` — background daily scheduler (recurring + overdue reminders)
- `deliver` — delivery router and the Deliverer interface
- `email` / `telegram` / `whatsapp` — channel implementations
- `pdf` — pure-Go PDF generation
- `i18n` — `en` / `pt-BR` catalogs and resolution
- `auth` — admin password + session management

The web UI exposes `/healthz` (returns `ok`) for external health checks.
