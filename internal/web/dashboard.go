package web

import (
	"context"
	"net/http"
	"time"

	"github.com/jesus/invoice-app/internal/model"
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

var frequencyLabels = map[string]string{
	"weekly":    "Semanal",
	"monthly":   "Mensal",
	"quarterly": "Trimestral",
	"yearly":    "Anual",
}

var methodLabels = map[string]string{
	"email":    "E-mail",
	"whatsapp": "WhatsApp",
	"telegram": "Telegram",
	"all":      "Todos",
}

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
		})
	}
	return rows, nil
}

type dashData struct {
	Pending  []invoiceRow
	Upcoming []recurringRow
	Recent   []invoiceRow
}

func (h *Handlers) dashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	pending, err := h.repos.Invoices.ListByStatus(ctx, "sent", "overdue")
	if err != nil {
		writeRepoErr(w, err)
		return
	}
	pendingRows, err := buildInvoiceRows(ctx, h, pending)
	if err != nil {
		writeRepoErr(w, err)
		return
	}

	all, err := h.repos.Invoices.List(ctx)
	if err != nil {
		writeRepoErr(w, err)
		return
	}
	recentRows, err := buildInvoiceRows(ctx, h, all[:min(5, len(all))])
	if err != nil {
		writeRepoErr(w, err)
		return
	}

	schedules, err := h.repos.Recurring.ListActive(ctx)
	if err != nil {
		writeRepoErr(w, err)
		return
	}
	horizon := truncDay(time.Now()).AddDate(0, 0, 8)
	upcoming := make([]recurringRow, 0, len(schedules))
	for _, s := range schedules {
		next := truncDay(s.NextSendDate)
		if !next.Before(horizon) {
			continue
		}
		tplNumber := int64(0)
		if tpl, err := h.repos.Invoices.Get(ctx, s.InvoiceTemplateID); err == nil {
			tplNumber = tpl.Number
		}
		upcoming = append(upcoming, recurringRow{
			ID:             s.ID,
			ClientName:     h.clientName(ctx, s.ClientID),
			TemplateID:     s.InvoiceTemplateID,
			TemplateNumber: tplNumber,
			FrequencyLabel: frequencyLabels[s.Frequency],
			MethodLabel:    methodLabels[s.DeliveryMethod],
			Next:           s.NextSendDate,
		})
	}

	h.render.renderPage(w, http.StatusOK, "dashboard.html", "Dashboard", dashData{
		Pending:  pendingRows,
		Upcoming: upcoming,
		Recent:   recentRows,
	})
}
