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

	"github.com/jesus/invoice-app/internal/auth"
	"github.com/jesus/invoice-app/internal/deliver"
	"github.com/jesus/invoice-app/internal/i18n"
	"github.com/jesus/invoice-app/internal/pdf"
	"github.com/jesus/invoice-app/internal/repo"
	"github.com/jesus/invoice-app/internal/whatsapp"
	assets "github.com/jesus/invoice-app/web"
)

// Handlers owns every page route plus the small htmx fragment endpoints.
type Handlers struct {
	repos   *repo.Repos
	router  *deliver.Router
	wa      *whatsapp.Session
	sender  pdf.SenderInfo
	pairing *pairingManager
	auth    *auth.Manager
	render  *renderer
	static  http.Handler
}

// New parses the embedded templates eagerly so a broken template fails at
// startup rather than on first request. am carries the session/password
// manager backing the login, logout and gate behavior.
func New(r *repo.Repos, router *deliver.Router, wa *whatsapp.Session, sender pdf.SenderInfo, am *auth.Manager) (*Handlers, error) {
	rnd, err := newRenderer(assets.Files)
	if err != nil {
		return nil, err
	}
	staticFS, err := fs.Sub(assets.Files, "static")
	if err != nil {
		return nil, err
	}
	h := &Handlers{
		repos:   r,
		router:  router,
		wa:      wa,
		sender:  sender,
		pairing: newPairing(wa),
		auth:    am,
		render:  rnd,
		static:  http.FileServerFS(staticFS),
	}
	return h, nil
}

func (h *Handlers) Mux() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /{$}", h.dashboard)
	mux.Handle("GET /static/", http.StripPrefix("/static", h.static))

	mux.HandleFunc("GET /login", h.loginForm)
	mux.HandleFunc("POST /login", h.loginSubmit)
	mux.HandleFunc("POST /logout", h.logout)

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
	mux.HandleFunc("POST /faturas/{id}/cancelar", h.cancelInvoiceAction)

	mux.HandleFunc("GET /recorrentes", h.listRecurring)
	mux.HandleFunc("GET /recorrentes/novo", h.newRecurringForm)
	mux.HandleFunc("POST /recorrentes/novo", h.createRecurring)
	mux.HandleFunc("POST /recorrentes/{id}/excluir", h.deleteRecurring)
	mux.HandleFunc("POST /recorrentes/{id}/alternar", h.toggleRecurring)

	mux.HandleFunc("GET /configuracoes", h.settingsForm)
	mux.HandleFunc("POST /configuracoes", h.saveSettings)
	mux.HandleFunc("GET /configuracoes/whatsapp/status", h.whatsappStatusFragment)
	mux.HandleFunc("POST /configuracoes/whatsapp/conectar", h.whatsappConnect)
	mux.HandleFunc("GET /configuracoes/whatsapp/qr.png", h.whatsappQRPNG)

	return mux
}

// writeRepoErr maps repo failures: ErrNotFound → 404, anything else is an
// unexpected internal error.
func writeRepoErr(w http.ResponseWriter, lang i18n.Lang, err error) {
	if errors.Is(err, repo.ErrNotFound) {
		failNotFound(w, lang)
		return
	}
	log.Printf("web: repo error: %v", err)
	failInternal(w, lang)
}

// strPtr converts a form value to a nullable field: blank means unset.
func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
