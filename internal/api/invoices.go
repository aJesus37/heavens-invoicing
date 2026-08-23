package api

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/jesus/invoice-app/internal/i18n"
	"github.com/jesus/invoice-app/internal/model"
	"github.com/jesus/invoice-app/internal/pdf"
	"github.com/jesus/invoice-app/internal/repo"
)

const dateLayout = "2006-01-02"

var invoiceStatuses = []string{"draft", "sent", "paid", "overdue", "cancelled"}
var sendMethods = []string{"email", "whatsapp", "telegram", "all"}

// Invoice payloads use YYYY-MM-DD dates and snake_case fields; model
// structs stay free of API concerns.

type itemPayload struct {
	ProductID   *string `json:"product_id,omitempty"`
	Description string  `json:"description"`
	UnitPrice   int64   `json:"unit_price"`
	Quantity    int64   `json:"quantity"`
}

type invoicePayload struct {
	ClientID  string        `json:"client_id"`
	Status    string        `json:"status,omitempty"`
	IssueDate string        `json:"issue_date"` // YYYY-MM-DD
	DueDate   string        `json:"due_date"`   // YYYY-MM-DD
	Notes     string        `json:"notes"`
	PIXKey    *string       `json:"pix_key,omitempty"`
	Items     []itemPayload `json:"items"`
}

type itemResponse struct {
	ID          string  `json:"id"`
	ProductID   *string `json:"product_id,omitempty"`
	Description string  `json:"description"`
	UnitPrice   int64   `json:"unit_price"`
	Quantity    int64   `json:"quantity"`
	Total       int64   `json:"total"`
}

