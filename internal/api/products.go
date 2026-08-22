package api

import (
	"net/http"
	"strings"

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
	if err := a.repos.Products.Update(r.Context(), &p); err != nil {
		writeRepoErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, &p)
}

func (a *api) deleteProduct(w http.ResponseWriter, r *http.Request) {
	if err := a.repos.Products.Delete(r.Context(), r.PathValue("id")); err != nil {
		writeRepoErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
