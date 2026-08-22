package web

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jesus/invoice-app/internal/deliver"
	"github.com/jesus/invoice-app/internal/model"
	"github.com/jesus/invoice-app/internal/pdf"
	"github.com/jesus/invoice-app/internal/repo"
)

const itemRowCount = 5

var invoiceStatusKeys = []string{"draft", "sent", "paid", "overdue", "cancelled"}

var invoiceStatusFilters = []statusFilter{
	{Key: "", Label: "Todas"},
	{Key: "draft", Label: "Rascunho"},
	{Key: "sent", Label: "Enviadas"},
	{Key: "paid", Label: "Pagas"},
	{Key: "overdue", Label: "Vencidas"},
	{Key: "cancelled", Label: "Canceladas"},
}

type statusFilter struct {
	Key    string
	Label  string
	Active bool
}

type faturaListData struct {
	Filters []statusFilter
	Rows    []invoiceRow
}

func (h *Handlers) listInvoices(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	if status != "" && !slices.Contains(invoiceStatusKeys, status) {
		notFound(w)
		return
	}

	var (
		invoices []*model.Invoice
		err      error
	)
	if status == "" {
		invoices, err = h.repos.Invoices.List(r.Context())
	} else {
		invoices, err = h.repos.Invoices.ListByStatus(r.Context(), status)
	}
	if err != nil {
		writeRepoErr(w, err)
		return
	}
	rows, err := buildInvoiceRows(r.Context(), h, invoices)
	if err != nil {
		writeRepoErr(w, err)
		return
	}

	filters := make([]statusFilter, 0, len(invoiceStatusFilters))
	for _, f := range invoiceStatusFilters {
		filters = append(filters, statusFilter{Key: f.Key, Label: f.Label, Active: f.Key == status})
	}

	h.render.renderPage(w, http.StatusOK, "faturas.html", "Faturas", faturaListData{
		Filters: filters,
		Rows:    rows,
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
	IssueDate string
	DueDate   string
	Notes     string
	PIXKey    string
	Items     [itemRowCount]itemForm
	Error     string
}

type selectOption struct {
	Value    string
	Label    string
	Selected bool
}

func clientOptions(clients []*model.Client, selected string) []selectOption {
	opts := make([]selectOption, 0, len(clients)+1)
	opts = append(opts, selectOption{Value: "", Label: "— selecione —"})
	for _, c := range clients {
		opts = append(opts, selectOption{Value: c.ID, Label: c.Name, Selected: c.ID == selected})
	}
	return opts
}

func (h *Handlers) newInvoiceForm(w http.ResponseWriter, r *http.Request) {
	clients, err := h.repos.Clients.List(r.Context())
	if err != nil {
		writeRepoErr(w, err)
		return
	}
	today := time.Now()
	data := &faturaFormData{
		Clients:   clientOptions(clients, ""),
		IssueDate: today.Format("2006-01-02"),
		DueDate:   today.AddDate(0, 0, 15).Format("2006-01-02"),
	}
	h.render.renderPage(w, http.StatusOK, "fatura_nova.html", "Nova fatura", data)
}

// readItemRows collects the fixed set of item inputs, dropping rows that
// were left completely empty.
func readItemRows(r *http.Request) []itemForm {
	rows := make([]itemForm, 0, itemRowCount)
	for i := 0; i < itemRowCount; i++ {
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
// first problem as a user-facing error that names the offending row.
func validateItemRows(rows []itemForm) ([]*model.InvoiceItem, error) {
	items := make([]*model.InvoiceItem, 0, len(rows))
	for i, row := range rows {
		label := strconv.Itoa(i + 1)
		if row.Description == "" {
			return nil, fmt.Errorf("item %s: informe a descrição", label)
		}
		qty := int64(1)
		if row.Quantity != "" {
			parsed, err := strconv.ParseInt(row.Quantity, 10, 64)
			if err != nil || parsed < 1 {
				return nil, fmt.Errorf("item %s: quantidade deve ser um número ≥ 1", label)
			}
			qty = parsed
		}
		cents, err := parseReais(row.UnitPrice)
		if err != nil || cents < 0 {
			return nil, fmt.Errorf("item %s: preço unitário inválido", label)
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
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulário inválido", http.StatusBadRequest)
		return
	}

	clients, err := h.repos.Clients.List(ctx)
	if err != nil {
		writeRepoErr(w, err)
		return
	}
	options := clientOptions(clients, r.FormValue("client_id"))

	// refail re-renders the form keeping everything the user already typed.
	refail := func(msg string, code int) {
		data := &faturaFormData{
			ClientID:  r.FormValue("client_id"),
			Clients:   options,
			IssueDate: r.FormValue("issue_date"),
			DueDate:   r.FormValue("due_date"),
			Notes:     r.FormValue("notes"),
			PIXKey:    strings.TrimSpace(r.FormValue("pix_key")),
			Error:     msg,
		}
		copy(data.Items[:], readItemRows(r))
		h.render.renderPage(w, code, "fatura_nova.html", "Nova fatura", data)
	}

	items, err := validateItemRows(readItemRows(r))
	if err != nil {
		refail(err.Error(), http.StatusBadRequest)
		return
	}
	if len(items) == 0 {
		refail("Informe pelo menos um item.", http.StatusBadRequest)
		return
	}

	clientID := r.FormValue("client_id")
	if clientID == "" {
		refail("Selecione o cliente.", http.StatusBadRequest)
		return
	}
	if _, err := h.repos.Clients.Get(ctx, clientID); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			refail("Cliente inexistente.", http.StatusBadRequest)
			return
		}
		writeRepoErr(w, err)
		return
	}

	issueDate, err := time.ParseInLocation("2006-01-02", r.FormValue("issue_date"), time.Local)
	if err != nil {
		refail("Data de emissão inválida.", http.StatusBadRequest)
		return
	}
	dueDate, err := time.ParseInLocation("2006-01-02", r.FormValue("due_date"), time.Local)
	if err != nil {
		refail("Data de vencimento inválida.", http.StatusBadRequest)
		return
	}
	if truncDay(dueDate).Before(truncDay(issueDate)) {
		refail("O vencimento não pode ser anterior à emissão.", http.StatusBadRequest)
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
		writeRepoErr(w, err)
		return
	}
	http.Redirect(w, r, "/faturas/"+inv.ID, http.StatusSeeOther)
}

type sendMethodOption struct {
	Value string
	Label string
}

type faturaDetailData struct {
	Inv          *model.Invoice
	Client       *model.Client
	EffectivePix *string
	SendMethods  []sendMethodOption
}

func (h *Handlers) showInvoice(w http.ResponseWriter, r *http.Request) {
	inv, err := h.repos.Invoices.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeRepoErr(w, err)
		return
	}
	client, err := h.repos.Clients.Get(r.Context(), inv.ClientID)
	if err != nil {
		writeRepoErr(w, err)
		return
	}

	methods := make([]sendMethodOption, 0, len(methodOrder))
	for _, m := range methodOrder {
		methods = append(methods, sendMethodOption{Value: m, Label: methodLabels[m]})
	}

	h.render.renderPage(w, http.StatusOK, "fatura_detalhe.html",
		fmt.Sprintf("Fatura #%06d", inv.Number),
		faturaDetailData{
			Inv:          inv,
			Client:       client,
			EffectivePix: pdf.PixKeyFor(*inv, h.sender),
			SendMethods:  methods,
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

// sendInvoiceAction backs the "Enviar" button on the invoice detail page:
// it renders the PDF, hands it to the delivery router, and returns an HTML
// fragment listing the per-channel outcomes inline (htmx swap).
func (h *Handlers) sendInvoiceAction(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	method := r.FormValue("method")
	if !validSendMethod(method) {
		http.Error(w, "método de envio inválido", http.StatusBadRequest)
		return
	}
	inv, err := h.repos.Invoices.Get(ctx, r.PathValue("id"))
	if err != nil {
		writeRepoErr(w, err)
		return
	}
	client, err := h.repos.Clients.Get(ctx, inv.ClientID)
	if err != nil {
		writeRepoErr(w, err)
		return
	}

	buf := &bytes.Buffer{}
	if err := pdf.RenderInvoice(buf, h.sender, *client, *inv); err != nil {
		log.Printf("web: render pdf for invoice %s: %v", inv.ID, err)
		http.Error(w, "erro interno ao gerar PDF", http.StatusInternalServerError)
		return
	}

	results, err := h.router.SendInvoice(ctx, *client, *inv, buf.Bytes(), method)
	if err != nil && len(results) == 0 {
		// Routing refused up front (e.g. the invoice is already paid);
		// surface the reason instead of a fake record-update failure.
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	out := make([]channelOut, 0, len(results)+1)
	sent := false
	for _, res := range results {
		ch := channelOut{Label: methodLabels[res.Channel], OK: res.Err == nil}
		if res.Err != nil {
			ch.Err = res.Err.Error()
		} else {
			sent = true
		}
		out = append(out, ch)
	}
	if err != nil {
		// Persistence failed after delivery; details stay in the server log.
		log.Printf("web: send invoice %s: %v", inv.ID, err)
		out = append(out, channelOut{Label: "Registro", Err: "falha ao atualizar status da fatura"})
	}
	h.render.renderFragment(w, "send_resultados.html", sendResultData{Sent: sent, Results: out})
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
	id := r.PathValue("id")
	if err := h.repos.Invoices.UpdateStatus(r.Context(), id, "paid"); err != nil {
		writeRepoErr(w, err)
		return
	}
	http.Redirect(w, r, "/faturas/"+id, http.StatusSeeOther)
}
