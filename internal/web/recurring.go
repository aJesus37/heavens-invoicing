package web

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

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
	schedules, err := h.repos.Recurring.List(ctx)
	if err != nil {
		writeRepoErr(w, err)
		return
	}
	rows := make([]recurringRowData, 0, len(schedules))
	for _, s := range schedules {
		row := recurringRowData{
			ID:             s.ID,
			ClientName:     h.clientName(ctx, s.ClientID),
			TemplateID:     s.InvoiceTemplateID,
			FrequencyLabel: frequencyLabels[s.Frequency],
			MethodLabel:    methodLabels[s.DeliveryMethod],
			Next:           s.NextSendDate,
			Last:           s.LastSentDate,
			Active:         s.Active,
		}
		if tpl, err := h.repos.Invoices.Get(ctx, s.InvoiceTemplateID); err == nil {
			row.TemplateNumber = tpl.Number
		}
		rows = append(rows, row)
	}
	h.render.renderPage(w, http.StatusOK, "recorrentes.html", "Recorrentes", recorrentesData{Rows: rows})
}

type selectGroup struct {
	Value    string
	Label    string
	Selected bool
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

func optionsFrom(values []string, labels map[string]string, selected string) []selectOption {
	opts := make([]selectOption, 0, len(values)+1)
	opts = append(opts, selectOption{Value: "", Label: "— selecione —"})
	for _, v := range values {
		opts = append(opts, selectOption{Value: v, Label: labels[v], Selected: v == selected})
	}
	return opts
}

func (h *Handlers) newRecurringForm(w http.ResponseWriter, r *http.Request) {
	data, err := h.recurringFormBase(r, "")
	if err != nil {
		writeRepoErr(w, err)
		return
	}
	h.render.renderPage(w, http.StatusOK, "recorrente_novo.html", "Nova recorrência", data)
}

// recurringFormBase assembles the select options for the new-recurring form.
// Templates are any draft invoice (they act as the model the scheduler
// clones on each fire).
func (h *Handlers) recurringFormBase(r *http.Request, clientFilter string) (*recorrenteFormData, error) {
	ctx := r.Context()
	clients, err := h.repos.Clients.List(ctx)
	if err != nil {
		return nil, err
	}
	drafts, err := h.repos.Invoices.ListByStatus(ctx, "draft")
	if err != nil {
		return nil, err
	}

	tplOpts := make([]selectOption, 0, len(drafts)+1)
	tplOpts = append(tplOpts, selectOption{Value: "", Label: "— selecione —"})
	for _, inv := range drafts {
		label := fmt.Sprintf("#%06d · %s · %s", inv.Number, h.clientName(ctx, inv.ClientID), pdf.FormatBRL(inv.Total))
		tplOpts = append(tplOpts, selectOption{Value: inv.ID, Label: label})
	}

	return &recorrenteFormData{
		Clients:   clientOptions(clients, ""),
		Templates: tplOpts,
		Frequency: optionsFrom(frequencyOrder, frequencyLabels, ""),
		Methods:   optionsFrom(methodOrder, methodLabels, ""),
		NextDate:  time.Now().Format("2006-01-02"),
	}, nil
}

func (h *Handlers) createRecurring(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulário inválido", http.StatusBadRequest)
		return
	}
	refail := func(msg string, code int) {
		data, err := h.recurringFormBase(r, "")
		if err != nil {
			writeRepoErr(w, err)
			return
		}
		data.Error = msg
		h.render.renderPage(w, code, "recorrente_novo.html", "Nova recorrência", data)
	}

	clientID := r.FormValue("client_id")
	templateID := r.FormValue("invoice_template_id")
	frequency := r.FormValue("frequency")
	method := r.FormValue("delivery_method")

	switch {
	case clientID == "":
		refail("Selecione o cliente.", http.StatusBadRequest)
		return
	case templateID == "":
		refail("Selecione a fatura modelo (rascunho).", http.StatusBadRequest)
		return
	case !slices.Contains(frequencyOrder, frequency):
		refail("Frequência inválida.", http.StatusBadRequest)
		return
	case !slices.Contains(methodOrder, method):
		refail("Método de entrega inválido.", http.StatusBadRequest)
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
	if _, err := h.repos.Invoices.Get(ctx, templateID); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			refail("Fatura modelo inexistente.", http.StatusBadRequest)
			return
		}
		writeRepoErr(w, err)
		return
	}

	nextSend, _ := time.ParseInLocation("2006-01-02", time.Now().Format("2006-01-02"), time.Local)
	if v := r.FormValue("next_send_date"); v != "" {
		parsed, err := time.ParseInLocation("2006-01-02", v, time.Local)
		if err != nil {
			refail("Data do próximo envio inválida.", http.StatusBadRequest)
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
		writeRepoErr(w, err)
		return
	}
	http.Redirect(w, r, "/recorrentes", http.StatusSeeOther)
}

func (h *Handlers) deleteRecurring(w http.ResponseWriter, r *http.Request) {
	if err := h.repos.Recurring.Delete(r.Context(), r.PathValue("id")); err != nil {
		writeRepoErr(w, err)
		return
	}
	http.Redirect(w, r, "/recorrentes", http.StatusSeeOther)
}
