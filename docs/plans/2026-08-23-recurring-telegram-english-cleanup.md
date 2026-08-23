# Recurring, Telegram & English-only Cleanup Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Recurring form shows guidance when no draft templates exist, Settings shows Telegram bot usage help, and all code file names and routes are English-only with no Portuguese redirects.

**Architecture:** Add conditional hint with link in `recurring_new.html` and help block in `settings.html` via i18n keys; `git mv` Portuguese template/Go files to English names and update all `renderPage` references; delete Portuguese redirect handlers from `handlers.go` so old paths 404.

**Tech Stack:** Go `html/template`, `net/http`, `internal/web`, `internal/i18n`, `modernc.org/sqlite`.

---

### Task 1: Recurring template empty-state hint

**Files:**
- Modify: `web/templates/pages/recurring_new.html` (currently `recorrente_novo.html` before rename — handle after Task 3, but implement hint now on current file)
- Modify: `internal/i18n/locales/en.json`, `internal/i18n/locales/pt-BR.json`
- Test: `internal/web/recurring_test.go` or `internal/web/routes_test.go` extension

**Step 1: Write the failing test**

Add to `internal/web/recurring_test.go`:
```go
func TestRecurringNewShowsNoDraftsHint(t *testing.T) {
  // No drafts: GET /recurring/new -> body contains hint.no_drafts_link and href="/invoices/new"
  // With 1 draft: hint absent
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/web -run TestRecurringNewShowsNoDraftsHint -v`
Expected: FAIL — hint not found

**Step 3: Write minimal implementation**

In `internal/i18n/locales/en.json` add (alphabetical near hint.*):
```json
"hint.no_drafts_link": "No draft invoices yet — <a href=\"/invoices/new\">create a draft invoice</a> first. Drafts are used as recurring templates."
```
Similarly `pt-BR.json`: `"hint.no_drafts_link": "Nenhuma fatura rascunho — <a href=\"/invoices/new\">crie uma fatura rascunho</a> primeiro. Rascunhos são usados como modelos recorrentes."`

In `web/templates/pages/recorrente_novo.html:19-26` (or `recurring_new.html` after rename):
```html
{{if eq (len .Templates) 1}}<p class="hint">{{T "hint.no_drafts_link"}}</p>{{end}}
```
Note Templates always has 1 placeholder option; len==1 means no drafts.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/web -run TestRecurringNewShowsNoDraftsHint -v`
Expected: PASS

**Step 5: Commit**

```bash
git -C /home/jesus/invoice-app add web/templates/pages/recorrente_novo.html internal/i18n/locales/en.json internal/i18n/locales/pt-BR.json internal/web/recurring_test.go
git -C /home/jesus/invoice-app commit -m "feat: hint when no draft templates for recurring"
```

---

### Task 2: Telegram help panel in Settings

**Files:**
- Modify: `web/templates/pages/configuracoes.html` (or `settings.html` after rename)
- Modify: `internal/i18n/locales/en.json`, `internal/i18n/locales/pt-BR.json`
- Test: `internal/web/settings_test.go`

**Step 1: Write the failing test**

Add to `internal/web/settings_test.go`:
```go
func TestSettingsShowsTelegramHelp(t *testing.T) {
  // GET /settings -> body contains settings.telegram_help, /paid, /status, BotFather
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/web -run TestSettingsShowsTelegramHelp -v`
Expected: FAIL

**Step 3: Write minimal implementation**

In both locale files add `settings.telegram_help` with steps + commands (keep HTML minimal, use <p> and <code> via T with safe HTML? Or store plain text and template formats). Simplest: store HTML-escaped string with line breaks and render via `{{T "settings.telegram_help"}}` inside `<div class="help">` (auto-escaped; line breaks via <br> if needed, use `template.HTML` via helper or just plain text with code blocks). Add to `web/templates/pages/configuracoes.html:44-50` under Telegram panel:
```html
<div class="help">{{T "settings.telegram_help"}}</div>
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/web -run TestSettingsShowsTelegramHelp -v`
Expected: PASS

**Step 5: Commit**

```bash
git -C /home/jesus/invoice-app add web/templates/pages/configuracoes.html internal/i18n/locales/en.json internal/i18n/locales/pt-BR.json internal/web/settings_test.go
git -C /home/jesus/invoice-app commit -m "feat: telegram help panel in settings"
```

---

### Task 3: Rename Portuguese files to English

**Files:**
- Rename: `internal/web/faturas.go` → `invoices.go`
- Rename: `internal/web/faturas_products_test.go` → `invoices_products_test.go`
- Rename templates via `git mv`:
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
- Modify: `internal/web/*.go` to update template names in `renderPage` calls

**Step 1: Write the failing test**

No new test; existing `go test ./...` will fail if template names not updated (missing template error).

**Step 2: Run test to verify it fails (before rename)**

Run: `go test ./internal/web -run TestNewEnglishRoutesWork -v`
Expected: FAIL if templates not found

**Step 3: Write minimal implementation**

Execute:
```bash
git -C /home/jesus/invoice-app mv internal/web/faturas.go internal/web/invoices.go
git -C /home/jesus/invoice-app mv internal/web/faturas_products_test.go internal/web/invoices_products_test.go
git -C /home/jesus/invoice-app mv web/templates/pages/cliente_detalhe.html web/templates/pages/client_detail.html
# ... all 11 moves
```
Then `grep -r "recorrente_novo\|fatura_nova\|configuracoes" --include="*.go" internal/web` and replace with new names in `renderPage` strings.

**Step 4: Run test to verify it passes**

Run: `go test ./... -count=1 -v 2>&1 | tail`
Expected: PASS

**Step 5: Commit**

```bash
git -C /home/jesus/invoice-app add -A
git -C /home/jesus/invoice-app commit -m "refactor: rename Portuguese files to English"
```

---

### Task 4: Remove Portuguese redirects (404 for old paths)

**Files:**
- Modify: `internal/web/handlers.go:111-138`
- Modify: `internal/web/routes_test.go`

**Step 1: Write the failing test**

Add to `routes_test.go`:
```go
func TestOldPortugueseRoutesAreGone(t *testing.T) {
  // GET /clientes -> 404, GET /faturas -> 404, etc.
}
```
Currently they return 301, so test expecting 404 will FAIL.

**Step 2: Run test to verify it fails**

Run: `go test ./internal/web -run TestOldPortugueseRoutesAreGone -v`
Expected: FAIL — got 301 want 404

**Step 3: Write minimal implementation**

In `internal/web/handlers.go` delete the entire Portuguese redirect block (the `redirect301`/`redirect301ID` helpers and their `HandleFunc` registrations). Keep only English routes.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/web -run TestOldPortugueseRoutesAreGone -v`
Expected: PASS
Run: `go test ./... -count=1 -race -v 2>&1 | tail`
Expected: PASS

**Step 5: Commit**

```bash
git -C /home/jesus/invoice-app add internal/web/handlers.go internal/web/routes_test.go
git -C /home/jesus/invoice-app commit -m "refactor: remove Portuguese redirects, English-only routes"
```

