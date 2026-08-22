package web

import (
	"context"
	"net/http"
	"strings"

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
	}
	c := &model.Client{
		Name:           f.Name,
		Email:          strPtr(f.Email),
		Phone:          strPtr(f.Phone),
		TelegramChatID: strPtr(f.TelegramChatID),
		PIXKey:         strPtr(f.PIXKey),
		Address:        f.Address,
		Notes:          r.FormValue("notes"),
	}
	return c, f
}

func (h *Handlers) listClients(w http.ResponseWriter, r *http.Request) {
	clients, err := h.repos.Clients.List(r.Context())
	if err != nil {
		writeRepoErr(w, err)
		return
	}
	h.render.renderPage(w, http.StatusOK, "clientes.html", "Clientes", clients)
}

func (h *Handlers) newClientForm(w http.ResponseWriter, r *http.Request) {
	h.render.renderPage(w, http.StatusOK, "cliente_novo.html", "Novo cliente", clientForm{})
}

func (h *Handlers) createClient(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulário inválido", http.StatusBadRequest)
		return
	}
	c, f := formToClient(r)
	if strings.TrimSpace(c.Name) == "" {
		f.Error = "O nome é obrigatório."
		h.render.renderPage(w, http.StatusBadRequest, "cliente_novo.html", "Novo cliente", f)
		return
	}
	c.Name = strings.TrimSpace(c.Name)
	created, err := h.repos.Clients.Create(r.Context(), c)
	if err != nil {
		writeRepoErr(w, err)
		return
	}
	http.Redirect(w, r, "/clientes/"+created.ID, http.StatusSeeOther)
}

type clientDetailData struct {
	Client   *model.Client
	Invoices []invoiceRow
}

func (h *Handlers) showClient(w http.ResponseWriter, r *http.Request) {
	client, err := h.repos.Clients.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeRepoErr(w, err)
		return
	}
	invoices, err := h.repos.Invoices.ListByClient(r.Context(), client.ID)
	if err != nil {
		writeRepoErr(w, err)
		return
	}
	rows, err := buildInvoiceRows(r.Context(), h, invoices)
	if err != nil {
		writeRepoErr(w, err)
		return
	}
	h.render.renderPage(w, http.StatusOK, "cliente_detalhe.html", client.Name, clientDetailData{
		Client:   client,
		Invoices: rows,
	})
}

func (h *Handlers) updateClient(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := h.repos.Clients.Get(r.Context(), id); err != nil {
		writeRepoErr(w, err)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulário inválido", http.StatusBadRequest)
		return
	}
	c, _ := formToClient(r)
	c.Name = strings.TrimSpace(c.Name)
	if c.Name == "" {
		http.Error(w, "o nome é obrigatório", http.StatusBadRequest)
		return
	}
	c.ID = id
	if err := h.repos.Clients.Update(r.Context(), c); err != nil {
		writeRepoErr(w, err)
		return
	}
	http.Redirect(w, r, "/clientes/"+id, http.StatusSeeOther)
}
