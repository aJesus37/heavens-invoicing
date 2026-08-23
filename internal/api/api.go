// Package api exposes the JSON API over the app's repos and delivery
// router. It is mounted by the HTTP server at /api/.
package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ajesus37/heavens-invoicing/internal/deliver"
	"github.com/ajesus37/heavens-invoicing/internal/pdf"
	"github.com/ajesus37/heavens-invoicing/internal/repo"
)

const maxBodyBytes = 1 << 20 // 1 MiB

// New builds the JSON API mux. senderInfo holds the business identity used
// when rendering invoice PDFs.
func New(r *repo.Repos, router *deliver.Router, senderInfo pdf.SenderInfo) http.Handler {
	a := &api{repos: r, router: router, senderInfo: senderInfo}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/clients", a.listClients)
	mux.HandleFunc("POST /api/clients", a.createClient)
	mux.HandleFunc("GET /api/clients/{id}", a.getClient)
	mux.HandleFunc("PUT /api/clients/{id}", a.updateClient)
	mux.HandleFunc("DELETE /api/clients/{id}", a.deleteClient)

	mux.HandleFunc("GET /api/products", a.listProducts)
	mux.HandleFunc("POST /api/products", a.createProduct)
	mux.HandleFunc("GET /api/products/{id}", a.getProduct)
	mux.HandleFunc("PUT /api/products/{id}", a.updateProduct)
	mux.HandleFunc("DELETE /api/products/{id}", a.deleteProduct)

	mux.HandleFunc("GET /api/invoices", a.listInvoices)
	mux.HandleFunc("POST /api/invoices", a.createInvoice)
	mux.HandleFunc("GET /api/invoices/{id}", a.getInvoice)
	mux.HandleFunc("GET /api/invoices/{id}/pdf", a.invoicePDF)
	mux.HandleFunc("POST /api/invoices/{id}/send", a.sendInvoice)
	mux.HandleFunc("POST /api/invoices/{id}/mark-paid", a.markInvoicePaid)
	mux.HandleFunc("POST /api/invoices/{id}/cancel", a.cancelInvoice)

	mux.HandleFunc("GET /api/recurring", a.listRecurring)
	mux.HandleFunc("POST /api/recurring", a.createRecurring)
	mux.HandleFunc("PUT /api/recurring/{id}", a.updateRecurring)
	mux.HandleFunc("DELETE /api/recurring/{id}", a.deleteRecurring)

	return mux
}

type api struct {
	repos      *repo.Repos
	router     *deliver.Router
	senderInfo pdf.SenderInfo
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

type errorResponse struct {
	Error string `json:"error"`
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}

// decodeJSON decodes the request body into dst, replying 400 and returning
// false on malformed or oversized input.
func decodeJSON[T any](w http.ResponseWriter, r *http.Request, dst *T) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

// writeRepoErr maps repo errors to status codes: ErrNotFound → 404,
// anything else → 500 with a generic message (internal details stay out of
// responses).
func writeRepoErr(w http.ResponseWriter, err error) {
	if errors.Is(err, repo.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	writeError(w, http.StatusInternalServerError, "internal error")
}
