# Invoice Product Picker Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Allow selecting existing products per invoice line on `GET /faturas/nova` via dropdown that auto-fills description and unit price (snapshot copy, still editable).

**Architecture:** Extend `newInvoiceForm` to load products and pass `ProductOptions` to `fatura_nova.html`; add a `<select class="product-picker" data-row="i">` per row with `data-description`/`data-price` attributes; minimal inline JS copies on change into existing `item_desc_*`/`item_price_*` inputs. Server handling unchanged — still validates snapshot rows.

**Tech Stack:** Go `html/template`, `net/http`, `internal/repo` (Products), `internal/model`, `modernc.org/sqlite`, i18n via `internal/i18n`.

---

### Task 1: Extend invoice form data to carry products

**Files:**
- Modify: `internal/web/faturas.go:100-140`

**Step 1: Write the failing test**

Create `internal/web/faturas_products_test.go`:
```go
package web_test
// TestNewInvoiceFormShowsProductPicker — GET /faturas/nova with 2 products should render two product <select> options with data attributes.
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/web -run TestNewInvoiceFormShowsProductPicker -v`
Expected: FAIL — product options not in HTML.

**Step 3: Write minimal implementation**

In `internal/web/faturas.go`:
- Add to `faturaFormData`:
```go
Products []productOption // for picker
```
```go
type productOption struct {
    Value       string
    Label       string
    Description string
    UnitPrice   string // formatted "0,00" via pdf.FormatBRL or fmt
}
```
- In `newInvoiceForm`, after loading clients, load products:
```go
products, err := h.repos.Products.List(r.Context())
if err != nil { writeRepoErr(w, lang, err); return }
```
- Map to `productOption` slice, pass into `faturaFormData{..., Products: opts}`.
- Ensure error case (no products) yields empty slice (no panic).

**Step 4: Run test to verify it passes**

Run: `go test ./internal/web -run TestNewInvoiceFormShowsProductPicker -v`
Expected: PASS

**Step 5: Commit**

```bash
git -C /home/jesus/invoice-app add internal/web/faturas.go internal/web/faturas_products_test.go
git -C /home/jesus/invoice-app commit -m "feat: pass products to new invoice form"
```

---

### Task 2: Add product picker dropdown per row in template

**Files:**
- Modify: `web/templates/pages/fatura_nova.html:28-43`

**Step 1: Write the failing test (template content)**

Extend same test to assert: for each of the 5 rows, HTML contains `<select` with `class="product-picker"` and `data-row="i"` and product options.

**Step 2: Run test to verify it fails**

Run: `go test ./internal/web -run TestNewInvoiceFormShowsProductPicker -v`
Expected: FAIL — selects not found.

**Step 3: Write minimal implementation**

In `fatura_nova.html` inside `{{range $i, $it := $d.Items}}`:
```html
<tr>
<td>
  <select class="product-picker" data-row="{{$i}}">
    <option value="">{{T "label.select_product"}}</option>
    {{range $d.Products}}<option value="{{.Value}}" data-description="{{.Description}}" data-price="{{.UnitPrice}}">{{.Label}} — {{.UnitPrice}}</option>{{end}}
  </select>
  <input type="text" name="item_desc_{{$i}}" value="{{$it.Description}}">
</td>
<td><input ... qty></td>
<td><input ... price></td>
</tr>
```
If `ProductOption` is at top-level, adjust to `$.Products` vs `$d.Products` accordingly (use `{{$d := .}}` already there).

**Step 4: Run test to verify it passes**

Run: `go test ./internal/web -run TestNewInvoiceFormShowsProductPicker -v`
Expected: PASS

**Step 5: Commit**

```bash
git -C /home/jesus/invoice-app add web/templates/pages/fatura_nova.html
git -C /home/jesus/invoice-app commit -m "feat: add product picker dropdown per invoice row"
```

---

### Task 3: JS auto-fill on product selection

**Files:**
- Modify: `web/templates/pages/fatura_nova.html:44-62` (add script block)

**Step 1: Write the failing test (manual / DOM)**

Add `internal/web/faturas_products_test.go` helper that parses rendered HTML and checks script contains `product-picker` and `data-description` handling. Or assert HTML contains `<script>` with `addEventListener.*change.*product-picker`.

**Step 2: Run test to verify it fails**

Run: `go test ./internal/web -run TestProductPickerJS -v`
Expected: FAIL

**Step 3: Write minimal implementation**

Append before `</form>` or after table:
```html
<script>
document.querySelectorAll('.product-picker').forEach(function(sel){
  sel.addEventListener('change', function(){
    var r = this.dataset.row;
    var opt = this.options[this.selectedIndex];
    if (!opt || !opt.value) return;
    var d = document.querySelector('input[name="item_desc_'+r+'"]');
    var p = document.querySelector('input[name="item_price_'+r+'"]');
    if (d) d.value = opt.dataset.description || "";
    if (p) p.value = opt.dataset.price || "";
  });
});
</script>
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/web -run TestProductPickerJS -v`
Expected: PASS
Run full suite: `go test ./... -count=1 -v | tail`
Expected: PASS

**Step 5: Commit**

```bash
git -C /home/jesus/invoice-app add web/templates/pages/fatura_nova.html internal/web/faturas_products_test.go
git -C /home/jesus/invoice-app commit -m "feat: auto-fill invoice row from product picker"
```

---

### Task 4: i18n and polish + verify no regression

**Files:**
- Modify: `internal/i18n/locales/en.json`, `internal/i18n/locales/pt-BR.json`
- Modify: `web/templates/pages/fatura_nova.html` (placeholder text)

**Step 1: Write the failing test**

Assert rendered invoice form contains i18n key `label.select_product` translation (not `!label.select_product`).

**Step 2: Run test to verify it fails**

Run: `go test ./internal/web -run TestProductPickerI18n -v`
Expected: FAIL — missing key.

**Step 3: Write minimal implementation**

Add to both locale files:
```json
"label.select_product": "Select product" / "Selecione o produto"
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/web -run TestProductPickerI18n -v`
Expected: PASS
Final full suite: `go test ./... -count=1 -race 2>&1 | tail`

**Step 5: Commit**

```bash
git -C /home/jesus/invoice-app add internal/i18n/locales/en.json internal/i18n/locales/pt-BR.json
git -C /home/jesus/invoice-app commit -m "feat: i18n for product picker placeholder"
```

