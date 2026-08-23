package web

import (
	"context"
	"net/http"
	"strings"

	"github.com/jesus/invoice-app/internal/i18n"
	"github.com/jesus/invoice-app/internal/model"
)

// clientName resolves a client id for display; failures degrade to the raw
// id instead of failing the whole page.
func (h *Handlers) clientName(ctx context.Context, id string) string {
	c, err := h.repos.Clients.Get(ctx, id)
	if err != nil {
		return id
	}
	return c.Name
}

type clientForm struct {
	Name           string
	Email          string
	Phone          string
	TelegramChatID string
	PIXKey         string
	Address        string
	Notes          string
	Language       string
	Error          string
}

func formToClient(r *http.Request) (*model.Client, clientForm) {
	f := clientForm{
		Name:           r.FormValue("name"),
		Email:          r.FormValue("email"),
		Phone:          r.FormValue("phone"),
		TelegramChatID: r.FormValue("telegram_chat_id"),
		PIXKey:         r.FormValue("pix_key"),
		Address:        r.FormValue("address"),
		Notes:          r.FormValue("notes"),
		Language:       r.FormValue("language"),
	}
	c := &model.Client{
		Name:           f.Name,
		Email:          strPtr(f.Email),
		Phone:          strPtr(f.Phone),
		TelegramChatID: strPtr(f.TelegramChatID),
		PIXKey:         strPtr(f.PIXKey),
		Address:        f.Address,
		Notes:          f.Notes,
		Language:       f.Language,
	}
	return c, f
}

func (h *Handlers) listClients(w http.ResponseWriter, r *http.Request) {
	lang := h.lang(r)
	clients, err := h.repos.Clients.List(r.Context())
	if err != nil {
		writeRepoErr(w, lang, err)
		return
	}
	h.renderPage(w, r, http.StatusOK, "clientes.html", i18n.T(lang, "clients.title"), lang, clients)
}

func (h *Handlers) newClientForm(w http.ResponseWriter, r *http.Request) {
	lang := h.lang(r)
	h.renderPage(w, r, http.StatusOK, "cliente_novo.html", i18n.T(lang, "clients.new"), lang, clientForm{})
}

func (h *Handlers) createClient(w http.ResponseWriter, r *http.Request) {
	lang := h.lang(r)
	if err := r.ParseForm(); err != nil {
		failBadRequest(w, lang)
		return
	}
	c, f := formToClient(r)
	if strings.TrimSpace(c.Name) == "" {
		f.Error = i18n.T(lang, "error.name_required")
		h.renderPage(w, r, http.StatusBadRequest, "cliente_novo.html", i18n.T(lang, "clients.new"), lang, f)
		return
	}
	clientLang, ok := i18n.Normalize(c.Language)
	if !ok {
		f.Error = i18n.T(lang, "error.language_invalid")
		h.renderPage(w, r, http.StatusBadRequest, "cliente_novo.html", i18n.T(lang, "clients.new"), lang, f)
		return
	}
	c.Name = strings.TrimSpace(c.Name)
	c.Language = string(clientLang)
	created, err := h.repos.Clients.Create(r.Context(), c)
	if err != nil {
		writeRepoErr(w, lang, err)
		return
	}
	http.Redirect(w, r, "/clients/"+created.ID, http.StatusSeeOther)
}

type clientDetailData struct {
	Client   *model.Client
	Invoices []invoiceRow
}

func (h *Handlers) showClient(w http.ResponseWriter, r *http.Request) {
	lang := h.lang(r)
	client, err := h.repos.Clients.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeRepoErr(w, lang, err)
		return
	}
	invoices, err := h.repos.Invoices.ListByClient(r.Context(), client.ID)
	if err != nil {
		writeRepoErr(w, lang, err)
		return
	}
	rows, err := buildInvoiceRows(r.Context(), h, invoices)
	if err != nil {
		writeRepoErr(w, lang, err)
		return
	}
	h.renderPage(w, r, http.StatusOK, "cliente_detalhe.html", client.Name, lang, clientDetailData{
		Client:   client,
		Invoices: rows,
	})
}

func (h *Handlers) updateClient(w http.ResponseWriter, r *http.Request) {
	lang := h.lang(r)
	id := r.PathValue("id")
	if _, err := h.repos.Clients.Get(r.Context(), id); err != nil {
		writeRepoErr(w, lang, err)
		return
	}
	if err := r.ParseForm(); err != nil {
		failBadRequest(w, lang)
		return
	}
	c, _ := formToClient(r)
	c.Name = strings.TrimSpace(c.Name)
	if c.Name == "" {
		http.Error(w, i18n.T(lang, "error.name_required"), http.StatusBadRequest)
		return
	}
	clientLang, ok := i18n.Normalize(c.Language)
	if !ok {
		http.Error(w, i18n.T(lang, "error.language_invalid"), http.StatusBadRequest)
		return
	}
	c.Language = string(clientLang)
	c.ID = id
	if err := h.repos.Clients.Update(r.Context(), c); err != nil {
		writeRepoErr(w, lang, err)
		return
	}
	http.Redirect(w, r, "/clients/"+id, http.StatusSeeOther)
}
