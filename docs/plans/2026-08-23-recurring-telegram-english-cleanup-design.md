# Recurring, Telegram & English-only Cleanup — Design

Date: 2026-08-23
Status: Approved

## Context
- Recurring at `/recurring/new` needs a draft invoice as template (`ListByStatus draft`). When no drafts exist, dropdown is empty with no guidance; user reports "no way to create templates".
- Telegram settings has only `telegram_bot_token` and `admin_telegram_chat_id` with generic hint; user has no idea how to get chat ID or use `/paid`, `/status`, etc. Bot help exists in code (`bot.help`) but not surfaced in UI.
- Code still contains Portuguese file names (`faturas.go`, `cliente_*.html`, `produtos.html`, etc.) and Portuguese route redirects; requirement is English-only code.

## Goals
- Make recurring template creation discoverable.
- Make Telegram bot usable without guessing.
- Remove all Portuguese from code: file names and routes (no redirects, 404 for old paths).

## Architecture

### 1. Recurring Template Empty State
- `internal/web/recurring.go:95` `recurringFormBase`: unchanged logic (loads drafts). If `len(drafts)==0`, template renders hint.
- `web/templates/pages/recurring_new.html` (renamed from `recorrente_novo.html`): below `invoice_template_id` select, if `len .Templates == 1` (only placeholder), show `<p class="hint">{{T "hint.no_drafts_link"}}</p>` where `hint.no_drafts_link` = `No draft invoices yet — <a href="/invoices/new">create a draft invoice</a> first. Drafts are used as recurring templates.` Select disabled via JS or just placeholder. No server change beyond i18n.

### 2. Telegram Help Panel
- `web/templates/pages/settings.html` (renamed from `configuracoes.html`): under Telegram panel, add static help block using i18n `settings.telegram_help` containing:
  - Steps: 1) Talk to @BotFather → /newbot → copy token, 2) Start your bot (/start), 3) Get chat ID (send any message, then Settings shows ID or via getUpdates), 4) Save → restart app.
  - Commands: `/paid <number>` (e.g., `/paid 12`), `/status`, `/upcoming`, `/clients`, with note that `/paid` marks invoice as paid.
- No handler change; content-only. Keys added to `en.json`/`pt-BR.json`.

### 3. English-only Code Cleanup
- Rename Go file: `internal/web/faturas.go` → `invoices.go`; `faturas_products_test.go` → `invoices_products_test.go` (update package imports if any, but package is `web` so only file name).
- Rename templates (11 files):
  - `cliente_detalhe.html` → `client_detail.html`
  - `cliente_novo.html` → `client_new.html`
  - `clientes.html` → `clients.html`
  - `produtos.html` → `products.html`
  - `produto_form.html` → `product_form.html`
  - `faturas.html` → `invoices.html`
  - `fatura_nova.html` → `invoice_new.html`
  - `fatura_detalhe.html` → `invoice_detail.html`
  - `recorrentes.html` → `recurring.html`
  - `recorrente_novo.html` → `recurring_new.html`
  - `configuracoes.html` → `settings.html`
- Update all `renderPage`/`renderFragment` calls in `internal/web/*.go` to new template names.
- `internal/web/handlers.go`: keep only English routes (`/clients`, `/products`, `/invoices`, `/recurring`, `/settings`); delete Portuguese redirect handlers added in `6f9aa5f` (`redirect301`/`redirect301ID` and their registrations). Old paths now 404.
- Update `internal/web/*_test.go` `routes_test.go`: remove `TestOldPortugueseRoutesRedirect` assertions of 301; replace with 404 checks for old paths, keep `TestNewEnglishRoutesWork`.

## Constraints
- No DB migration.
- i18n: add `hint.no_drafts_link` and `settings.telegram_help` to both locales.
- File renames must be `git mv` to preserve history.

## Testing
- Render `GET /recurring/new` with 0 drafts → contains link to `/invoices/new` and hint; with drafts → no hint.
- Render `GET /settings` → contains telegram help strings.
- `GET /clientes` → 404 (not 301); `GET /clients` → 200.
- `go test ./... -count=1 -race` green.

## Out of Scope
- Dedicated template entity separate from drafts.
- Auto-creating template from recurring form.
- Telegram user-client (MTProto) phone resolution.
