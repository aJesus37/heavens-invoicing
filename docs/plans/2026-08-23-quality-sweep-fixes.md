# Quality Sweep Fixes Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix all critical/important UX and code quality issues from the 2026-08-23 sweep plus send PIX key as a separate copyable message.

**Architecture:** Patch web templates/handlers for table/header, flashes, confirmations, and English-only routes; preload maps to fix N+1; add retry for invoice number race; handle sparse invoice items and client/template validation; extend deliverers to send PIX as second message; keep i18n and tests green.

**Tech Stack:** Go `html/template`, `net/http`, `internal/web`/`repo`/`deliver`/`pdf`/`i18n`, `modernc.org/sqlite`, `go-pdf/fpdf`.

---

### Task 1: C1 Table header/body mismatch + mobile overflow

**Files:**
- Modify: `web/templates/pages/recurring.html:10,38`
- Modify: `web/static/app.css:56`
- Test: `internal/web/routes_test.go` or `web_test.go` snapshot

**Step 1: Write failing test**
```go
func TestRecurringTableColspan(t *testing.T) {
  // GET /recurring -> count <th> (8) vs <td> in row (should match) and empty colspan
}
```

**Step 2: Run test fails** `go test -run TestRecurringTableColspan -v` FAIL

**Step 3: Implement**
- `recurring.html:10` header split toggle/delete into two `<th>` or set `colspan=2`; body has 9 `<td>` so header must be 9; empty `colspan="9"`; wrap `<table>` in `<div style="overflow-x:auto">`.
- `app.css` add `.panel table { display:block; overflow-x:auto }` or wrapper.

**Step 4: Test pass** `go test -run TestRecurringTableColspan -v` PASS

**Step 5: Commit**
```bash
git add web/templates/pages/recurring.html web/static/app.css
git commit -m "fix: recurring table colspan and mobile overflow"
```

---

### Task 2: C2 Invoice number race

**Files:**
- Modify: `internal/repo/invoice.go:68`

**Step 1: Write failing test** (concurrent creates)
```go
func TestInvoiceNumberConcurrent(t *testing.T) {
  // spawn 5 goroutines Create -> assert no duplicate number error, 5 distinct numbers
}
```

**Step 2: Run fails** with UNIQUE violation

**Step 3: Implement** retry loop on `SQLITE_CONSTRAINT_UNIQUE` or use `INSERT ... SELECT MAX+1` with `BEGIN IMMEDIATE` / handle error and retry 3 times; or change to `number` as `INTEGER PRIMARY KEY AUTOINCREMENT`.

**Step 4: Test pass**

**Step 5: Commit**

---

### Task 3: C3 N+1 queries

**Files:**
- Modify: `internal/web/recurring.go:42`, `internal/web/dashboard.go:109`

**Step 1: Test** benchmark N+1 count via query log or just assert preload map used (unit test with fake repo count).

**Step 2: Implement** preload `clients := map[string]*Client` via `Clients.List` once, and `templates := map[string]*Invoice` via `Invoices.List` filtered; use maps in loop.

**Step 3: Test pass**

**Step 4: Commit**

---

### Task 4: C6 Cancelled invoice still sendable + C4 partial (recurring edit)

**Files:**
- Modify: `web/templates/pages/invoice_detail.html:56`, `internal/web/invoices.go:390` (check sendInvoiceAction guard), `internal/repo/invoice.go:22`
- Test: `internal/web/resources_test.go:231`

**Step 1: Test** `POST /invoices/{id}/send` on cancelled -> 409

**Step 2: Implement** guard `if inv.Status=="cancelled"` return 409; hide send button.

**Step 3: Test pass**

---

### Task 5: I1 Flash after create + I8 error rendering

**Files:**
- Modify: `internal/web/recurring.go:198`, `invoices.go:314`, `clients.go:98`, `products.go:72`, templates for banners

**Step 1: Test** `GET /recurring?created=1` shows banner

**Step 2: Implement** `Redirect(...?created=1)` and template `{{if .Created}}<div class="ok-banner">...</div>{{end}}`; make client/product `update` use banner not plain `http.Error`.

---

### Task 6: I2 Delete confirm + picker 20 cap warning + I4/I5 polish

**Files:**
- Modify: `web/templates/pages/recurring.html:30`, `web/templates/pages/invoice_new.html`, `internal/web/invoices.go`

