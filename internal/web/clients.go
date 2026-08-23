package web

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/jesus/invoice-app/internal/i18n"
	"github.com/jesus/invoice-app/internal/model"
	"github.com/jesus/invoice-app/internal/repo"
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

type clientListData struct {
	Clients    []*model.Client
	Q          string
	Page       int
	Total      int
	TotalPages int
	HasPrev    bool
	HasNext    bool
	PrevURL    string
	NextURL    string
	Pages      []pageLink
}

func clientPageURL(page int, q string) string {
	v := url.Values{}
	if q != "" {
		v.Set("q", q)
	}
	if page > 1 {
		v.Set("page", strconv.Itoa(page))
	}
	if len(v) == 0 {
		return "/clients"
	}
	return "/clients?" + v.Encode()
}

func (h *Handlers) listClients(w http.ResponseWriter, r *http.Request) {
	lang := h.lang(r)
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	page := 1
	if s := r.URL.Query().Get("page"); s != "" {
		if p, err := strconv.Atoi(s); err == nil && p > 0 {
			page = p
		}
	}
	clients, total, err := h.repos.Clients.ListPaginated(r.Context(), page, q)
	if err != nil {
		writeRepoErr(w, lang, err)
		return
	}
	perPage := repo.ClientPageSize
	totalPages := (total + perPage - 1) / perPage
	if totalPages == 0 {
		totalPages = 1
	}
	hasPrev := page > 1
	hasNext := page < totalPages
	var prevURL, nextURL string
	if hasPrev {
		prevURL = clientPageURL(page-1, q)
	}
	if hasNext {
		nextURL = clientPageURL(page+1, q)
	}
	pages := make([]pageLink, 0, totalPages)
	for i := 1; i <= totalPages; i++ {
		pages = append(pages, pageLink{Num: i, URL: clientPageURL(i, q), Active: i == page})
	}
	h.renderPage(w, r, http.StatusOK, "clients.html", i18n.T(lang, "clients.title"), lang, clientListData{
		Clients:    clients,
		Q:          q,
		Page:       page,
		Total:      total,
		TotalPages: totalPages,
		HasPrev:    hasPrev,
		HasNext:    hasNext,
		PrevURL:    prevURL,
		NextURL:    nextURL,
		Pages:      pages,
	})
}

func (h *Handlers) newClientForm(w http.ResponseWriter, r *http.Request) {
	lang := h.lang(r)
	h.renderPage(w, r, http.StatusOK, "client_new.html", i18n.T(lang, "clients.new"), lang, clientForm{})
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
		h.renderPage(w, r, http.StatusBadRequest, "client_new.html", i18n.T(lang, "clients.new"), lang, f)
		return
	}
	clientLang, ok := i18n.Normalize(c.Language)
	if !ok {
		f.Error = i18n.T(lang, "error.language_invalid")
		h.renderPage(w, r, http.StatusBadRequest, "client_new.html", i18n.T(lang, "clients.new"), lang, f)
		return
	}
	c.Name = strings.TrimSpace(c.Name)
	c.Language = string(clientLang)
	created, err := h.repos.Clients.Create(r.Context(), c)
	if err != nil {
		writeRepoErr(w, lang, err)
		return
	}
	http.Redirect(w, r, "/clients/"+created.ID+"?created=1", http.StatusSeeOther)
}

type clientDetailData struct {
	Client   *model.Client
	Invoices []invoiceRow
	Created  bool
	Error    string
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
	created := r.URL.Query().Get("created") == "1"
	h.renderPage(w, r, http.StatusOK, "client_detail.html", client.Name, lang, clientDetailData{
		Client:   client,
		Invoices: rows,
		Created:  created,
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
		invoices, _ := h.repos.Invoices.ListByClient(r.Context(), id)
		rows, _ := buildInvoiceRows(r.Context(), h, invoices)
		c.ID = id
		if c.Language == "" {
			c.Language = string(defaultLang)
		}
		h.renderPage(w, r, http.StatusBadRequest, "client_detail.html", i18n.T(lang, "clients.detail.edit"), lang, clientDetailData{
			Client:   c,
			Invoices: rows,
			Error:    i18n.T(lang, "error.name_required"),
		})
		return
	}
	clientLang, ok := i18n.Normalize(c.Language)
	if !ok {
		invoices, _ := h.repos.Invoices.ListByClient(r.Context(), id)
		rows, _ := buildInvoiceRows(r.Context(), h, invoices)
		c.ID = id
		h.renderPage(w, r, http.StatusBadRequest, "client_detail.html", i18n.T(lang, "clients.detail.edit"), lang, clientDetailData{
			Client:   c,
			Invoices: rows,
			Error:    i18n.T(lang, "error.language_invalid"),
		})
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

func (h *Handlers) deleteClient(w http.ResponseWriter, r *http.Request) {
	lang := h.lang(r)
	id := r.PathValue("id")
	if err := h.repos.Clients.Delete(r.Context(), id); err != nil {
		writeRepoErr(w, lang, err)
		return
	}
	http.Redirect(w, r, "/clients", http.StatusSeeOther)
}
