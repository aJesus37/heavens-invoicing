package web

import (
	"net/http"
	"strings"

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
	products, err := h.repos.Products.List(r.Context())
	if err != nil {
		writeRepoErr(w, err)
		return
	}
	h.render.renderPage(w, http.StatusOK, "produtos.html", "Produtos", products)
}

func (h *Handlers) newProductForm(w http.ResponseWriter, r *http.Request) {
	h.render.renderPage(w, http.StatusOK, "produto_form.html", "Novo produto", &productForm{Action: "/produtos/novo"})
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
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulário inválido", http.StatusBadRequest)
		return
	}
	p, f := formToProduct(r)
	f.Action = "/produtos/novo"

	cents, err := parseReais(f.Price)
	if err != nil {
		f.Error = "Preço unitário: " + err.Error() + "."
		h.render.renderPage(w, http.StatusBadRequest, "produto_form.html", "Novo produto", &f)
		return
	}
	if strings.TrimSpace(p.Name) == "" {
		f.Error = "O nome é obrigatório."
		h.render.renderPage(w, http.StatusBadRequest, "produto_form.html", "Novo produto", &f)
		return
	}
	p.UnitPrice = cents
	if _, err := h.repos.Products.Create(r.Context(), p); err != nil {
		writeRepoErr(w, err)
		return
	}
	http.Redirect(w, r, "/produtos", http.StatusSeeOther)
}

func (h *Handlers) editProductForm(w http.ResponseWriter, r *http.Request) {
	p, err := h.repos.Products.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeRepoErr(w, err)
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
	h.render.renderPage(w, http.StatusOK, "produto_form.html", "Editar produto", f)
}

func (h *Handlers) updateProduct(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, err := h.repos.Products.Get(r.Context(), id)
	if err != nil {
		writeRepoErr(w, err)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulário inválido", http.StatusBadRequest)
		return
	}
	_, f := formToProduct(r)
	cents, err := parseReais(f.Price)
	if err != nil {
		f.Error = "Preço unitário: " + err.Error() + "."
		f.Action = "/produtos/" + id + "/editar"
		f.Editing = true
		f.ID = id
		h.render.renderPage(w, http.StatusBadRequest, "produto_form.html", "Editar produto", &f)
		return
	}
	if strings.TrimSpace(f.Name) == "" {
		http.Error(w, "o nome é obrigatório", http.StatusBadRequest)
		return
	}
	p.Name = f.Name
	p.Description = f.Description
	p.UnitPrice = cents
	p.Active = f.Active
	if err := h.repos.Products.Update(r.Context(), p); err != nil {
		writeRepoErr(w, err)
		return
	}
	http.Redirect(w, r, "/produtos", http.StatusSeeOther)
}
