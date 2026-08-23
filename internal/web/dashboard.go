package web

import (
	"context"
	"net/http"
	"time"

	"github.com/ajesus37/heavens-invoicing/internal/i18n"
	"github.com/ajesus37/heavens-invoicing/internal/model"
)

// invoiceRow is a display-ready invoice line for tables.
type invoiceRow struct {
	ID         string
	Number     int64
	ClientName string
	Issue      time.Time
	Due        time.Time
	Total      int64
	Status     string
	CanCancel  bool
}

type recurringRow struct {
	ID             string
	ClientName     string
	TemplateID     string
	TemplateNumber int64
	FrequencyLabel string
	MethodLabel    string
	Next           time.Time
}

// Order used for select options and i18n key lookups (freq.* / method.*).
var (
	frequencyOrder = []string{"weekly", "monthly", "quarterly", "yearly"}
	methodOrder    = []string{"email", "whatsapp", "telegram", "all"}
)

func truncDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// buildInvoiceRows pairs invoices with client names in a single map lookup
// pass (repo List methods stay untouched).
func buildInvoiceRows(ctx context.Context, h *Handlers, invoices []*model.Invoice) ([]invoiceRow, error) {
	clients, err := h.repos.Clients.List(ctx)
	if err != nil {
		return nil, err
	}
	names := make(map[string]string, len(clients))
	for _, c := range clients {
		names[c.ID] = c.Name
	}
	rows := make([]invoiceRow, 0, len(invoices))
	for _, inv := range invoices {
		rows = append(rows, invoiceRow{
			ID:         inv.ID,
			Number:     inv.Number,
			ClientName: names[inv.ClientID],
			Issue:      inv.IssueDate,
			Due:        inv.DueDate,
			Total:      inv.Total,
			Status:     inv.Status,
			CanCancel:  cancellable(inv.Status),
		})
	}
	return rows, nil
}

type dashData struct {
	Pending      []invoiceRow
	PendingTotal int64
	OverdueCount int
	Upcoming     []recurringRow
	Recent       []invoiceRow
	RecentTotal  int64
}

func (h *Handlers) dashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	lang := h.lang(r)

	pending, err := h.repos.Invoices.ListByStatus(ctx, "sent", "overdue")
	if err != nil {
		writeRepoErr(w, lang, err)
		return
	}
	pendingRows, err := buildInvoiceRows(ctx, h, pending)
	if err != nil {
		writeRepoErr(w, lang, err)
		return
	}

	all, err := h.repos.Invoices.List(ctx)
	if err != nil {
		writeRepoErr(w, lang, err)
		return
	}
	recentRows, err := buildInvoiceRows(ctx, h, all[:min(5, len(all))])
	if err != nil {
		writeRepoErr(w, lang, err)
		return
	}

	schedules, err := h.repos.Recurring.ListActive(ctx)
	if err != nil {
		writeRepoErr(w, lang, err)
		return
	}
	horizon := truncDay(time.Now()).AddDate(0, 0, 8)
	clients, err := h.repos.Clients.List(ctx)
	if err != nil {
		writeRepoErr(w, lang, err)
		return
	}
	clientNames := make(map[string]string, len(clients))
	for _, c := range clients {
		clientNames[c.ID] = c.Name
	}
	invoices, err := h.repos.Invoices.List(ctx)
	if err != nil {
		writeRepoErr(w, lang, err)
		return
	}
	invoiceNumbers := make(map[string]int64, len(invoices))
	for _, inv := range invoices {
		invoiceNumbers[inv.ID] = inv.Number
	}
	var pendingTotal int64
	var overdueCount int
	for _, row := range pendingRows {
		if row.Status == "cancelled" {
			continue
		}
		pendingTotal += row.Total
		if row.Status == "overdue" {
			overdueCount++
		}
	}
	var recentTotal int64
	for _, row := range recentRows {
		if row.Status == "cancelled" {
			continue
		}
		recentTotal += row.Total
	}

	upcoming := make([]recurringRow, 0, len(schedules))
	for _, s := range schedules {
		next := truncDay(s.NextSendDate)
		if !next.Before(horizon) {
			continue
		}
		name := clientNames[s.ClientID]
		if name == "" {
			name = s.ClientID
		}
		upcoming = append(upcoming, recurringRow{
			ID:             s.ID,
			ClientName:     name,
			TemplateID:     s.InvoiceTemplateID,
			TemplateNumber: invoiceNumbers[s.InvoiceTemplateID],
			FrequencyLabel: i18n.T(lang, "freq."+s.Frequency),
			MethodLabel:    i18n.T(lang, "method."+s.DeliveryMethod),
			Next:           s.NextSendDate,
		})
	}

	h.renderPage(w, r, http.StatusOK, "dashboard.html", i18n.T(lang, "dash.title"), lang, dashData{
		Pending:      pendingRows,
		PendingTotal: pendingTotal,
		OverdueCount: overdueCount,
		Upcoming:     upcoming,
		Recent:       recentRows,
		RecentTotal:  recentTotal,
	})
}
