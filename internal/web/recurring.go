package web

import (
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

type recurringRowData struct {
	ID             string
	ClientName     string
	TemplateID     string
	TemplateNumber int64
	FrequencyLabel string
	MethodLabel    string
	Next           time.Time
	Last           *time.Time
	Active         bool
}

type recorrentesData struct {
	Rows    []recurringRowData
	Created bool
}

func (h *Handlers) listRecurring(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	lang := h.lang(r)
	schedules, err := h.repos.Recurring.List(ctx)
	if err != nil {
		writeRepoErr(w, lang, err)
		return
	}
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
	rows := make([]recurringRowData, 0, len(schedules))
	for _, s := range schedules {
		name := clientNames[s.ClientID]
		if name == "" {
			name = s.ClientID
		}
		row := recurringRowData{
			ID:             s.ID,
			ClientName:     name,
			TemplateID:     s.InvoiceTemplateID,
			TemplateNumber: invoiceNumbers[s.InvoiceTemplateID],
			FrequencyLabel: i18n.T(lang, "freq."+s.Frequency),
			MethodLabel:    i18n.T(lang, "method."+s.DeliveryMethod),
			Next:           s.NextSendDate,
			Last:           s.LastSentDate,
			Active:         s.Active,
		}
		rows = append(rows, row)
	}
	created := r.URL.Query().Get("created") == "1"
	h.renderPage(w, r, http.StatusOK, "recurring.html", i18n.T(lang, "recurring.title"), lang, recorrentesData{Rows: rows, Created: created})
}

type recorrenteFormData struct {
	Clients    []selectOption
	Templates  []selectOption
	Frequency  []selectOption
	Methods    []selectOption
	NextDate   string
	ClientID   string
	TemplateID string
	Error      string
}

// optionsFrom builds select options translating each value through the
// given i18n key prefix ("freq." or "method.").
func optionsFrom(lang i18n.Lang, prefix string, values []string, selected string) []selectOption {
	opts := make([]selectOption, 0, len(values)+1)
	opts = append(opts, selectOption{Value: "", Label: i18n.T(lang, "label.select")})
	for _, v := range values {
		opts = append(opts, selectOption{Value: v, Label: i18n.T(lang, prefix+v), Selected: v == selected})
	}
	return opts
}

func (h *Handlers) newRecurringForm(w http.ResponseWriter, r *http.Request) {
	lang := h.lang(r)
	data, err := h.recurringFormBase(r, "")
	if err != nil {
		writeRepoErr(w, lang, err)
		return
	}
	h.renderPage(w, r, http.StatusOK, "recurring_new.html", i18n.T(lang, "recurring.new"), lang, data)
}

// recurringFormBase assembles the select options for the new-recurring form.
// Templates are any draft invoice (they act as the model the scheduler
// clones on each fire).
func (h *Handlers) recurringFormBase(r *http.Request, clientFilter string) (*recorrenteFormData, error) {
	ctx := r.Context()
	lang := h.lang(r)
	clients, err := h.repos.Clients.List(ctx)
	if err != nil {
		return nil, err
	}
	drafts, err := h.repos.Invoices.ListByStatus(ctx, "draft")
	if err != nil {
		return nil, err
	}

	clientNames := make(map[string]string, len(clients))
	for _, c := range clients {
		clientNames[c.ID] = c.Name
	}
	tplOpts := make([]selectOption, 0, len(drafts)+1)
	tplOpts = append(tplOpts, selectOption{Value: "", Label: i18n.T(lang, "label.select")})
	for _, inv := range drafts {
		name := clientNames[inv.ClientID]
		if name == "" {
			name = inv.ClientID
		}
		label := fmt.Sprintf("#%06d · %s · %s", inv.Number, name, pdf.FormatBRL(inv.Total))
		tplOpts = append(tplOpts, selectOption{Value: inv.ID, Label: label})
	}

	return &recorrenteFormData{
		Clients:   clientOptions(clients, "", lang),
		Templates: tplOpts,
		Frequency: optionsFrom(lang, "freq.", frequencyOrder, ""),
		Methods:   optionsFrom(lang, "method.", methodOrder, ""),
		NextDate:  time.Now().Format("2006-01-02"),
	}, nil
}

func (h *Handlers) createRecurring(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	lang := h.lang(r)
	if err := r.ParseForm(); err != nil {
		failBadRequest(w, lang)
		return
	}
	refail := func(msg string, code int) {
		data, err := h.recurringFormBase(r, "")
		if err != nil {
			writeRepoErr(w, lang, err)
			return
		}
		data.Error = msg
		h.renderPage(w, r, code, "recurring_new.html", i18n.T(lang, "recurring.new"), lang, data)
	}

	clientID := r.FormValue("client_id")
	templateID := r.FormValue("invoice_template_id")
	frequency := r.FormValue("frequency")
	method := r.FormValue("delivery_method")

	switch {
	case clientID == "":
		refail(i18n.T(lang, "error.client_required"), http.StatusBadRequest)
		return
	case templateID == "":
		refail(i18n.T(lang, "error.template_required"), http.StatusBadRequest)
		return
	case !slices.Contains(frequencyOrder, frequency):
		refail(i18n.T(lang, "error.frequency_invalid"), http.StatusBadRequest)
		return
	case !slices.Contains(methodOrder, method):
		refail(i18n.T(lang, "error.method_invalid"), http.StatusBadRequest)
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
	tpl, err := h.repos.Invoices.Get(ctx, templateID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			refail(i18n.T(lang, "error.template_missing"), http.StatusBadRequest)
			return
		}
		writeRepoErr(w, lang, err)
		return
	}
	if tpl.ClientID != clientID {
		refail(i18n.T(lang, "error.template_client_mismatch"), http.StatusBadRequest)
		return
	}

	nextSend, _ := time.ParseInLocation("2006-01-02", time.Now().Format("2006-01-02"), time.Local)
	if v := r.FormValue("next_send_date"); v != "" {
		parsed, err := time.ParseInLocation("2006-01-02", v, time.Local)
		if err != nil {
			refail(i18n.T(lang, "error.next_date_invalid"), http.StatusBadRequest)
			return
		}
		nextSend = parsed
	}

	s := &model.RecurringSchedule{
		ClientID:          clientID,
		InvoiceTemplateID: templateID,
		Frequency:         frequency,
		DeliveryMethod:    method,
		NextSendDate:      nextSend,
	}
	if err := h.repos.Recurring.Create(ctx, s); err != nil {
		writeRepoErr(w, lang, err)
		return
	}
	http.Redirect(w, r, "/recurring?created=1", http.StatusSeeOther)
}

func (h *Handlers) deleteRecurring(w http.ResponseWriter, r *http.Request) {
	lang := h.lang(r)
	if err := h.repos.Recurring.Delete(r.Context(), r.PathValue("id")); err != nil {
		writeRepoErr(w, lang, err)
		return
	}
	http.Redirect(w, r, "/recurring", http.StatusSeeOther)
}

// toggleRecurring flips a schedule's active flag (pause/resume) and returns
// to the list. The csrf_token field is validated by the auth gate, so this
// is only reachable from an authenticated, same-origin form post.
func (h *Handlers) toggleRecurring(w http.ResponseWriter, r *http.Request) {
	lang := h.lang(r)
	id := r.PathValue("id")
	s, err := h.repos.Recurring.Get(r.Context(), id)
	if err != nil {
		writeRepoErr(w, lang, err)
		return
	}
	s.Active = !s.Active
	if err := h.repos.Recurring.Update(r.Context(), s); err != nil {
		writeRepoErr(w, lang, err)
		return
	}
	http.Redirect(w, r, "/recurring", http.StatusSeeOther)
}

type recurringEditData struct {
	ID        string
	Frequency []selectOption
	Methods   []selectOption
	NextDate  string
	Error     string
}

func (h *Handlers) editRecurringForm(w http.ResponseWriter, r *http.Request) {
	lang := h.lang(r)
	id := r.PathValue("id")
	s, err := h.repos.Recurring.Get(r.Context(), id)
	if err != nil {
		writeRepoErr(w, lang, err)
		return
	}
	data := &recurringEditData{
		ID:        s.ID,
		Frequency: optionsFrom(lang, "freq.", frequencyOrder, s.Frequency),
		Methods:   optionsFrom(lang, "method.", methodOrder, s.DeliveryMethod),
		NextDate:  s.NextSendDate.Format("2006-01-02"),
	}
	h.renderPage(w, r, http.StatusOK, "recurring_edit.html", i18n.T(lang, "recurring.edit_title"), lang, data)
}

func (h *Handlers) updateRecurring(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	lang := h.lang(r)
	id := r.PathValue("id")
	s, err := h.repos.Recurring.Get(ctx, id)
	if err != nil {
		writeRepoErr(w, lang, err)
		return
	}
	if err := r.ParseForm(); err != nil {
		failBadRequest(w, lang)
		return
	}
	frequency := r.FormValue("frequency")
	method := r.FormValue("delivery_method")
	nextDateStr := r.FormValue("next_send_date")

	refail := func(msg string, code int) {
		data := &recurringEditData{
			ID:        s.ID,
			Frequency: optionsFrom(lang, "freq.", frequencyOrder, frequency),
			Methods:   optionsFrom(lang, "method.", methodOrder, method),
			NextDate:  nextDateStr,
			Error:     msg,
		}
		if data.NextDate == "" {
			data.NextDate = s.NextSendDate.Format("2006-01-02")
		}
		h.renderPage(w, r, code, "recurring_edit.html", i18n.T(lang, "recurring.edit_title"), lang, data)
	}

	if !slices.Contains(frequencyOrder, frequency) {
		refail(i18n.T(lang, "error.frequency_invalid"), http.StatusBadRequest)
		return
	}
	if !slices.Contains(methodOrder, method) {
		refail(i18n.T(lang, "error.method_invalid"), http.StatusBadRequest)
		return
	}
	nextSend := s.NextSendDate
	if nextDateStr == "" {
		refail(i18n.T(lang, "error.next_date_invalid"), http.StatusBadRequest)
		return
	}
	parsed, err := time.ParseInLocation("2006-01-02", nextDateStr, time.Local)
	if err != nil {
		refail(i18n.T(lang, "error.next_date_invalid"), http.StatusBadRequest)
		return
	}
	nextSend = parsed

	s.Frequency = frequency
	s.DeliveryMethod = method
	s.NextSendDate = nextSend
	if err := h.repos.Recurring.Update(ctx, s); err != nil {
		writeRepoErr(w, lang, err)
		return
	}
	http.Redirect(w, r, "/recurring", http.StatusSeeOther)
}
