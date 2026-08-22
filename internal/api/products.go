package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/jesus/invoice-app/internal/model"
)

func (a *api) listProducts(w http.ResponseWriter, r *http.Request) {
	products, err := a.repos.Products.List(r.Context())
	if err != nil {
		writeRepoErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, products)
}

func (a *api) createProduct(w http.ResponseWriter, r *http.Request) {
	var p model.Product
	if !decodeJSON(w, r, &p) {
		return
	}
	if strings.TrimSpace(p.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if p.UnitPrice < 0 {
		writeError(w, http.StatusBadRequest, "unit_price must be >= 0")
		return
	}
	// Caller-supplied identity and timestamps are never honored.
	p.ID = ""
	p.CreatedAt = time.Time{}
	p.UpdatedAt = time.Time{}
	created, err := a.repos.Products.Create(r.Context(), &p)
	if err != nil {
		writeRepoErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (a *api) getProduct(w http.ResponseWriter, r *http.Request) {
	p, err := a.repos.Products.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeRepoErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (a *api) updateProduct(w http.ResponseWriter, r *http.Request) {
	var p model.Product
	if !decodeJSON(w, r, &p) {
		return
	}
	p.ID = r.PathValue("id")
	if strings.TrimSpace(p.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if p.UnitPrice < 0 {
		writeError(w, http.StatusBadRequest, "unit_price must be >= 0")
		return
	}
	if err := a.repos.Products.Update(r.Context(), &p); err != nil {
		writeRepoErr(w, err)
		return
	}
	// Re-fetch so the response carries the stored timestamps.
	stored, err := a.repos.Products.Get(r.Context(), p.ID)
	if err != nil {
		writeRepoErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, stored)
}

func (a *api) deleteProduct(w http.ResponseWriter, r *http.Request) {
	if err := a.repos.Products.Delete(r.Context(), r.PathValue("id")); err != nil {
		writeRepoErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
