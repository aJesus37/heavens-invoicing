package web

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jesus/invoice-app/internal/deliver"
	"github.com/jesus/invoice-app/internal/i18n"
	"github.com/jesus/invoice-app/internal/model"
	"github.com/jesus/invoice-app/internal/pdf"
	"github.com/jesus/invoice-app/internal/repo"
)

// maxInvoiceItems is the hard limit for line items per invoice (I2). The
// invoice form disables "Add item" at this count and shows an inline
// error (error.items_limit); the server also caps at this value.
const maxInvoiceItems = 20

var invoiceStatusKeys = []string{"draft", "sent", "paid", "overdue", "cancelled"}

// invoiceStatusFilters backs the status tabs; labels are resolved per
// request via i18n ("" maps to filter.all).
var invoiceStatusFilters = []statusFilter{
	{Key: ""},
	{Key: "draft"},
	{Key: "sent"},
	{Key: "paid"},
	{Key: "overdue"},
	{Key: "cancelled"},
}

type statusFilter struct {
	Key    string
	Label  string
	Active bool
	URL    string
}

func (f statusFilter) label(lang i18n.Lang) string {
	name := f.Key
	if name == "" {
		name = "all"
	}
	return i18n.T(lang, "filter."+name)
}

type pageLink struct {
	Num    int
	URL    string
	Active bool
}

type faturaListData struct {
	Filters       []statusFilter
	Rows          []invoiceRow
	Q             string
	Page          int
	Total         int
	TotalPages    int
	HasPrev       bool
	HasNext       bool
	PrevURL       string
	NextURL       string
	Pages         []pageLink
	CurrentStatus string
}

func invoicePageURL(page int, q, status string) string {
	v := url.Values{}
	if status != "" {
		v.Set("status", status)
	}
	if q != "" {
		v.Set("q", q)
	}
	if page > 1 {
		v.Set("page", strconv.Itoa(page))
	}
	if len(v) == 0 {
		return "/invoices"
	}
	return "/invoices?" + v.Encode()
}