type invoiceResponse struct {
	ID        string         `json:"id"`
	ClientID  string         `json:"client_id"`
	Number    int64          `json:"number"`
	Status    string         `json:"status"`
	IssueDate string         `json:"issue_date"` // YYYY-MM-DD
	DueDate   string         `json:"due_date"`
	Subtotal  int64          `json:"subtotal"`
	Total     int64          `json:"total"`
	Notes     string         `json:"notes"`
	PIXKey    *string        `json:"pix_key,omitempty"`
	Items     []itemResponse `json:"items"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

func formatDate(t time.Time) string { return t.Format(dateLayout) }

func toInvoiceResponse(inv *model.Invoice) invoiceResponse {
	resp := invoiceResponse{
		ID:        inv.ID,
		ClientID:  inv.ClientID,
		Number:    inv.Number,
		Status:    inv.Status,
		IssueDate: formatDate(inv.IssueDate),
		DueDate:   formatDate(inv.DueDate),
		Subtotal:  inv.Subtotal,
		Total:     inv.Total,
		Notes:     inv.Notes,
		PIXKey:    inv.PIXKey,
		Items:     make([]itemResponse, 0, len(inv.Items)),
		CreatedAt: inv.CreatedAt,
		UpdatedAt: inv.UpdatedAt,
	}
	for _, it := range inv.Items {
		resp.Items = append(resp.Items, itemResponse{
			ID:          it.ID,
			ProductID:   it.ProductID,
			Description: it.Description,
			UnitPrice:   it.UnitPrice,
			Quantity:    it.Quantity,
			Total:       it.Total,
		})
	}
	return resp
}

func (a *api) listInvoices(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	clientID := r.URL.Query().Get("client_id")

	if status != "" && !slices.Contains(invoiceStatuses, status) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown status %q (valid: draft, sent, paid, overdue, cancelled)", status))
		return
	}

	var (
		invoices []*model.Invoice
		err      error
	)
	switch {
	case status != "" && clientID != "":
		if invoices, err = a.repos.Invoices.ListByStatus(r.Context(), status); err == nil {
			invoices = filterByClient(invoices, clientID)
		}
	case status != "":
		invoices, err = a.repos.Invoices.ListByStatus(r.Context(), status)
	case clientID != "":
		invoices, err = a.repos.Invoices.ListByClient(r.Context(), clientID)
	default:
		invoices, err = a.repos.Invoices.List(r.Context())
	}
	if err != nil {
		writeRepoErr(w, err)
		return
	}
	out := make([]invoiceResponse, 0, len(invoices))
	for _, inv := range invoices {
		out = append(out, toInvoiceResponse(inv))
	}
	writeJSON(w, http.StatusOK, out)
}

func filterByClient(invoices []*model.Invoice, clientID string) []*model.Invoice {
	filtered := invoices[:0]
	for _, inv := range invoices {
		if inv.ClientID == clientID {
			filtered = append(filtered, inv)
		}
	}
	return filtered
}

// validateInvoicePayload performs request-level validation so malformed
// bodies become 400s before reaching repo-level (internal) errors.
func validateInvoicePayload(p *invoicePayload) (string, bool) {
	if p.ClientID == "" {
		return "client_id is required", false
	}
	if len(p.Items) == 0 {
		return "at least one item is required", false
	}
	if p.Status != "" && !slices.Contains(invoiceStatuses, p.Status) {
		return fmt.Sprintf("unknown status %q (valid: draft, sent, paid, overdue, cancelled)", p.Status), false
	}
	for i, it := range p.Items {
		if it.Description == "" {
			return fmt.Sprintf("items[%d]: description is required", i), false
		}
		if it.Quantity < 1 {
			return fmt.Sprintf("items[%d]: quantity must be >= 1", i), false
		}
		if it.UnitPrice < 0 {
			return fmt.Sprintf("items[%d]: unit_price must be >= 0", i), false
		}
	}
	return "", true
}

func parseDate(w http.ResponseWriter, field, value string) (time.Time, bool) {
	t, err := time.Parse(dateLayout, value)
	if err != nil {
		writeError(w, http.StatusBadRequest, field+" must be YYYY-MM-DD")
		return time.Time{}, false
	}
	return t, true
}

// writeUnknownRef reports a payload reference to a nonexistent entity.
func writeUnknownRef(w http.ResponseWriter, what string) {
	writeError(w, http.StatusBadRequest, "unknown "+what)
}

func (a *api) createInvoice(w http.ResponseWriter, r *http.Request) {
	var payload invoicePayload
	if !decodeJSON(w, r, &payload) {
		return
	}
	if msg, ok := validateInvoicePayload(&payload); !ok {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	issueDate, ok := parseDate(w, "issue_date", payload.IssueDate)
	if !ok {
		return
	}
	dueDate, ok := parseDate(w, "due_date", payload.DueDate)
	if !ok {
		return
	}
	if dueDate.Before(issueDate) {
		writeError(w, http.StatusBadRequest, "due_date must be on or after issue_date")
		return
	}
	if _, err := a.repos.Clients.Get(r.Context(), payload.ClientID); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			writeUnknownRef(w, "client_id")
			return
		}
		writeRepoErr(w, err)
		return
	}

	inv := &model.Invoice{
		ClientID:  payload.ClientID,
		Status:    payload.Status,
		IssueDate: issueDate,
		DueDate:   dueDate,
		Notes:     payload.Notes,
		PIXKey:    payload.PIXKey,
	}
	for _, it := range payload.Items {
		inv.Items = append(inv.Items, &model.InvoiceItem{
			ProductID:   it.ProductID,
			Description: it.Description,
			UnitPrice:   it.UnitPrice,
			Quantity:    it.Quantity,
		})
	}
	if err := a.repos.Invoices.Create(r.Context(), inv); err != nil {
		writeRepoErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toInvoiceResponse(inv))
}

func (a *api) getInvoice(w http.ResponseWriter, r *http.Request) {
	inv, err := a.repos.Invoices.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeRepoErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toInvoiceResponse(inv))
}

func (a *api) invoicePDF(w http.ResponseWriter, r *http.Request) {
	inv, ok := a.loadInvoice(w, r)
	if !ok {
		return
	}
	client, err := a.repos.Clients.Get(r.Context(), inv.ClientID)
	if err != nil {
		writeRepoErr(w, err)
		return
	}

	buf := &bytes.Buffer{}
	if err := pdf.RenderInvoice(buf, a.senderInfo, *client, *inv); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to render pdf")
		return
	}

	prefix := "fatura-"
	if i18n.Resolve(client.Language) == i18n.En {
		prefix = "invoice-"
	}
	filename := fmt.Sprintf("%s%06d.pdf", prefix, inv.Number)
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Content-Length", fmt.Sprint(buf.Len()))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

func (a *api) markInvoicePaid(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := a.repos.Invoices.UpdateStatus(r.Context(), id, "paid"); err != nil {
		writeRepoErr(w, err)
		return
	}
	inv, err := a.repos.Invoices.Get(r.Context(), id)
	if err != nil {
		writeRepoErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toInvoiceResponse(inv))
}

// loadInvoice fetches the path invoice, replying 404 when missing.
func (a *api) loadInvoice(w http.ResponseWriter, r *http.Request) (*model.Invoice, bool) {
	inv, err := a.repos.Invoices.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeRepoErr(w, err)
		return nil, false
	}
	return inv, true
}

// cancelInvoice cancels a draft, sent or overdue invoice. Paid invoices are
// rejected with 409 because cancelling would undo a completed payment; the
// status flip is delegated to the repo's permissive UpdateStatus.
func (a *api) cancelInvoice(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}
	inv, err := a.repos.Invoices.Get(r.Context(), id)
	if err != nil {
		writeRepoErr(w, err)
		return
	}
	if inv.Status == "paid" {
		writeError(w, http.StatusConflict, "invoice is paid and cannot be cancelled")
		return
	}
	if err := a.repos.Invoices.UpdateStatus(r.Context(), id, "cancelled"); err != nil {
		writeRepoErr(w, err)
		return
	}
	updated, err := a.repos.Invoices.Get(r.Context(), id)
	if err != nil {
		writeRepoErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toInvoiceResponse(updated))
}