**Step 1: Test** delete button has `onsubmit="return confirm"`; add-item disabled at 20, shows inline error.

**Step 2: Implement** add confirm to recurring toggle/delete; in `invoice_new.html` JS `if (nextIdx>=20) { button.disabled=true; error }`; add `aria-label` to remove, `scope="col"` etc.

---

### Task 7: I4 Picker use ListActive + I9 client/template validation

**Files:**
- Modify: `internal/web/invoices.go:141`, `internal/web/recurring.go:107`, `web/templates/pages/invoice_new.html:40`

**Step 1: Test** picker excludes inactive products; createRecurring with mismatched client/template -> 400

**Step 2: Implement** `Products.ListActive`; in `createRecurring` check `inv.ClientID != clientID` -> 400; JS deduplication via `Label — Price (ID)` already.

---

### Task 8: C4 Missing web delete buttons + recurring edit

**Files:**
- Modify: `web/templates/pages/client_detail.html`, `products.html`, `web/templates/pages/recurring.html` (edit link), `internal/web/handlers.go`, `clients.go`, `products.go`, `recurring.go`

**Step 1: Test** client detail has delete form, product list has delete, recurring edit page loads

**Step 2: Implement** add delete handlers already exist in API; expose via web POST `/clients/{id}/delete`, `/products/{id}/delete`, `GET /recurring/{id}/edit` + `POST`.

---

### Task 9: C5 Pagination + I10 Search

**Files:**
- Modify: `internal/repo/*.go` to add `ListPaginated`, `internal/web/*.go` to handle `?page=&q=`

**Step 1: Test** `GET /invoices?page=2` -> second page

**Step 2: Implement** `LIMIT 20 OFFSET` + `q` LIKE filter.

---

### Task 10: I7 Dashboard + I11 Client CTA + I12 PDF filename

**Files:**
- Modify: `internal/web/dashboard.go`, `web/templates/pages/dashboard.html`, `internal/api/invoices.go`

**Step 1: Test** dashboard shows totals, overdue badge

**Step 2: Implement** compute sums, add CTA links, fix PDF filename to `invoice-` for en.

---

### Task 11: PIX key as separate copyable message (new requirement)

**Files:**
- Modify: `internal/deliver/messages.go`, `internal/deliver/whatsapp.go:36`, `internal/deliver/telegram.go:34`, `internal/deliver/email.go:45` (optional), `internal/i18n/locales/*.json`
- Test: `internal/deliver/*_test.go`

**Step 1: Write failing test**
```go
func TestPixAsSeparateMessage(t *testing.T) {
  // SendInvoice with pix key -> fake API should see two calls: SendDocument then SendMessage with just the key
  // caption should NOT contain pix line, second message should be exactly "<pix>" or "PIX key: <pix>" as separate copyable
}
```

**Step 2: Run fails** (currently one call with pix in caption)

**Step 3: Implement**
- In `messages.go` add `pixMessage(lang, key) string` returning just the key (or `deliver.pix_message` i18n) — format `key` alone or `PIX key:\n<key>` per user spec: `...Content... Pix key:\n<pix-key>` as separate message. Spec says separate message containing just the key, with preceding line "Pix key:".
- In `whatsapp.go:36` and `telegram.go:34`: build `caption := invoiceCaption(..., businessName)` WITHOUT pixLine, call `SendDocument`, then if `pix := pixKeyFor(...); pix!=""` call `SendMessage(ctx, jid, pix)` or `SendMessage(ctx, chatID, pix)` with `i18n.T(lang, "deliver.pix_message", pix)` where `deliver.pix_message` = "%s" or "Pix key:\n%s" ? User spec shows:
  ```
  ...Content... Pix key:

  <pix-key>
  ```
  So first message is caption+content, second is just the key. Implement as two-step: after `SendDocument` success, if pix != "" then `api.SendMessage(ctx, jid, pix)` (or with label). For fallback, ensure second message failures don't hide first success but are reported in `ChannelResult`? Router aggregates per-channel results; for whatsapp/telegram single channel, second message is extra. Simplest: deliverers do two sends and return combined error (first error wins, second logged).

- Update `internal/i18n/locales/en.json` add `"deliver.pix_message": "%s"` or `"Pix key:\n%s"` — but spec wants separate message just the key, so deliverers can send raw `pix` as copyable. Use raw key.

- Ensure `pixLine` not appended to caption.

**Step 4: Test pass**

**Step 5: Commit**