func (h *Handlers) listInvoices(w http.ResponseWriter, r *http.Request) {
	lang := h.lang(r)
	status := r.URL.Query().Get("status")
	if status != "" && !slices.Contains(invoiceStatusKeys, status) {
		failNotFound(w, lang)
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	page := 1
	if s := r.URL.Query().Get("page"); s != "" {
		if p, err := strconv.Atoi(s); err == nil && p > 0 {
			page = p
		}
	}

	invoices, total, err := h.repos.Invoices.ListPaginated(r.Context(), page, q, status)
	if err != nil {
		writeRepoErr(w, lang, err)
		return
	}
	rows, err := buildInvoiceRows(r.Context(), h, invoices)
	if err != nil {
		writeRepoErr(w, lang, err)
		return
	}

	filters := make([]statusFilter, 0, len(invoiceStatusFilters))
	for _, f := range invoiceStatusFilters {
		v := url.Values{}
		if f.Key != "" {
			v.Set("status", f.Key)
		}
		if q != "" {
			v.Set("q", q)
		}
		u := "/invoices"
		if len(v) > 0 {
			u += "?" + v.Encode()
		}
		filters = append(filters, statusFilter{Key: f.Key, Label: f.label(lang), Active: f.Key == status, URL: u})
	}

	perPage := repo.InvoicePageSize
	totalPages := (total + perPage - 1) / perPage
	if totalPages == 0 {
		totalPages = 1
	}
	hasPrev := page > 1
	hasNext := page < totalPages
	var prevURL, nextURL string
	if hasPrev {
		prevURL = invoicePageURL(page-1, q, status)
	}
	if hasNext {
		nextURL = invoicePageURL(page+1, q, status)
	}
	pages := make([]pageLink, 0, totalPages)
	for i := 1; i <= totalPages; i++ {
		pages = append(pages, pageLink{Num: i, URL: invoicePageURL(i, q, status), Active: i == page})
	}

	h.renderPage(w, r, http.StatusOK, "invoices.html", i18n.T(lang, "invoices.title"), lang, faturaListData{
		Filters:       filters,
		Rows:          rows,
		Q:             q,
		Page:          page,
		Total:         total,
		TotalPages:    totalPages,
		HasPrev:       hasPrev,
		HasNext:       hasNext,
		PrevURL:       prevURL,
		NextURL:       nextURL,
		Pages:         pages,
		CurrentStatus: status,
	})
}

// itemForm holds one line of the new-invoice form exactly as typed.
type itemForm struct {
	Description string
	Quantity    string
	UnitPrice   string
}

type faturaFormData struct {
	ClientID  string
	Clients   []selectOption
	Products  []productOption
	IssueDate string
	DueDate   string
	Notes     string
	PIXKey    string
	Items     [maxInvoiceItems]itemForm
	Error     string
	MaxItems  int
}

type selectOption struct {
	Value    string
	Label    string
	Selected bool
}

type productOption struct {
	Value       string
	Label       string
	Description string
	UnitPrice   string
}

func clientOptions(clients []*model.Client, selected string, lang i18n.Lang) []selectOption {
	opts := make([]selectOption, 0, len(clients)+1)
	opts = append(opts, selectOption{Value: "", Label: i18n.T(lang, "label.select")})
	for _, c := range clients {
		opts = append(opts, selectOption{Value: c.ID, Label: c.Name, Selected: c.ID == selected})
	}
	return opts
}

func (h *Handlers) newInvoiceForm(w http.ResponseWriter, r *http.Request) {
	lang := h.lang(r)
	clients, err := h.repos.Clients.List(r.Context())
	if err != nil {
		writeRepoErr(w, lang, err)
		return
	}
	products, err := h.repos.Products.ListActive(r.Context())
	if err != nil {
		writeRepoErr(w, lang, err)
		return
	}
	opts := make([]productOption, 0, len(products))
	for _, p := range products {
		opts = append(opts, productOption{
			Value:       p.ID,
			Label:       p.Name,
			Description: p.Description,
			UnitPrice:   formatReais(p.UnitPrice),
		})
	}
	today := time.Now()
	preselected := r.URL.Query().Get("client_id")
	data := &faturaFormData{
		ClientID:  preselected,
		Clients:   clientOptions(clients, preselected, lang),
		Products:  opts,
		IssueDate: today.Format("2006-01-02"),
		DueDate:   today.AddDate(0, 0, 15).Format("2006-01-02"),
		MaxItems:  maxInvoiceItems,
	}
	h.renderPage(w, r, http.StatusOK, "invoice_new.html", i18n.T(lang, "invoices.new_title"), lang, data)
}

// readItemRows collects the fixed set of item inputs, dropping rows that
// were left completely empty. It handles sparse indices (e.g., 0,5,12) up
// to maxInvoiceItems (JS may add/remove rows leaving gaps).
func readItemRows(r *http.Request) []itemForm {
	rows := make([]itemForm, 0, maxInvoiceItems)
	for i := 0; i < maxInvoiceItems; i++ {
		n := strconv.Itoa(i)
		row := itemForm{
			Description: strings.TrimSpace(r.FormValue("item_desc_" + n)),
			Quantity:    strings.TrimSpace(r.FormValue("item_qty_" + n)),
			UnitPrice:   strings.TrimSpace(r.FormValue("item_price_" + n)),
		}
		if row.Description == "" && row.Quantity == "" && row.UnitPrice == "" {
			continue
		}
		rows = append(rows, row)
	}
	return rows
}

// validateItemRows converts typed rows into invoice items, reporting the
// first problem as a user-facing (localized) error that names the
// offending row.
func validateItemRows(rows []itemForm, lang i18n.Lang) ([]*model.InvoiceItem, error) {
	items := make([]*model.InvoiceItem, 0, len(rows))
	for i, row := range rows {
		label := strconv.Itoa(i + 1)
		if row.Description == "" {
			return nil, fmt.Errorf("%s", i18n.T(lang, "error.item_desc", label))
		}
		qty := int64(1)
		if row.Quantity != "" {
			parsed, err := strconv.ParseInt(row.Quantity, 10, 64)
			if err != nil || parsed < 1 {
				return nil, fmt.Errorf("%s", i18n.T(lang, "error.item_qty", label))
			}
			qty = parsed
		}
		cents, err := parseReais(lang, row.UnitPrice)
		if err != nil || cents < 0 {
			return nil, fmt.Errorf("%s", i18n.T(lang, "error.item_price", label))
		}
		items = append(items, &model.InvoiceItem{
			Description: row.Description,
			Quantity:    qty,
			UnitPrice:   cents,
		})
	}
	return items, nil
}

func (h *Handlers) createInvoice(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	lang := h.lang(r)
	if err := r.ParseForm(); err != nil {
		failBadRequest(w, lang)
		return
	}

	clients, err := h.repos.Clients.List(ctx)
	if err != nil {
		writeRepoErr(w, lang, err)
		return
	}
	options := clientOptions(clients, r.FormValue("client_id"), lang)

	products, err := h.repos.Products.ListActive(ctx)
	if err != nil {
		writeRepoErr(w, lang, err)
		return
	}
	productOpts := make([]productOption, 0, len(products))
	for _, p := range products {
		productOpts = append(productOpts, productOption{
			Value:       p.ID,
			Label:       p.Name,
			Description: p.Description,
			UnitPrice:   formatReais(p.UnitPrice),
		})
	}

	// refail re-renders the form keeping everything the user already typed.
	refail := func(msg string, code int) {
		data := &faturaFormData{
			ClientID:  r.FormValue("client_id"),
			Clients:   options,
			Products:  productOpts,
			IssueDate: r.FormValue("issue_date"),
			DueDate:   r.FormValue("due_date"),
			Notes:     r.FormValue("notes"),
			PIXKey:    strings.TrimSpace(r.FormValue("pix_key")),
			Error:     msg,
			MaxItems:  maxInvoiceItems,
		}
		copy(data.Items[:], readItemRows(r))
		h.renderPage(w, r, code, "invoice_new.html", i18n.T(lang, "invoices.new_title"), lang, data)
	}

	items, err := validateItemRows(readItemRows(r), lang)
	if err != nil {
		refail(err.Error(), http.StatusBadRequest)
		return
	}
	if len(items) == 0 {
		refail(i18n.T(lang, "error.items_required"), http.StatusBadRequest)
		return
	}

	clientID := r.FormValue("client_id")
	if clientID == "" {
		refail(i18n.T(lang, "error.client_required"), http.StatusBadRequest)
		return
	}
	if _, err := h.repos.Clients.Get(ctx, clientID); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			refail(i18n.T(lang, "error.client_missing"), http.StatusBadRequest)
			return
		}
		writeRepoErr(w, lang, err)
		return
	}

	issueDate, err := time.ParseInLocation("2006-01-02", r.FormValue("issue_date"), time.Local)
	if err != nil {
		refail(i18n.T(lang, "error.issue_date_invalid"), http.StatusBadRequest)
		return
	}
	dueDate, err := time.ParseInLocation("2006-01-02", r.FormValue("due_date"), time.Local)
	if err != nil {
		refail(i18n.T(lang, "error.due_date_invalid"), http.StatusBadRequest)
		return
	}
	if truncDay(dueDate).Before(truncDay(issueDate)) {
		refail(i18n.T(lang, "error.due_before_issue"), http.StatusBadRequest)
		return
	}

	inv := &model.Invoice{
		ClientID:  clientID,
		Status:    "draft",
		IssueDate: issueDate,
		DueDate:   dueDate,
		Notes:     r.FormValue("notes"),
		PIXKey:    strPtr(strings.TrimSpace(r.FormValue("pix_key"))),
		Items:     items,
	}
	if err := h.repos.Invoices.Create(ctx, inv); err != nil {
		writeRepoErr(w, lang, err)
		return
	}
	http.Redirect(w, r, "/invoices/"+inv.ID+"?created=1", http.StatusSeeOther)
}

