// Package web serves the server-rendered UI (html/template + htmx). Page
// handlers talk to the repos directly and reuse the delivery router for
// invoice sending; the JSON API under /api/ remains the programmatic
// surface.
package web

import (
	"context"
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
	repos        *repo.Repos
	router       *deliver.Router
	wa           *whatsapp.Session
	sender       pdf.SenderInfo
	pairing      *pairingManager
	auth         *auth.Manager
	render       *renderer
	static       http.Handler
	tgReloader   func(context.Context)
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

// SetTelegramReloader registers a callback invoked after Settings are saved
// so the Telegram bot can be hot-reloaded without a process restart.
func (h *Handlers) SetTelegramReloader(fn func(context.Context)) {
	h.tgReloader = fn
}

func (h *Handlers) Mux() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /{$}", h.dashboard)
	mux.Handle("GET /static/", http.StripPrefix("/static", h.static))
	// Dead-end the favicon probe: without this it falls through to the
	// dashboard route, which for anonymous visitors 303s into /login —
	// and browsers silently follow, re-rendering the login form (rotating
	// its CSRF cookie out from under the page in a second tab scenario)
	// while spamming console errors.
	mux.HandleFunc("GET /favicon.ico", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("GET /login", h.loginForm)
	mux.HandleFunc("POST /login", h.loginSubmit)
	mux.HandleFunc("POST /logout", h.logout)

	mux.HandleFunc("GET /clients", h.listClients)
	mux.HandleFunc("GET /clients/new", h.newClientForm)
	mux.HandleFunc("POST /clients/new", h.createClient)
	mux.HandleFunc("GET /clients/{id}", h.showClient)
	mux.HandleFunc("POST /clients/{id}", h.updateClient)

	mux.HandleFunc("GET /products", h.listProducts)
	mux.HandleFunc("GET /products/new", h.newProductForm)
	mux.HandleFunc("POST /products/new", h.createProduct)
	mux.HandleFunc("GET /products/{id}/edit", h.editProductForm)
	mux.HandleFunc("POST /products/{id}/edit", h.updateProduct)

	mux.HandleFunc("GET /invoices", h.listInvoices)
	mux.HandleFunc("GET /invoices/new", h.newInvoiceForm)
	mux.HandleFunc("POST /invoices/new", h.createInvoice)
	mux.HandleFunc("GET /invoices/{id}", h.showInvoice)
	mux.HandleFunc("POST /invoices/{id}/send", h.sendInvoiceAction)
	mux.HandleFunc("POST /invoices/{id}/mark-paid", h.markInvoicePaidAction)
	mux.HandleFunc("POST /invoices/{id}/cancel", h.cancelInvoiceAction)

	mux.HandleFunc("GET /recurring", h.listRecurring)
	mux.HandleFunc("GET /recurring/new", h.newRecurringForm)
	mux.HandleFunc("POST /recurring/new", h.createRecurring)
	mux.HandleFunc("POST /recurring/{id}/delete", h.deleteRecurring)
	mux.HandleFunc("POST /recurring/{id}/toggle", h.toggleRecurring)

	mux.HandleFunc("GET /settings", h.settingsForm)
	mux.HandleFunc("POST /settings", h.saveSettings)
	mux.HandleFunc("GET /settings/whatsapp/status", h.whatsappStatusFragment)
	mux.HandleFunc("POST /settings/whatsapp/connect", h.whatsappConnect)
	mux.HandleFunc("GET /settings/whatsapp/qr.png", h.whatsappQRPNG)

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
