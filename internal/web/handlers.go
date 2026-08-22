// Package web serves the server-rendered UI (html/template + htmx). Page
// handlers talk to the repos directly and reuse the delivery router for
// invoice sending; the JSON API under /api/ remains the programmatic
// surface.
package web

import (
	"errors"
	"io/fs"
	"log"
	"net/http"

	"github.com/jesus/invoice-app/internal/deliver"
	"github.com/jesus/invoice-app/internal/pdf"
	"github.com/jesus/invoice-app/internal/repo"
	"github.com/jesus/invoice-app/internal/whatsapp"
	assets "github.com/jesus/invoice-app/web"
)

// Handlers owns every page route plus the small htmx fragment endpoints.
type Handlers struct {
	repos  *repo.Repos
	router *deliver.Router
	wa     *whatsapp.Session
	sender pdf.SenderInfo
	render *renderer
	static http.Handler
}

// New parses the embedded templates eagerly so a broken template fails at
// startup rather than on first request.
func New(r *repo.Repos, router *deliver.Router, wa *whatsapp.Session, sender pdf.SenderInfo) (*Handlers, error) {
	rnd, err := newRenderer(assets.Files)
	if err != nil {
		return nil, err
	}
	staticFS, err := fs.Sub(assets.Files, "static")
	if err != nil {
		return nil, err
	}
	h := &Handlers{
		repos:  r,
		router: router,
		wa:     wa,
		sender: sender,
		render: rnd,
		static: http.FileServerFS(staticFS),
	}
	return h, nil
}

func (h *Handlers) Mux() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /{$}", h.dashboard)
	mux.Handle("GET /static/", http.StripPrefix("/static", h.static))

	mux.HandleFunc("GET /clientes", h.listClients)
	mux.HandleFunc("GET /clientes/novo", h.newClientForm)
	mux.HandleFunc("POST /clientes/novo", h.createClient)
	mux.HandleFunc("GET /clientes/{id}", h.showClient)
	mux.HandleFunc("POST /clientes/{id}", h.updateClient)

	mux.HandleFunc("GET /produtos", h.listProducts)
	mux.HandleFunc("GET /produtos/novo", h.newProductForm)
	mux.HandleFunc("POST /produtos/novo", h.createProduct)
	mux.HandleFunc("GET /produtos/{id}/editar", h.editProductForm)
	mux.HandleFunc("POST /produtos/{id}/editar", h.updateProduct)

	mux.HandleFunc("GET /faturas", h.listInvoices)
	mux.HandleFunc("GET /faturas/nova", h.newInvoiceForm)
	mux.HandleFunc("POST /faturas/nova", h.createInvoice)
	mux.HandleFunc("GET /faturas/{id}", h.showInvoice)
	mux.HandleFunc("POST /faturas/{id}/enviar", h.sendInvoiceAction)
	mux.HandleFunc("POST /faturas/{id}/marcar-paga", h.markInvoicePaidAction)

	mux.HandleFunc("GET /recorrentes", h.listRecurring)
	mux.HandleFunc("GET /recorrentes/novo", h.newRecurringForm)
	mux.HandleFunc("POST /recorrentes/novo", h.createRecurring)
	mux.HandleFunc("POST /recorrentes/{id}/excluir", h.deleteRecurring)

	return mux
}

// notFound replies with a plain 404; missing ids are never template errors.
func notFound(w http.ResponseWriter) {
	http.Error(w, "não encontrado", http.StatusNotFound)
}

// writeRepoErr maps repo failures: ErrNotFound → 404, anything else is an
// unexpected internal error.
func writeRepoErr(w http.ResponseWriter, err error) {
	if errors.Is(err, repo.ErrNotFound) {
		notFound(w)
		return
	}
	log.Printf("web: repo error: %v", err)
	http.Error(w, "erro interno", http.StatusInternalServerError)
}

// strPtr converts a form value to a nullable field: blank means unset.
func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