type faturaDetailData struct {
	Inv          *model.Invoice
	Client       *model.Client
	EffectivePix *string
	SendMethods  []selectOption
	CanCancel    bool
	Created      bool
}

func (h *Handlers) showInvoice(w http.ResponseWriter, r *http.Request) {
	lang := h.lang(r)
	inv, err := h.repos.Invoices.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeRepoErr(w, lang, err)
		return
	}
	client, err := h.repos.Clients.Get(r.Context(), inv.ClientID)
	if err != nil {
		writeRepoErr(w, lang, err)
		return
	}

	methods := make([]selectOption, 0, len(methodOrder))
	for _, m := range methodOrder {
		methods = append(methods, selectOption{Value: m, Label: i18n.T(lang, "method."+m)})
	}

	created := r.URL.Query().Get("created") == "1"
	h.renderPage(w, r, http.StatusOK, "invoice_detail.html",
		i18n.T(lang, "detail.title", inv.Number),
		lang,
		faturaDetailData{
			Inv:          inv,
			Client:       client,
			EffectivePix: pdf.PixKeyFor(*inv, h.sender),
			SendMethods:  methods,
			CanCancel:    cancellable(inv.Status),
			Created:      created,
		})
}

type channelOut struct {
	Label string
	OK    bool
	Err   string
}

type sendResultData struct {
	Sent    bool
	Results []channelOut
}

