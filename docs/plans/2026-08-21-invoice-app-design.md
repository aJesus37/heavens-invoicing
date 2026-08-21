# Invoice App — Design

**Date:** 2026-08-21
**Status:** Approved
**User:** Solo (single admin)

## Purpose

A minimal self-hosted invoicing app for a solo user in Brazil. Fixed-price monthly invoices sent as payment reminders with a PIX key. No payment processing integration — clients pay via PIX manually.

## Stack

- **Backend:** Go (`net/http` stdlib)
- **Database:** SQLite (single file, zero config)
- **PDF:** Pure Go PDF library (no external deps)
- **Web UI:** Server-rendered `html/template` + htmx for interactivity, minimal CSS
- **Delivery:** Email (SMTP), Telegram (Bot API), WhatsApp (`whatsmeow`)

## Data Model

All monetary values stored as integers (cents) to avoid float issues.

```
clients
├── id (UUID)
├── name
├── email (nullable)
├── phone (nullable, WhatsApp target)
├── telegram_chat_id (nullable)
├── pix_key (nullable)
├── address
├── notes
├── created_at / updated_at

products
├── id (UUID)
├── name
├── description
├── unit_price (cents)
├── currency (default BRL)
├── active (bool)
├── created_at / updated_at

invoices
├── id (UUID)
├── client_id (FK)
├── number (auto-increment)
├── status (draft | sent | paid | overdue | cancelled)
├── issue_date
├── due_date
├── subtotal / total (cents)
├── notes
├── pix_key (copied from client or custom)
├── pdf_path
├── created_at / updated_at

invoice_items
├── id (UUID)
├── invoice_id (FK)
├── product_id (FK, nullable)
├── description
├── unit_price (cents)
├── quantity
└── total (cents)

recurring_schedules
├── id (UUID)
├── client_id (FK)
├── invoice_template_id (FK → invoices; the template invoice)
├── frequency (weekly | monthly | quarterly | yearly)
├── next_send_date
├── last_sent_date
├── delivery_method (email | whatsapp | telegram | all)
├── active (bool)
├── created_at / updated_at
```

Recurring schedules reference a template invoice rather than duplicating its fields. When the schedule fires, the app clones the template with a new number and current dates.

## PDF Generation

Pure Go library. Layout: logo + sender info top-left, invoice number/dates top-right, bill-to block, items table (qty, description, price, total), subtotal/total, PIX key prominently displayed, notes footer.

- Customizable: fonts, colors, logo
- Stored on disk under a data dir, served via web UI
- Generated on demand or when invoice is marked sent

## Delivery Channels

Common Go interface:

```go
type Deliverer interface {
    SendInvoice(ctx context.Context, c Client, inv Invoice, pdf []byte) error
    SendReminder(ctx context.Context, c Client, inv Invoice) error
}
```

### Email
SMTP via config. PDF attached, customizable body template.

### Telegram
Bot API. Bot token configured in settings. Sends PDF as document to the client's `telegram_chat_id`.

### WhatsApp
`whatsmeow` (unofficial WhatsApp Web bridge, free). One-time QR code scan to link a device. Sends PDF as document to client's phone number. Risk: unofficial API could break on WhatsApp changes — acceptable for personal use.

## Telegram Admin Bot

The bot doubles as an admin interface:

**Commands:**
- `/paid <number>` — mark invoice paid
- `/status` — pending invoices
- `/upcoming` — invoices due this week
- `/clients` — list clients

**Reminder confirmation flow:**

1. Scheduler detects an unpaid overdue invoice
2. Bot asks admin: "Invoice #001 (R$100) to Client X is 7 days overdue. Paid? → /paid 001"
3. If admin confirms paid → status updated, done
4. If no reply within 24h → reminder sent to client via their channel

**Admin notifications:** every invoice sent ("Invoice #001 sent to Client X via WhatsApp"), every payment marked.

## Payment Tracking

Manual for MVP: admin marks invoices paid via web UI or Telegram bot. PIX webhook auto-detection is out of scope (possible future enhancement).

## Web UI & API

**Pages:**
- `/` dashboard (pending invoices, recent activity)
- `/clients`, `/clients/new`, `/clients/:id`
- `/products`, `/products/new`
- `/invoices`, `/invoices/new`, `/invoices/:id`
- `/invoices/:id/pdf` download
- `/recurring`, `/recurring/new`
- `/settings` (SMTP, Telegram token, WhatsApp link, default PIX key, default notes)

**JSON API** mirrors pages for programmatic access:

```
GET/POST        /api/clients
GET/PUT/DELETE  /api/clients/:id
GET/POST        /api/products
...
POST            /api/invoices/:id/send
POST            /api/invoices/:id/mark-paid
GET             /api/invoices/:id/pdf
```

## Scheduler

Background goroutine (single process, no cron dependency):

1. Daily tick: find active recurring schedules where `next_send_date <= today`
2. Clone template invoice → new invoice with number, issue date today, due date per terms
3. Deliver via configured channel(s), notify admin via Telegram
4. Advance `next_send_date`, set `last_sent_date`

Overdue check runs daily: unpaid past-due invoices trigger the admin confirmation flow above.

## Error Handling

- Delivery failures logged and surfaced to admin via Telegram notification
- Failed sends retry once, then marked failed in UI for manual resend
- SQLite WAL mode for safe concurrent reads

## Out of Scope (MVP)

- Multi-user auth/roles
- Taxes beyond a simple rate field
- PIX webhook payment detection
- Quotes/estimates, credit notes
- Currency conversion (BRL only assumed but currency field exists)
