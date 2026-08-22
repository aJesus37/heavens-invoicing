package web

import (
	"net/http"
	"strings"

	"github.com/jesus/invoice-app/internal/i18n"
	"github.com/jesus/invoice-app/internal/model"
)

type productForm struct {
	Name        string
	Description string
	Price       string
	Active      bool
	Error       string
	Action      string
	Editing     bool
	ID          string
}

func (h *Handlers) listProducts(w http.ResponseWriter, r *http.Request) {
	lang := h.lang(r)
	products, err := h.repos.Products.List(r.Context())
	if err != nil {
		writeRepoErr(w, lang, err)
		return
	}
	h.renderPage(w, r, http.StatusOK, "produtos.html", i18n.T(lang, "products.title"), lang, products)
}

func (h *Handlers) newProductForm(w http.ResponseWriter, r *http.Request) {
	lang := h.lang(r)
	h.renderPage(w, r, http.StatusOK, "produto_form.html", i18n.T(lang, "products.new"), lang, &productForm{Action: "/produtos/novo"})
}

func formToProduct(r *http.Request) (*model.Product, productForm) {
	f := productForm{
		Name:        r.FormValue("name"),
		Description: r.FormValue("description"),
		Price:       r.FormValue("unit_price"),
		Active:      r.FormValue("active") == "on",
	}
	return &model.Product{Name: f.Name, Description: f.Description}, f
}

func (h *Handlers) createProduct(w http.ResponseWriter, r *http.Request) {
	lang := h.lang(r)
	if err := r.ParseForm(); err != nil {
		failBadRequest(w, lang)
		return
	}
	p, f := formToProduct(r)
	f.Action = "/produtos/novo"

	cents, err := parseReais(lang, f.Price)
	if err != nil {
		f.Error = i18n.T(lang, "error.unit_price", err.Error())
		h.renderPage(w, r, http.StatusBadRequest, "produto_form.html", i18n.T(lang, "products.new"), lang, &f)
		return
	}
	if strings.TrimSpace(p.Name) == "" {
		f.Error = i18n.T(lang, "error.name_required")
		h.renderPage(w, r, http.StatusBadRequest, "produto_form.html", i18n.T(lang, "products.new"), lang, &f)
		return
	}
	p.UnitPrice = cents
	if _, err := h.repos.Products.Create(r.Context(), p); err != nil {
		writeRepoErr(w, lang, err)
		return
	}
	http.Redirect(w, r, "/produtos", http.StatusSeeOther)
}

func (h *Handlers) editProductForm(w http.ResponseWriter, r *http.Request) {
	lang := h.lang(r)
	p, err := h.repos.Products.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeRepoErr(w, lang, err)
		return
	}
	f := &productForm{
		Name:        p.Name,
		Description: p.Description,
		Price:       formatReais(p.UnitPrice),
		Active:      p.Active,
		Action:      "/produtos/" + p.ID + "/editar",
		Editing:     true,
		ID:          p.ID,
	}
	h.renderPage(w, r, http.StatusOK, "produto_form.html", i18n.T(lang, "products.edit_title"), lang, f)
}

func (h *Handlers) updateProduct(w http.ResponseWriter, r *http.Request) {
	lang := h.lang(r)
	id := r.PathValue("id")
	p, err := h.repos.Products.Get(r.Context(), id)
	if err != nil {
		writeRepoErr(w, lang, err)
		return
	}
	if err := r.ParseForm(); err != nil {
		failBadRequest(w, lang)
		return
	}
	_, f := formToProduct(r)
	cents, err := parseReais(lang, f.Price)
	if err != nil {
		f.Error = i18n.T(lang, "error.unit_price", err.Error())
		f.Action = "/produtos/" + id + "/editar"
		f.Editing = true
		f.ID = id
		h.renderPage(w, r, http.StatusBadRequest, "produto_form.html", i18n.T(lang, "products.edit_title"), lang, &f)
		return
	}
	if strings.TrimSpace(f.Name) == "" {
		f.Error = i18n.T(lang, "error.name_required")
		f.Action = "/produtos/" + id + "/editar"
		f.Editing = true
		f.ID = id
		h.renderPage(w, r, http.StatusBadRequest, "produto_form.html", i18n.T(lang, "products.edit_title"), lang, &f)
		return
	}
	p.Name = strings.TrimSpace(f.Name)
	p.Description = f.Description
	p.UnitPrice = cents
	p.Active = f.Active
	if err := h.repos.Products.Update(r.Context(), p); err != nil {
		writeRepoErr(w, lang, err)
		return
	}
	http.Redirect(w, r, "/produtos", http.StatusSeeOther)
}
