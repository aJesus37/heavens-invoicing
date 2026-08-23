# Invoice Form & Routes — Design

Date: 2026-08-23
Status: Approved

## Context
- Invoice creation at `/faturas/nova` has 5 fixed rows, manual typing per row, no live totals. User wants 1 initial row + add button, product picker already added, plus per-row and grand total live calculation (qty × price).
- Web UI paths are Portuguese (`/clientes`, `/produtos`, `/faturas`, `/recorrentes`, `/configuracoes`); user wants English, standardized, with API already English (`/api/*`). Need 301 redirects for old paths.

## Goals
- Live totals per row + grand total updating on product pick, qty, price input.
- Dynamic rows: start 1, add/remove via button, up to 20.
- Rename web routes to English, keep old Portuguese as 301 redirects.

## Architecture

### Live Totals
- `web/templates/pages/fatura_nova.html`: add `<td class="row-total">` per row and `<tfoot>` grand total row. Add JS `recalc()` that reads `item_qty_*` (int) and `item_price_*` (parse comma/dot via parseReais-like logic), computes `qty * cents`, formats via `formatBRL` inverse (dot thousands, comma decimal), updates row total and sum. Bind to `input` and `change` on picker/qty/price, and on `addRow`.
- No server change for display; server still validates via `validateItemRows`.

### Dynamic Rows
- `internal/web/faturas.go`: change `itemRowCount = 5` → `maxItems = 20`; update `readItemRows` to loop `i < maxItems` and collect sparse non-empty rows (handles gaps from removed rows). Keep `faturaFormData.Items [itemRowCount]itemForm` but either size to maxItems or keep array+separate rendered count; simplest: change to `[20]itemForm` and render only first N via JS template.
- Template: keep 1 visible row at load; hidden `<template id="row-template">` with row HTML (including picker, inputs, remove button, total cell). JS `addItem()` clones, increments counter, updates `name` attributes (`item_desc_N`, `item_qty_N`, `item_price_N`), appends to tbody, re-binds picker/total listeners. Remove button deletes row (keep at least 1).
- `newInvoiceForm` still passes 1 empty item; error re-render (`createInvoice.refail`) must preserve dynamic row count and products.

### Web Routes English
- `internal/web/handlers.go`: rename:
  - `GET /clientes` → `GET /clients`
  - `GET /clientes/novo` → `GET /clients/new`
  - `POST /clientes/novo` → `POST /clients/new`
  - `GET /clientes/{id}` → `GET /clients/{id}`
  - `POST /clientes/{id}` → `POST /clients/{id}`
  - Similarly `/produtos` → `/products` (+ `/new`, `/{id}/edit`)
  - `/faturas` → `/invoices` (+ `/new`, `/{id}`, `/{id}/send`, `/{id}/mark-paid`, `/{id}/cancel`)
  - `/recorrentes` → `/recurring` (+ `/new`, `/{id}/delete`, `/{id}/toggle`)
  - `/configuracoes` → `/settings` (+ `/whatsapp/status`, `/whatsapp/connect`, `/whatsapp/qr.png`)
- Add redirects: for each old Portuguese path, `mux.HandleFunc("GET /clientes", redirect301("/clients"))` etc., including subpaths with preserved IDs/query.
- Update all templates (`layout.html` nav, `faturas.html`, `produtos.html`, `clientes.html`, etc.), `internal/web/*_test.go`, and `internal/api` unchanged.
- Keep `GET /` and `/static/`, `/login`, `/logout`, `/healthz`, `/favicon.ico` as-is.

## Constraints
- No DB migration.
- Totals display only; server is source of truth.
- Max 20 rows to bound POST parsing.
- 301 redirects preserve SEO/bookmarks; no breaking old links.

## Testing
- Web tests: render `/invoices/new` with products, assert picker + total cells + add button present; POST with dynamic rows up to 20 succeeds; old Portuguese GET redirects 301.
- JS totals: manual browser verification (parse `1.500,00` × 2 → `3.000,00`), plus unit test for format/parse helpers if needed.
- Full `go test ./... -count=1 -race`.

## Out of Scope
- Invoice edit after creation.
- API route changes (already English).
- Persisting row order or product_id linkage.
