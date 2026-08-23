# Invoice Form & Routes Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Invoice creation has 1 initial row with add/remove, live per-row and grand totals, and all web UI routes are English with 301 redirects from old Portuguese paths.

**Architecture:** Extend `fatura_nova.html` with a `<template>` row and JS for cloning, picker prefill, and totals recalc (parse `1.500,00` ↔ cents). Raise `itemRowCount` to 20 and make `readItemRows` sparse. Rename `internal/web/handlers.go` routes to English (`/clients`, `/products`, `/invoices`, `/recurring`, `/settings`) and add 301 handlers for old Portuguese paths; update templates and tests.

**Tech Stack:** Go `html/template`, `net/http`, `internal/web`/`internal/repo`/`internal/model`, `internal/i18n`, `modernc.org/sqlite`.

---

### Task 1: Live totals + dynamic row template in invoice form

**Files:**
- Modify: `web/templates/pages/fatura_nova.html:28-78`
- Test: `internal/web/faturas_products_test.go`

**Step 1: Write the failing test**

Add to `internal/web/faturas_products_test.go`:
```go
func TestInvoiceFormHasTotalsAndAddButton(t *testing.T) {
  // GET /invoices/new -> assert HTML contains row-total cell, grand total element (#grand-total), add button (#add-item), and template#row-template, and script with recalc/total logic
}
func TestInvoiceFormSingleInitialRow(t *testing.T) {
  // Count visible product-picker selects == 1 (not 5)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/web -run TestInvoiceFormHasTotalsAndAddButton -v`
Expected: FAIL — totals/add button/template not found

**Step 3: Write minimal implementation**

