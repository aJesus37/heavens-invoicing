package api

import (
	"net/http"
	"slices"
	"time"

	"github.com/jesus/invoice-app/internal/model"
)

type recurringPayload struct {
	ClientID          string `json:"client_id"`
	InvoiceTemplateID string `json:"invoice_template_id"`
	Frequency         string `json:"frequency"`       // weekly|monthly|quarterly|yearly
	DeliveryMethod    string `json:"delivery_method"` // email|whatsapp|telegram|all
	NextSendDate      string `json:"next_send_date"`  // YYYY-MM-DD; defaults to today
}

var (
	recurringFrequencies = []string{"weekly", "monthly", "quarterly", "yearly"}
	recurringMethods     = []string{"email", "whatsapp", "telegram", "all"}
)

type recurringResponse struct {
	ID                string    `json:"id"`
	ClientID          string    `json:"client_id"`
	InvoiceTemplateID string    `json:"invoice_template_id"`
	Frequency         string    `json:"frequency"`
	DeliveryMethod    string    `json:"delivery_method"`
	NextSendDate      string    `json:"next_send_date"`
	LastSentDate      *string   `json:"last_sent_date,omitempty"`
	Active            bool      `json:"active"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

func toRecurringResponse(s *model.RecurringSchedule) recurringResponse {
	resp := recurringResponse{
		ID:                s.ID,
		ClientID:          s.ClientID,
		InvoiceTemplateID: s.InvoiceTemplateID,
		Frequency:         s.Frequency,
		DeliveryMethod:    s.DeliveryMethod,
		NextSendDate:      formatDate(s.NextSendDate),
		Active:            s.Active,
		CreatedAt:         s.CreatedAt,
		UpdatedAt:         s.UpdatedAt,
	}
	if s.LastSentDate != nil {
		d := formatDate(*s.LastSentDate)
		resp.LastSentDate = &d
	}
	return resp
}

func (a *api) listRecurring(w http.ResponseWriter, r *http.Request) {
	schedules, err := a.repos.Recurring.List(r.Context())
	if err != nil {
		writeRepoErr(w, err)
		return
	}
	out := make([]recurringResponse, 0, len(schedules))
	for _, s := range schedules {
		out = append(out, toRecurringResponse(s))
	}
	writeJSON(w, http.StatusOK, out)
}

// createRecurring validates everything request-shaped (frequency,
// delivery method, referenced entities) so bad input is a 400 rather than
// a repo-level error.
func (a *api) createRecurring(w http.ResponseWriter, r *http.Request) {
	var payload recurringPayload
	if !decodeJSON(w, r, &payload) {
		return
	}
	if payload.ClientID == "" {
		writeError(w, http.StatusBadRequest, "client_id is required")
		return
	}
	if payload.InvoiceTemplateID == "" {
		writeError(w, http.StatusBadRequest, "invoice_template_id is required")
		return
	}
	if !slices.Contains(recurringFrequencies, payload.Frequency) {
		writeError(w, http.StatusBadRequest, "frequency must be one of: weekly, monthly, quarterly, yearly")
		return
	}
	if !slices.Contains(recurringMethods, payload.DeliveryMethod) {
		writeError(w, http.StatusBadRequest, "delivery_method must be one of: email, whatsapp, telegram, all")
		return
	}
	if _, err := a.repos.Clients.Get(r.Context(), payload.ClientID); err != nil {
		writeError(w, http.StatusBadRequest, "unknown client_id")
		return
	}
	if _, err := a.repos.Invoices.Get(r.Context(), payload.InvoiceTemplateID); err != nil {
		writeError(w, http.StatusBadRequest, "unknown invoice_template_id")
		return
	}

	// Default to today in the server's local timezone: schedule dates are
	// business days, not UTC instants.
	nextSend, _ := time.Parse(dateLayout, time.Now().Format(dateLayout))
	if payload.NextSendDate != "" {
		parsed, ok := parseDate(w, "next_send_date", payload.NextSendDate)
		if !ok {
			return
		}
		nextSend = parsed
	}

	s := &model.RecurringSchedule{
		ClientID:          payload.ClientID,
		InvoiceTemplateID: payload.InvoiceTemplateID,
		Frequency:         payload.Frequency,
		DeliveryMethod:    payload.DeliveryMethod,
		NextSendDate:      nextSend,
	}
	if err := a.repos.Recurring.Create(r.Context(), s); err != nil {
		writeRepoErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toRecurringResponse(s))
}

func (a *api) deleteRecurring(w http.ResponseWriter, r *http.Request) {
	if err := a.repos.Recurring.Delete(r.Context(), r.PathValue("id")); err != nil {
		writeRepoErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
