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
	Rows []recurringRowData
}

func (h *Handlers) listRecurring(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	lang := h.lang(r)
	schedules, err := h.repos.Recurring.List(ctx)
	if err != nil {
		writeRepoErr(w, lang, err)
		return
	}
	rows := make([]recurringRowData, 0, len(schedules))
	for _, s := range schedules {
		row := recurringRowData{
			ID:             s.ID,
			ClientName:     h.clientName(ctx, s.ClientID),
			TemplateID:     s.InvoiceTemplateID,
			FrequencyLabel: i18n.T(lang, "freq."+s.Frequency),
			MethodLabel:    i18n.T(lang, "method."+s.DeliveryMethod),
			Next:           s.NextSendDate,
			Last:           s.LastSentDate,
			Active:         s.Active,
		}
		if tpl, err := h.repos.Invoices.Get(ctx, s.InvoiceTemplateID); err == nil {
			row.TemplateNumber = tpl.Number
		}
		rows = append(rows, row)
	}
	h.renderPage(w, r, http.StatusOK, "recorrentes.html", i18n.T(lang, "recurring.title"), lang, recorrentesData{Rows: rows})
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
	h.renderPage(w, r, http.StatusOK, "recorrente_novo.html", i18n.T(lang, "recurring.new"), lang, data)
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

	tplOpts := make([]selectOption, 0, len(drafts)+1)
	tplOpts = append(tplOpts, selectOption{Value: "", Label: i18n.T(lang, "label.select")})
	for _, inv := range drafts {
		label := fmt.Sprintf("#%06d · %s · %s", inv.Number, h.clientName(ctx, inv.ClientID), pdf.FormatBRL(inv.Total))
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
		h.renderPage(w, r, code, "recorrente_novo.html", i18n.T(lang, "recurring.new"), lang, data)
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
	if _, err := h.repos.Invoices.Get(ctx, templateID); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			refail(i18n.T(lang, "error.template_missing"), http.StatusBadRequest)
			return
		}
		writeRepoErr(w, lang, err)
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
	http.Redirect(w, r, "/recorrentes", http.StatusSeeOther)
}

func (h *Handlers) deleteRecurring(w http.ResponseWriter, r *http.Request) {
	lang := h.lang(r)
	if err := h.repos.Recurring.Delete(r.Context(), r.PathValue("id")); err != nil {
		writeRepoErr(w, lang, err)
		return
	}
	http.Redirect(w, r, "/recorrentes", http.StatusSeeOther)
}
