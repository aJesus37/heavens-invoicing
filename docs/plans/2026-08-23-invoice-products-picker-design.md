# Invoice Product Picker — Design

Date: 2026-08-23
Status: Approved
Context: Products can be created at `/produtos` but `fatura_nova.html` has 5 fixed manual rows (`item_desc_*` / `item_qty_*` / `item_price_*`); users must retype product data per invoice.

## Goal
Let users pick existing products per invoice line, auto-filling description and unit price while preserving manual editability and snapshot semantics (later product edits must not affect past invoices).

## Decision
Client-side prefill — no DB migration, no new API.

## Architecture
- `internal/web/faturas.go:126` `newInvoiceForm`: additionally load `Products` via `h.repos.Products.List(ctx)` and pass `ProductOptions []ProductOption{Value, Label, Description, UnitPriceDisplay}` to `faturaFormData`.
- `web/templates/pages/fatura_nova.html`: each of the 5 rows gains `<select class="product-picker" data-row="i"><option value="">— Select product —</option>{{range .Products}}<option value="{{.Value}}" data-description="{{.Description}}" data-price="{{.UnitPrice}}">{{.Label}} — {{.UnitPrice}}</option>{{end}}</select>` above/beside description input.
- Inline minimal JS: on `change`, copy `selectedOption.dataset.description` → `item_desc_i` and `selectedOption.dataset.price` → `item_price_i`. If empty selection, optionally clear row. Keep inputs editable after fill.

## Data Flow
1. GET `/faturas/nova` → handler loads clients + products → renders form with picker per row.
2. User selects product → JS fills row → user may adjust qty/price/notes.
3. POST `/faturas/nova` → existing `readItemRows` / `validateItemRows` / `parseReais` path unchanged; snapshot stored in `invoice_items` (description, quantity, unit_price).

## Constraints
- No new DB column; snapshot-only copy.
- Existing 5-row fixed layout retained; picker is additive.
- `parseReais` handles comma format; price display uses `FormatBRL` inverse.
- If no products exist, picker shows empty placeholder.

## Error Handling
- Product deleted after form render: subsequent POST still succeeds (snapshot already typed or user picks another).
- Invalid product data: JS no-op, server validation of `item_desc_*` / `item_price_*` remains authoritative.

## Testing
- Update `internal/web` page tests: render `fatura_nova.html` with products, assert dropdown presence and data attributes; verify POST with prefilled snapshot succeeds.
- Manual browser verification of JS prefill per row.
- i18n: picker placeholder strings via existing catalog.

## Out of Scope
- Product-linked invoice_items.product_id column (future if traceability needed).
- Autocomplete or dynamic row addition.
- Editing invoice items after creation.