In `fatura_nova.html`:
- Change table header to `Description | Qty | UnitPrice | Total |`
- Each row: add `<td class="row-total">0,00</td>` and remove button `<button type="button" class="remove-row">×</button>`
- Keep 1 visible row at load (range only first item or single row), others via `<template id="row-template">` containing full row HTML with placeholders `__IDX__` for name replacement.
- Add footer: `<tfoot><tr><td colspan="3">Total</td><td id="grand-total">0,00</td></tr></tfoot>`
- Add button: `<button type="button" id="add-item">{{T "button.add_item"}}</button>` (add i18n keys `button.add_item` / `button.remove_item` if needed, fallback to English).
- JS: `function parsePrice(s){...}` (strip `R$`, dots, comma→dot, parseFloat→cents), `function fmt(cents){...}` (cents→ "1.500,00"), `function recalc(){ loop rows, qty*price, update row-total, sum grand-total }`, bind to `input`/`change` on `item_qty_`, `item_price_`, and picker change; `addItem()` clones template, replaces `__IDX__`, appends, binds; `remove` handler.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/web -run TestInvoiceFormHasTotalsAndAddButton -v`
Expected: PASS
Run: `go test ./internal/web -run TestInvoiceFormSingleInitialRow -v`
Expected: PASS

**Step 5: Commit**

```bash
git -C /home/jesus/invoice-app add web/templates/pages/fatura_nova.html internal/web/faturas_products_test.go
git -C /home/jesus/invoice-app commit -m "feat: live totals and dynamic rows in invoice form"
```

---

### Task 2: Handler support for dynamic rows (sparse indices up to 20)

**Files:**
- Modify: `internal/web/faturas.go:21-160`
- Test: `internal/web/faturas_products_test.go`

**Step 1: Write the failing test**

Add:
```go
func TestCreateInvoiceDynamicRows(t *testing.T) {
  // POST /invoices/new with item_desc_0, item_desc_5, item_desc_12 (sparse) -> should create invoice with 3 items; also test 20 items max, 21st ignored or error
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/web -run TestCreateInvoiceDynamicRows -v`
Expected: FAIL — only 0..4 handled

**Step 3: Write minimal implementation**

- Change `const itemRowCount = 5` → `const maxInvoiceItems = 20`
- Update `faturaFormData.Items [itemRowCount]itemForm` → `[maxInvoiceItems]itemForm` (or keep 5 but handle sparse separately; simplest change constant to 20)
- Update `readItemRows` loop `for i:=0; i<maxInvoiceItems; i++`
- Update `newInvoiceForm`/`refail` to copy correctly
- Ensure `validateItemRows` still works for up to 20

**Step 4: Run test to verify it passes**

Run: `go test ./internal/web -run TestCreateInvoiceDynamicRows -v`
Expected: PASS
Run: `go test ./... -count=1 -v | tail`
Expected: PASS

**Step 5: Commit**

```bash
git -C /home/jesus/invoice-app add internal/web/faturas.go internal/web/faturas_products_test.go
git -C /home/jesus/invoice-app commit -m "feat: support sparse dynamic invoice rows up to 20"
```

---

### Task 3: Rename web routes to English with 301 redirects

**Files:**
- Modify: `internal/web/handlers.go:59-109`
- Test: `internal/web/*_test.go` (update expectations)

**Step 1: Write the failing test**

Add `internal/web/routes_test.go`:
```go
func TestOldPortugueseRoutesRedirect(t *testing.T) {
  // GET /clientes -> 301 to /clients
  // GET /faturas -> 301 to /invoices
  // GET /configuracoes -> 301 to /settings
}
func TestNewEnglishRoutesWork(t *testing.T) {
  // GET /invoices/new -> 200, etc.
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/web -run TestOldPortugueseRoutesRedirect -v`
Expected: FAIL — 404 not 301

**Step 3: Write minimal implementation**

In `internal/web/handlers.go` `Mux()`:
- Rename all `HandleFunc` patterns to English: `/clients`, `/clients/new`, `/products`, `/invoices`, `/recurring`, `/settings` etc.
- After English routes, add redirects for each old Portuguese path:
```go
for old, ne := range map[string]string{
  "/clientes": "/clients",
  "/clientes/novo": "/clients/new",
  "/produtos": "/products",
  "/faturas": "/invoices",
  "/faturas/nova": "/invoices/new",
  // ... all subpaths including {id} variants need prefix redirect handling
} {
  mux.HandleFunc("GET "+old, redirect301(ne))
}
```
Implement `func redirect301(to string) http.HandlerFunc { return func(w http.ResponseWriter, r *http.Request){ http.Redirect(w,r,to,http.StatusMovedPermanently)} }` and for `{id}` paths use pattern with `r.PathValue`.

Simpler: register explicit `HandleFunc` for each old Portuguese exact path + prefix where needed.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/web -run TestOldPortugueseRoutesRedirect -v`
Expected: PASS
Run: `go test ./internal/web -run TestNewEnglishRoutesWork -v`
Expected: PASS

**Step 5: Commit**

```bash
git -C /home/jesus/invoice-app add internal/web/handlers.go internal/web/routes_test.go
git -C /home/jesus/invoice-app commit -m "feat: rename web routes to English with 301 redirects"
```

---

### Task 4: Update templates, nav, and tests for new routes + i18n

**Files:**
- Modify: `web/templates/layout.html`, `web/templates/pages/*.html`
- Modify: `internal/web/*_test.go`, `internal/i18n/locales/*.json` (if new button keys)

**Step 1: Write the failing test**

Existing tests that assert old paths (e.g., `TestInvoiceCreateFlowEndToEnd` posting to `/faturas/nova`) will fail after Task 3; this task fixes them. Also add check that nav contains `/invoices`, `/settings`.

**Step 2: Run test to verify it fails**

Run: `go test ./internal/web -run TestInvoiceCreateFlowEndToEnd -v`
Expected: FAIL — 404 on old path

**Step 3: Write minimal implementation**

- Update `web/templates/layout.html` nav links: `href="/clients"` etc.
- Update all templates with forms/actions: `action="/faturas/nova"` → `/invoices/new`, etc.
- Update any `htmx` `hx-get`/`hx-post` in `configuracoes` fragments to `/settings/whatsapp/*`
- Add i18n keys if needed: `button.add_item` = "Add item"/"Adicionar item", `col.total` already exists, `invoices.total` etc.
- Update tests to use new paths; keep old redirect test.

**Step 4: Run test to verify it passes**

Run: `go test ./... -count=1 -race -v 2>&1 | tail`
Expected: PASS all

**Step 5: Commit**

```bash
git -C /home/jesus/invoice-app add web/templates/ internal/web/ internal/i18n/
git -C /home/jesus/invoice-app commit -m "feat: update templates and tests for English routes"
```

