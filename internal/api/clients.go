package api

import (
	"net/http"
	"strings"

	"github.com/jesus/invoice-app/internal/model"
)

func (a *api) listClients(w http.ResponseWriter, r *http.Request) {
	clients, err := a.repos.Clients.List(r.Context())
	if err != nil {
		writeRepoErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, clients)
}

func (a *api) createClient(w http.ResponseWriter, r *http.Request) {
	var c model.Client
	if !decodeJSON(w, r, &c) {
		return
	}
	if strings.TrimSpace(c.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	created, err := a.repos.Clients.Create(r.Context(), &c)
	if err != nil {
		writeRepoErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (a *api) getClient(w http.ResponseWriter, r *http.Request) {
	c, err := a.repos.Clients.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeRepoErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (a *api) updateClient(w http.ResponseWriter, r *http.Request) {
	var c model.Client
	if !decodeJSON(w, r, &c) {
		return
	}
	c.ID = r.PathValue("id")
	if strings.TrimSpace(c.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if err := a.repos.Clients.Update(r.Context(), &c); err != nil {
		writeRepoErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, &c)
}

func (a *api) deleteClient(w http.ResponseWriter, r *http.Request) {
	if err := a.repos.Clients.Delete(r.Context(), r.PathValue("id")); err != nil {
		writeRepoErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