// sendInvoiceAction backs the "Send" button on the invoice detail page:
// it renders the PDF, hands it to the delivery router, and returns an HTML
// fragment listing the per-channel outcomes inline (htmx swap).
func (h *Handlers) sendInvoiceAction(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	lang := h.lang(r)

	method := r.FormValue("method")
	if !validSendMethod(method) {
		http.Error(w, i18n.T(lang, "error.send_method_invalid"), http.StatusBadRequest)
		return
	}
	inv, err := h.repos.Invoices.Get(ctx, r.PathValue("id"))
	if err != nil {
		writeRepoErr(w, lang, err)
		return
	}
	if inv.Status == "cancelled" {
		http.Error(w, sendErrText(lang, deliver.ErrInvoiceCancelled), http.StatusConflict)
		return
	}
	client, err := h.repos.Clients.Get(ctx, inv.ClientID)
	if err != nil {
		writeRepoErr(w, lang, err)
		return
	}

	buf := &bytes.Buffer{}
	if err := pdf.RenderInvoice(buf, h.sender, *client, *inv); err != nil {
		log.Printf("web: render pdf for invoice %s: %v", inv.ID, err)
		http.Error(w, i18n.T(lang, "error.pdf_render"), http.StatusInternalServerError)
		return
	}

	results, err := h.router.SendInvoice(ctx, *client, *inv, buf.Bytes(), method)
	if err != nil && len(results) == 0 {
		// Routing refused up front (e.g. the invoice is already paid);
		// surface a localized reason instead of a fake update failure.
		http.Error(w, sendErrText(lang, err), http.StatusConflict)
		return
	}
	out := make([]channelOut, 0, len(results)+1)
	sent := false
	for _, res := range results {
		ch := channelOut{Label: i18n.T(lang, "method."+res.Channel), OK: res.Err == nil}
		if res.Err != nil {
			ch.Err = sendErrText(lang, res.Err)
		} else {
			sent = true
		}
		out = append(out, ch)
	}
	if err != nil {
		// Persistence failed after delivery; details stay in the server log.
		log.Printf("web: send invoice %s: %v", inv.ID, err)
		out = append(out, channelOut{
			Label: i18n.T(lang, "method.record"),
			Err:   i18n.T(lang, "send.record_failed"),
		})
	}
	h.render.renderFragment(w, "send_resultados.html", lang, sendResultData{Sent: sent, Results: out})
}

// sendErrText renders router errors in the UI language when they are known
// sentinels; anything else (SMTP failures etc.) passes through as-is.
func sendErrText(lang i18n.Lang, err error) string {
	switch {
	case errors.Is(err, deliver.ErrNotConfigured):
		return i18n.T(lang, "send.err_not_configured")
	case errors.Is(err, deliver.ErrInvoicePaid):
		return i18n.T(lang, "send.already_paid")
	case errors.Is(err, deliver.ErrInvoiceCancelled):
		return i18n.T(lang, "send.already_cancelled")
	default:
		return err.Error()
	}
}

func validSendMethod(m string) bool {
	switch m {
	case deliver.MethodEmail, deliver.MethodWhatsApp, deliver.MethodTelegram, deliver.MethodAll:
		return true
	default:
		return false
	}
}

func (h *Handlers) markInvoicePaidAction(w http.ResponseWriter, r *http.Request) {
	lang := h.lang(r)
	id := r.PathValue("id")
	if err := h.repos.Invoices.UpdateStatus(r.Context(), id, "paid"); err != nil {
		writeRepoErr(w, lang, err)
		return
	}
	http.Redirect(w, r, "/invoices/"+id, http.StatusSeeOther)
}

// cancellable reports whether an invoice in the given status may be cancelled
// (draft, sent or overdue — never paid or already cancelled).
func cancellable(status string) bool {
	switch status {
	case "draft", "sent", "overdue":
		return true
	default:
		return false
	}
}

// cancelInvoiceAction cancels a draft/sent/overdue invoice. Paid invoices
// are refused with a localized 409 (the UI hides the action for them, but
// the guard protects direct posts); the csrf_token field is validated by the
// auth gate before this runs.
func (h *Handlers) cancelInvoiceAction(w http.ResponseWriter, r *http.Request) {
	lang := h.lang(r)
	id := r.PathValue("id")
	inv, err := h.repos.Invoices.Get(r.Context(), id)
	if err != nil {
		writeRepoErr(w, lang, err)
		return
	}
	if !cancellable(inv.Status) {
		http.Error(w, i18n.T(lang, "invoices.cancel_forbidden"), http.StatusConflict)
		return
	}
	if err := h.repos.Invoices.UpdateStatus(r.Context(), id, "cancelled"); err != nil {
		writeRepoErr(w, lang, err)
		return
	}
	http.Redirect(w, r, "/invoices/"+id, http.StatusSeeOther)
}
